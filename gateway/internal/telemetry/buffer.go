package telemetry

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/themisto/gateway/internal/metrics"
	"github.com/themisto/gateway/internal/store"
)

type Buffer struct {
	ch       chan Event
	dlpCh    chan DLPEvent
	db       *store.Store
	logger   *slog.Logger
	batch    int
	interval time.Duration
	wg       sync.WaitGroup
	dropped  int64
	mu       sync.Mutex
}

func NewBuffer(db *store.Store, bufferSize, batchSize int, flushInterval time.Duration, logger *slog.Logger) *Buffer {
	return &Buffer{
		ch:       make(chan Event, bufferSize),
		dlpCh:    make(chan DLPEvent, bufferSize),
		db:       db,
		logger:   logger,
		batch:    batchSize,
		interval: flushInterval,
	}
}

func (b *Buffer) Emit(e Event) {
	select {
	case b.ch <- e:
	default:
		b.mu.Lock()
		b.dropped++
		b.mu.Unlock()
		metrics.TelemetryEventsDropped.Inc()
	}
}

func (b *Buffer) EmitDLP(e DLPEvent) {
	select {
	case b.dlpCh <- e:
	default:
		b.mu.Lock()
		b.dropped++
		b.mu.Unlock()
		metrics.TelemetryEventsDropped.Inc()
	}
}

func (b *Buffer) Start(ctx context.Context) {
	b.wg.Add(1)
	go b.run(ctx)
}

func (b *Buffer) Stop() {
	close(b.ch)
	close(b.dlpCh)
	b.wg.Wait()
}

func (b *Buffer) run(ctx context.Context) {
	defer b.wg.Done()

	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	pending := make([]Event, 0, b.batch)
	pendingDLP := make([]DLPEvent, 0, b.batch)

	for {
		select {
		case e, ok := <-b.ch:
			if !ok {
				b.flush(ctx, pending)
				b.flushDLP(ctx, pendingDLP)
				return
			}
			pending = append(pending, e)
			if len(pending) >= b.batch {
				b.flush(ctx, pending)
				pending = pending[:0]
			}
		case e, ok := <-b.dlpCh:
			if !ok {
				continue
			}
			pendingDLP = append(pendingDLP, e)
			if len(pendingDLP) >= b.batch {
				b.flushDLP(ctx, pendingDLP)
				pendingDLP = pendingDLP[:0]
			}
		case <-ticker.C:
			if len(pending) > 0 {
				b.flush(ctx, pending)
				pending = pending[:0]
			}
			if len(pendingDLP) > 0 {
				b.flushDLP(ctx, pendingDLP)
				pendingDLP = pendingDLP[:0]
			}
		}
	}
}

