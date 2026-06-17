package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "gateway",
		Name:      "requests_total",
		Help:      "Total number of requests processed by the gateway.",
	}, []string{"org", "decision", "status"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "gateway",
		Name:      "request_duration_seconds",
		Help:      "Histogram of request latencies in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"org"})

	TelemetryEventsDropped = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "gateway",
		Name:      "telemetry_events_dropped_total",
		Help:      "Total number of telemetry events dropped due to buffer overflow.",
	})

	ActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "gateway",
		Name:      "active_connections",
		Help:      "Current number of active client connections.",
	})

	RevocationCheckErrors = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "gateway",
		Name:      "revocation_check_errors_total",
		Help:      "Total number of certificate revocation check errors.",
	})

	DLPProtocolScans = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "gateway",
		Name:      "dlp_protocol_scans_total",
		Help:      "Total DLP scans grouped by protocol.",
	}, []string{"protocol", "quality"})
)
