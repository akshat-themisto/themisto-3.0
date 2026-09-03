package api

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/themisto/backend/internal/ailedger"
	"github.com/themisto/backend/internal/ailedger/adapters/builtin"
	"github.com/themisto/backend/internal/store"
)

func (s *Server) handleAILedgerSummary(w http.ResponseWriter, r *http.Request) {
	s.aiLedgerJSON(w, r, func(ctx context.Context, orgID string) (interface{}, error) {
		return s.aiLedgerStore.Summary(ctx, orgID)
	})
}

func (s *Server) handleAILedgerProducts(w http.ResponseWriter, r *http.Request) {
	s.aiLedgerJSON(w, r, func(ctx context.Context, orgID string) (interface{}, error) {
		return s.aiLedgerStore.Products(ctx, orgID)
	})
}

func (s *Server) handleAILedgerPeople(w http.ResponseWriter, r *http.Request) {
	s.aiLedgerJSON(w, r, func(ctx context.Context, orgID string) (interface{}, error) {
		return s.aiLedgerStore.People(ctx, orgID)
	})
}

func (s *Server) handleAILedgerFindings(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	rows, err := s.aiLedgerStore.Findings(r.Context(), user.OrgID, strings.TrimSpace(r.URL.Query().Get("status")))
	if err != nil {
		s.aiLedgerError(w, "list findings", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": rows, "insights_only": true})
}

func (s *Server) handleAILedgerSources(w http.ResponseWriter, r *http.Request) {
	s.aiLedgerJSON(w, r, func(ctx context.Context, orgID string) (interface{}, error) {
		return s.aiLedgerStore.Sources(ctx, orgID)
	})
}

func (s *Server) handleAILedgerDirectoryUsers(w http.ResponseWriter, r *http.Request) {
	s.aiLedgerJSON(w, r, func(ctx context.Context, orgID string) (interface{}, error) {
		return s.aiLedgerStore.DirectoryUsers(ctx, orgID)
	})
}

func (s *Server) aiLedgerJSON(w http.ResponseWriter, r *http.Request, query func(context.Context, string) (interface{}, error)) {
	user := getUserFromContext(r)
	data, err := query(r.Context(), user.OrgID)
	if err != nil {
		s.aiLedgerError(w, "query AI Ledger", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": data, "insights_only": true})
}

func (s *Server) handleAILedgerConnectorTypes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": s.aiLedgerManager.ConnectorTypes()})
}

type createAILedgerConnectorRequest struct {
	AdapterKey  string                   `json:"adapter_key"`
	DisplayName string                   `json:"display_name"`
	Credentials ailedger.Credentials     `json:"credentials"`
	Config      ailedger.ConnectorConfig `json:"config"`
}

func (s *Server) handleAILedgerCreateConnector(w http.ResponseWriter, r *http.Request) {
	if s.aiLedgerCipherErr != nil {
		writeError(w, http.StatusServiceUnavailable, "CONNECTOR_ENCRYPTION_UNAVAILABLE", s.aiLedgerCipherErr.Error())
		return
	}
	var request createAILedgerConnectorRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid connector JSON")
		return
	}
	user := getUserFromContext(r)
	record, err := s.aiLedgerManager.CreateConnector(
		r.Context(), user.OrgID, request.AdapterKey, request.DisplayName,
		request.Credentials, request.Config,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "CONNECTOR_INVALID", err.Error())
		return
	}
	s.aiLedgerAudit(r, "ai_ledger.connector.created", "ai_ledger_connector", record.ID, map[string]interface{}{"adapter_key": record.AdapterKey})
	writeJSON(w, http.StatusCreated, record)
}

func (s *Server) handleAILedgerConnectorStatus(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	record, err := s.aiLedgerManager.GetConnector(r.Context(), user.OrgID, r.PathValue("connectorID"))
	if err != nil {
		s.aiLedgerError(w, "get connector", err)
		return
	}
	if record == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "connector not found")
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) handleAILedgerSyncConnector(w http.ResponseWriter, r *http.Request) {
	if s.aiLedgerCipherErr != nil {
		writeError(w, http.StatusServiceUnavailable, "CONNECTOR_ENCRYPTION_UNAVAILABLE", s.aiLedgerCipherErr.Error())
		return
	}
	user := getUserFromContext(r)
	connectorID := r.PathValue("connectorID")
	if err := s.aiLedgerManager.SyncConnector(r.Context(), user.OrgID, connectorID); err != nil {
		s.logger.Error("sync AI Ledger connector", "connector_id", connectorID, "error", err)
		writeError(w, http.StatusBadGateway, "CONNECTOR_SYNC_FAILED", err.Error())
		return
	}
	s.aiLedgerAudit(r, "ai_ledger.connector.synced", "ai_ledger_connector", connectorID, nil)
	record, _ := s.aiLedgerManager.GetConnector(r.Context(), user.OrgID, connectorID)
	writeJSON(w, http.StatusOK, record)
}

