package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "backend",
		Name:      "http_requests_total",
		Help:      "Total HTTP requests processed by the backend.",
	}, []string{"method", "path", "status"})

	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "backend",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path"})

	ActiveDevicesTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "backend",
		Name:      "active_devices_total",
		Help:      "Number of active devices per org.",
	}, []string{"org"})

	CertsExpiringSoon = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "backend",
		Name:      "certificates_expiring_soon",
		Help:      "Number of certificates expiring within 14 days.",
	}, []string{"org"})

	EnrollmentCompletionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "backend",
		Name:      "enrollment_completions_total",
		Help:      "Total successful enrollment completions.",
	}, []string{"org"})

	CertRenewalsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "backend",
		Name:      "cert_renewals_total",
		Help:      "Total successful certificate renewals.",
	}, []string{"org"})
)
