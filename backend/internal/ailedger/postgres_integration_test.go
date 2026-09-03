package ailedger

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestPostgresLedgerLiveQueriesAndRetention(t *testing.T) {
	dsn := os.Getenv("AI_LEDGER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("AI_LEDGER_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	slug := fmt.Sprintf("ledger-live-%d", now.UnixNano())
	var orgID, deviceID, connectorID string
	if err := db.QueryRowContext(ctx, `INSERT INTO organizations(name,slug,api_key_hash) VALUES($1,$2,'test') RETURNING id::text`, slug, slug).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM ai_ledger_activity_events WHERE org_id=$1`, orgID)
		_, _ = db.ExecContext(ctx, `DELETE FROM organizations WHERE id=$1::uuid`, orgID)
	}()
	if err := db.QueryRowContext(ctx, `INSERT INTO devices(org_id,device_name,os,status) VALUES($1::uuid,'shared-mac','darwin','active') RETURNING id::text`, orgID).Scan(&deviceID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO ai_ledger_connectors(org_id,adapter_key,display_name,encrypted_credentials,status,freshness_at) VALUES($1::uuid,'fixture','Fixture','x','healthy',$2) RETURNING id::text`, orgID, now).Scan(&connectorID); err != nil {
		t.Fatal(err)
	}

	store := NewSQLStore(db)
	sink := &connectorFactSink{delegate: store.FactSink(orgID, connectorID), connectorID: connectorID, now: now}
	base := Provenance{VendorKey: "acme", ProductKey: "assistant", SourceKey: "product", FreshnessAt: now, Scope: "organization"}
	if err := sink.UpsertProduct(ProductFact{Provenance: base, DisplayName: "Acme Assistant", FunctionalCategory: "general_assistant", HasContract: true, HasBillingSource: true}); err != nil {
		t.Fatal(err)
	}
	directory := base
	directory.SourceKey = "directory-user"
	if err := sink.UpsertIdentity(ExternalIdentityFact{Provenance: directory, IdentitySourceKey: "directory", IdentityKind: "directory_user", ExternalID: "employee-1", NormalizedEmail: "employee@example.com"}); err != nil {
		t.Fatal(err)
	}
	account := base
	account.SourceKey = "provider-account"
	if err := sink.UpsertIdentity(ExternalIdentityFact{Provenance: account, IdentitySourceKey: "directory", IdentityKind: "account", ExternalID: "employee-1", NormalizedEmail: "renamed@example.com"}); err != nil {
		t.Fatal(err)
	}
	periodStart, periodEnd := now.Add(-24*time.Hour), now
	seat := base
	seat.SourceKey = "seat"
	seat.ReportingPeriodStart, seat.ReportingPeriodEnd = &periodStart, &periodEnd
	amount := "25.000000000"
	if err := sink.UpsertLicense(LicenseFact{Provenance: seat, LicenseKey: "seat-1", ExternalIdentityID: "employee-1", Status: "assigned", Paid: true, UnitAmount: &amount, Currency: "USD", BillingInterval: "month"}); err != nil {
		t.Fatal(err)
	}
	metric := seat
	metric.SourceKey = "requests"
	if err := sink.UpsertMetric(MetricFact{Provenance: metric, MetricKey: "requests", Value: "7", Unit: "requests", IsActivity: true, ExternalIdentityID: "employee-1"}); err != nil {
		t.Fatal(err)
	}
	for _, cost := range []struct{ key, amount, currency, kind string }{
		{"cost-usd", "123.450000000", "USD", "charge"},
		{"credit-usd", "-3.450000000", "USD", "credit"},
		{"cost-eur", "10.000000000", "EUR", "charge"},
	} {
		p := seat
		p.SourceKey = cost.key
		if err := sink.UpsertCost(CostFact{Provenance: p, CostKey: cost.key, Amount: cost.amount, Currency: cost.currency, CostKind: cost.kind}); err != nil {
			t.Fatal(err)
		}
	}
	projectCost := seat
	projectCost.SourceKey = "project-cost"
	if err := sink.UpsertCost(CostFact{Provenance: projectCost, CostKey: "project-cost", Amount: "5.000000000", Currency: "GBP", CostKind: "charge", ProjectKey: "known-project"}); err != nil {
		t.Fatal(err)
	}

	insertActivity := func(key string, at time.Time) {
		t.Helper()
		_, err := db.ExecContext(ctx, `INSERT INTO ai_ledger_activity_events(observed_at,device_id,org_id,vendor_key,product_key,surface,activity_kind,source_application,activity_count,source_identifier,source_key,freshness_at) VALUES($1,$2,$3,'acme','assistant','desktop','request','Acme',1,$2,$4,$1)`, at, deviceID, orgID, key)
		if err != nil {
			t.Fatal(err)
		}
	}
	insertActivity("endpoint-current", now.Add(-time.Hour))
	insertActivity("endpoint-expired", now.Add(-120*24*time.Hour))
	var endpointBefore int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM ai_ledger_activity_events WHERE org_id=$1`, orgID).Scan(&endpointBefore); err != nil || endpointBefore != 2 {
		t.Fatalf("endpoint fixture failed: count=%d err=%v", endpointBefore, err)
	}
	if err := store.Reconcile(ctx, orgID); err != nil {
		t.Fatal(err)
	}
	var projectEvidence, projectStatus string
	if err := db.QueryRowContext(ctx, `SELECT evidence_level,reconciliation_status FROM ai_ledger_costs WHERE org_id=$1::uuid AND cost_key='project-cost'`, orgID).Scan(&projectEvidence, &projectStatus); err != nil || projectEvidence != "reconciled" || projectStatus != "matched" {
		t.Fatalf("project-only cost was not reconciled: evidence=%q status=%q err=%v", projectEvidence, projectStatus, err)
	}
	var userID string
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM ai_ledger_directory_users WHERE org_id=$1::uuid`, orgID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := store.AssignDeviceUser(ctx, orgID, deviceID, userID, "mdm", "", now.Add(-365*24*time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	if err := store.RefreshFindings(ctx, orgID, now); err != nil {
		t.Fatal(err)
	}

	summary, err := store.Summary(ctx, orgID)
	if err != nil || summary["endpoint_activity_count"] != int64(2) {
		t.Fatalf("summary failed: %#v %v", summary, err)
	}
	products, err := store.Products(ctx, orgID)
	if err != nil || len(products) != 1 || products[0]["endpoint_activity_count"] != int64(2) {
		t.Fatalf("products failed: %#v %v", products, err)
	}
	people, err := store.People(ctx, orgID)
	if err != nil || len(people) != 1 || people[0]["endpoint_activity_count"] != int64(2) {
		t.Fatalf("people failed: %#v %v", people, err)
	}
	sources, err := store.Sources(ctx, orgID)
	if err != nil || len(sources) < 2 {
		t.Fatalf("sources failed: %#v %v", sources, err)
	}
	findings, err := store.Findings(ctx, orgID, "open")
	if err != nil || len(findings) == 0 {
		t.Fatalf("findings failed: %#v %v", findings, err)
	}
	var usdTotal string
	if err := db.QueryRowContext(ctx, `SELECT sum(amount)::text FROM ai_ledger_costs WHERE org_id=$1::uuid AND currency='USD'`, orgID).Scan(&usdTotal); err != nil || usdTotal != "120.000000000" {
		t.Fatalf("authoritative USD total changed: %q %v", usdTotal, err)
	}
	var endpointAfterConnector int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM ai_ledger_activity_events WHERE org_id=$1`, orgID).Scan(&endpointAfterConnector); err != nil || endpointAfterConnector != endpointBefore {
		t.Fatalf("connector ingestion changed endpoint count: before=%d after=%d err=%v", endpointBefore, endpointAfterConnector, err)
	}
	deleted, err := store.DeleteEndpointActivityBefore(ctx, orgID, now.Add(-90*24*time.Hour))
	if err != nil || deleted != 1 {
		t.Fatalf("retention failed: deleted=%d err=%v", deleted, err)
	}
}