type assignDeviceUserRequest struct {
	DeviceID         string     `json:"device_id"`
	DirectoryUserID  string     `json:"directory_user_id"`
	AssignmentSource string     `json:"assignment_source"`
	AssignedFrom     *time.Time `json:"assigned_from,omitempty"`
	AssignedUntil    *time.Time `json:"assigned_until,omitempty"`
}

func (s *Server) handleAILedgerAssignDeviceUser(w http.ResponseWriter, r *http.Request) {
	var request assignDeviceUserRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid assignment JSON")
		return
	}
	user := getUserFromContext(r)
	from := time.Now().UTC()
	if request.AssignedFrom != nil {
		from = request.AssignedFrom.UTC()
	}
	if err := s.aiLedgerStore.AssignDeviceUser(
		r.Context(), user.OrgID, request.DeviceID, request.DirectoryUserID,
		request.AssignmentSource, user.ID, from, request.AssignedUntil,
	); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ASSIGNMENT", err.Error())
		return
	}
	s.aiLedgerAudit(r, "ai_ledger.device_user.assigned", "device", request.DeviceID, map[string]interface{}{
		"directory_user_id": request.DirectoryUserID, "assignment_source": request.AssignmentSource,
	})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "assigned"})
}

type csvImportRequest struct {
	CSVData          string `json:"csv_data"`
	SourceIdentifier string `json:"source_identifier,omitempty"`
}

func decodeCSVImportRequest(w http.ResponseWriter, r *http.Request) (csvImportRequest, error) {
	var request csvImportRequest
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&request)
	if err != nil {
		return request, err
	}
	if strings.TrimSpace(request.CSVData) == "" {
		return request, fmt.Errorf("csv_data is required")
	}
	return request, nil
}

