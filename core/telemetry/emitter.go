package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
)

// httpDoer is a narrow interface satisfied by transport.GatewayClient so the
// telemetry package avoids importing transport (prevents import cycle).
type httpDoer interface {
	Do(ctx context.Context, req *domain.HTTPRequest) (*domain.HTTPResponse, error)
}

// Emitter periodically flushes the Collector's ring buffer to the gateway.
type Emitter struct {
	collector  *Collector
	client     httpDoer
	gatewayURL string
	interval   time.Duration
	log        log.Logger
}

// NewEmitter creates an emitter that flushes collector to gatewayURL/telemetry.
func NewEmitter(c *Collector, client httpDoer, gatewayURL string, interval time.Duration, logger log.Logger) *Emitter {
	return &Emitter{
		collector:  c,
		client:     client,
		gatewayURL: gatewayURL,
		interval:   interval,
		log:        logger,
	}
}

// Run starts the flush loop. It blocks until ctx is cancelled, then performs
// a final synchronous flush.
func (e *Emitter) Run(ctx context.Context) {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.finalFlush()
			return
		case <-ticker.C:
			e.flush(ctx)
		}
	}
}

func (e *Emitter) flush(ctx context.Context) {
	entries := e.collector.Drain()
	if len(entries) == 0 {
		return
	}

	if err := e.send(ctx, entries); err != nil {
		e.log.Warn("telemetry flush failed", "entries", len(entries), "error", err)
	} else {
		e.log.Debug("telemetry flushed", "entries", len(entries))
	}
}

func (e *Emitter) finalFlush() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e.flush(ctx)
}

// telemetryBatch is the JSON payload sent to the gateway.
type telemetryBatch struct {
	Metrics []metricEntry `json:"metrics,omitempty"`
	Events  []eventEntry  `json:"events,omitempty"`
}

type metricEntry struct {
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
	Labels    map[string]string `json:"labels,omitempty"`
	Timestamp time.Time         `json:"ts"`
}

type eventEntry struct {
	Name      string               `json:"name"`
	Payload   *domain.EventPayload `json:"payload,omitempty"`
	Timestamp time.Time            `json:"ts"`
}

func (e *Emitter) send(ctx context.Context, entries []entry) error {
	var batch telemetryBatch
	for _, ent := range entries {
		switch ent.Kind {
		case kindEvent:
			batch.Events = append(batch.Events, eventEntry{
				Name:      ent.Name,
				Payload:   ent.Payload,
				Timestamp: ent.Timestamp,
			})
		default:
			kind := "counter"
			val := float64(ent.IValue)
			switch ent.Kind {
			case kindGauge:
				kind = "gauge"
				val = ent.FValue
			case kindHistogram:
				kind = "histogram"
				val = ent.FValue
			}
			batch.Metrics = append(batch.Metrics, metricEntry{
				Kind:      kind,
				Name:      ent.Name,
				Value:     val,
				Labels:    ent.Labels,
				Timestamp: ent.Timestamp,
			})
		}
	}

	body, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("marshal batch: %w", err)
	}

	resp, err := e.client.Do(ctx, &domain.HTTPRequest{
		Method:  "POST",
		URL:     e.gatewayURL + "/telemetry",
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    body,
	})
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("gateway returned %d", resp.StatusCode)
	}
	return nil
}