func (b *Buffer) flush(ctx context.Context, events []Event) {
	if len(events) == 0 {
		return
	}

	storeEvents := make([]store.TelemetryEvent, len(events))
	for i, e := range events {
		storeEvents[i] = store.TelemetryEvent{
			Timestamp:       e.Timestamp,
			DeviceID:        e.DeviceID,
			OrgID:           e.OrgID,
			RequestMethod:   e.RequestMethod,
			RequestHost:     e.RequestHost,
			RequestPath:     e.RequestPath,
			RequestPort:     e.RequestPort,
			ResponseStatus:  e.ResponseStatus,
			LatencyMs:       e.LatencyMs,
			BytesSent:       e.BytesSent,
			BytesReceived:   e.BytesReceived,
			PolicyDecision:  e.PolicyDecision,
			MatchedRuleID:   e.MatchedRuleID,
			PolicyVersion:   e.PolicyVersion,
			AgentVersion:    e.AgentVersion,
			ProtocolVersion: e.ProtocolVersion,
			SourceApp:       e.SourceApp,
			OS:              e.OS,
			ServiceCategory: e.ServiceCategory,
			AIVendor:        e.AIVendor,
			CaptureSurface:  e.CaptureSurface,
		}
	}

	if err := b.db.InsertTelemetryBatch(ctx, storeEvents); err != nil {
		b.logger.Error("telemetry flush failed", "count", len(events), "error", err)
	} else {
		auditEntries := make([]store.AuditEntry, 0, len(events))
		for _, e := range events {
			if strings.ToLower(e.PolicyDecision) != "block" {
				continue
			}

			orgID := normalizedAuditOrgID(e.OrgID)
			resourceID := e.RequestHost
			if e.RequestPath != "" {
				resourceID = e.RequestHost + e.RequestPath
			}

			details := map[string]interface{}{
				"device_id":       e.DeviceID,
				"request_method":  e.RequestMethod,
				"request_host":    e.RequestHost,
				"request_path":    e.RequestPath,
				"response_status": e.ResponseStatus,
				"latency_ms":      e.LatencyMs,
				"policy_decision": e.PolicyDecision,
			}
			if e.MatchedRuleID != nil && *e.MatchedRuleID != "" {
				details["matched_rule_id"] = *e.MatchedRuleID
			}
			if orgID == nil && e.OrgID != "" {
				details["org_id_raw"] = e.OrgID
			}

			auditEntries = append(auditEntries, store.AuditEntry{
				Timestamp:    e.Timestamp,
				ActorType:    "gateway",
				ActorID:      "gateway",
				OrgID:        orgID,
				Action:       "policy.blocked",
				ResourceType: "request",
				ResourceID:   resourceID,
				Details:      details,
			})
		}

		if len(auditEntries) > 0 {
			if err := b.db.InsertAuditBatch(ctx, auditEntries); err != nil {
				b.logger.Error("audit flush failed", "count", len(auditEntries), "error", err)
			}
		}

		b.logger.Debug("telemetry flushed", "count", len(events))
	}
}

func (b *Buffer) flushDLP(ctx context.Context, events []DLPEvent) {
	if len(events) == 0 {
		return
	}
	storeEvents := make([]store.DLPEvent, len(events))
	for i, e := range events {
		storeEvents[i] = store.DLPEvent{
			Timestamp:            e.Timestamp,
			DeviceID:             e.DeviceID,
			OrgID:                e.OrgID,
			RequestID:            e.RequestID,
			RequestHost:          e.RequestHost,
			RequestPath:          e.RequestPath,
			RequestMethod:        e.RequestMethod,
			SourceApp:            e.SourceApp,
			ServiceCategory:      e.ServiceCategory,
			AIVendor:             e.AIVendor,
			MatchTypes:           e.MatchTypes,
			MatchedPatterns:      e.MatchedPatterns,
			MatchedFields:        e.MatchedFields,
			MatchCount:           e.MatchCount,
			Severity:             e.Severity,
			ClassificationReason: e.ClassificationReason,
			ContentType:          e.ContentType,
			FileCount:            e.FileCount,
			ActionTaken:          e.ActionTaken,
			PolicyRuleID:         e.PolicyRuleID,
			ReasonCode:           e.ReasonCode,
			ReasonDetail:         e.ReasonDetail,
			Protocol:             e.Protocol,
			InterceptedHTTPS:     e.InterceptedHTTPS,
			InspectionQuality:    e.InspectionQuality,
			InspectionSkipReason: e.InspectionSkipReason,
			Direction:            e.Direction,
			RequestBody:          e.RequestBody,
			RequestBodyTruncated: e.RequestBodyTruncated,
		}
	}
	if err := b.db.InsertDLPBatch(ctx, storeEvents); err != nil {
		b.logger.Error("DLP flush failed", "count", len(events), "error", err)
	} else {
		b.logger.Debug("DLP events flushed", "count", len(events))
	}
}

func normalizedAuditOrgID(raw string) *string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "-")
	if len(parts) != 5 {
		return nil
	}
	expected := []int{8, 4, 4, 4, 12}
	for i, part := range parts {
		if len(part) != expected[i] {
			return nil
		}
		for _, c := range part {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				return nil
			}
		}
	}
	v := raw
	return &v
}