func (s *Server) handleAILedgerCSVValidate(w http.ResponseWriter, r *http.Request) {
	request, err := decodeCSVImportRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	validation, validationErr := builtin.ValidateCSV(request.CSVData)
	hash := sha256.Sum256([]byte(request.CSVData))
	result := map[string]interface{}{
		"schema_version": validation.SchemaVersion, "record_count": validation.RecordCount,
		"errors": validation.Errors, "content_hash": hex.EncodeToString(hash[:]),
		"valid": validationErr == nil,
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAILedgerCSVCommit(w http.ResponseWriter, r *http.Request) {
	request, err := decodeCSVImportRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	validation, err := builtin.ValidateCSV(request.CSVData)
	if err != nil {
		writeError(w, http.StatusBadRequest, "CSV_INVALID", err.Error())
		return
	}
	user := getUserFromContext(r)
	hashBytes := sha256.Sum256([]byte(request.CSVData))
	hash := hex.EncodeToString(hashBytes[:])
	source := strings.TrimSpace(request.SourceIdentifier)
	if source == "" {
		source = "csv:" + hash
	}
	adapter := builtin.CSV()
	ctx := ailedger.WithConnectorMaterial(r.Context(), ailedger.ConnectorMaterial{
		Config: ailedger.ConnectorConfig{"csv_data": request.CSVData}, OrgID: user.OrgID,
	}, nil)
	sink := ailedger.NewImportFactSink(s.aiLedgerStore.FactSink(user.OrgID, source), source, time.Now().UTC())
	if _, err := adapter.Sync(ctx, ailedger.Checkpoint{}, sink); err != nil {
		writeError(w, http.StatusBadRequest, "CSV_COMMIT_FAILED", err.Error())
		return
	}
	if err := s.aiLedgerStore.Reconcile(r.Context(), user.OrgID); err != nil {
		s.aiLedgerError(w, "reconcile CSV import", err)
		return
	}
	if err := s.aiLedgerStore.RefreshFindings(r.Context(), user.OrgID, time.Now().UTC()); err != nil {
		s.aiLedgerError(w, "refresh findings", err)
		return
	}
	importID, err := s.aiLedgerStore.RecordImport(
		r.Context(), user.OrgID, "csv", validation.SchemaVersion, hash, source,
		validation.RecordCount, "committed", nil,
	)
	if err != nil {
		s.aiLedgerError(w, "record CSV import", err)
		return
	}
	s.aiLedgerAudit(r, "ai_ledger.csv.committed", "ai_ledger_import", importID, map[string]interface{}{"record_count": validation.RecordCount, "content_hash": hash})
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": importID, "record_count": validation.RecordCount, "content_hash": hash})
}

func (s *Server) handleAILedgerExport(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	products, err := s.aiLedgerStore.Products(r.Context(), user.OrgID)
	if err != nil {
		s.aiLedgerError(w, "export AI Ledger", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="themisto-ai-ledger.csv"`)
	writer := csv.NewWriter(w)
	_ = writer.Write(aiLedgerExportFields)
	for _, product := range products {
		costs, _ := json.Marshal(product["cost_totals"])
		endpointEvidence := ""
		if fmt.Sprint(product["endpoint_activity_count"]) != "0" {
			endpointEvidence = "observed"
		}
		connectorSourceEvidence := ""
		connectorEvidence := ""
		connectorReconciliation := ""
		if fmt.Sprint(product["connector_activity_record_count"]) != "0" {
			connectorSourceEvidence = fmt.Sprint(product["connector_source_evidence_level"])
			connectorEvidence = fmt.Sprint(product["connector_evidence_level"])
			connectorReconciliation = fmt.Sprint(product["connector_reconciliation_status"])
		}
		_ = writer.Write([]string{
			fmt.Sprint(product["vendor_key"]), fmt.Sprint(product["product_key"]),
			fmt.Sprint(product["display_name"]), fmt.Sprint(product["functional_category"]),
			strconv.FormatBool(product["approved"] == true), fmt.Sprint(product["endpoint_activity_count"]),
			fmt.Sprint(product["connector_activity_record_count"]), fmt.Sprint(product["paid_seat_count"]),
			string(costs), endpointEvidence, connectorSourceEvidence, connectorEvidence, connectorReconciliation,
		})
	}
	writer.Flush()
}

var aiLedgerExportFields = []string{
	"vendor_key", "product_key", "display_name", "functional_category", "approved",
	"endpoint_activity_count", "connector_activity_record_count", "paid_seat_count",
	"cost_totals_by_currency", "endpoint_evidence_level", "connector_source_evidence_level",
	"connector_evidence_level", "connector_reconciliation_status",
}

func (s *Server) aiLedgerError(w http.ResponseWriter, operation string, err error) {
	s.logger.Error(operation, "error", err)
	writeError(w, http.StatusInternalServerError, "AI_LEDGER_INTERNAL", "AI Ledger operation failed")
}

func (s *Server) aiLedgerAudit(r *http.Request, action, resourceType, resourceID string, details map[string]interface{}) {
	user := getUserFromContext(r)
	orgID := user.OrgID
	_ = s.store.InsertAudit(r.Context(), nil, &store.AuditEntry{
		ActorType: "admin_user", ActorID: user.ID, OrgID: &orgID,
		Action: action, ResourceType: resourceType, ResourceID: resourceID,
		Details: details, IPAddress: clientIPFromRequest(r),
	})
}
