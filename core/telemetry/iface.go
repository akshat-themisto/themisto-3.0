// Package telemetry defines metrics and event reporting.
package telemetry

import "github.com/themisto/agent/core/domain"

// Metrics records counters, gauges, and histograms.
type Metrics interface {
	Counter(name string, value int64, labels map[string]string)
	Gauge(name string, value float64, labels map[string]string)
	Histogram(name string, value float64, labels map[string]string)
}

// Events emits discrete events (e.g. agent started, policy updated).
type Events interface {
	Emit(event string, payload *domain.EventPayload) error
}
