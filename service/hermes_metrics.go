package service

// hermes_metrics.go — AR8: 5 Prometheus metrics for Hermes observability.
// Metrics are registered at package init and updated by relay/billing code.
// GET /metrics is served by promhttp.Handler() (see router/api-router.go).

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// MetricRequestsTotal counts relay requests, labelled by model and status.
	MetricRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "newapi",
		Subsystem: "relay",
		Name:      "requests_total",
		Help:      "Total relay requests processed.",
	}, []string{"model", "status"})

	// MetricRequestDuration tracks relay request latency in seconds.
	MetricRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "newapi",
		Subsystem: "relay",
		Name:      "request_duration_seconds",
		Help:      "Relay request latency distribution.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"model"})

	// MetricTokensTotal counts prompt + completion tokens consumed.
	MetricTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "newapi",
		Subsystem: "relay",
		Name:      "tokens_total",
		Help:      "Total tokens consumed (prompt + completion).",
	}, []string{"model", "type"}) // type: "prompt" | "completion"

	// MetricUpstreamErrorsTotal counts upstream channel errors.
	MetricUpstreamErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "newapi",
		Subsystem: "relay",
		Name:      "upstream_errors_total",
		Help:      "Total upstream channel errors.",
	}, []string{"channel_type"})

	// MetricActiveKeysCount is a gauge for currently enabled API keys across all channels.
	MetricActiveKeysCount = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "newapi",
		Subsystem: "relay",
		Name:      "active_keys_count",
		Help:      "Number of currently enabled API keys across all channels.",
	})
)

// RecordRelayRequest updates requests_total, request_duration, and tokens_total
// after a relay response is finalized. Call from PostTextConsumeQuota.
func RecordRelayRequest(model, status string, durationSec float64, promptTokens, completionTokens int) {
	MetricRequestsTotal.WithLabelValues(model, status).Inc()
	MetricRequestDuration.WithLabelValues(model).Observe(durationSec)
	if promptTokens > 0 {
		MetricTokensTotal.WithLabelValues(model, "prompt").Add(float64(promptTokens))
	}
	if completionTokens > 0 {
		MetricTokensTotal.WithLabelValues(model, "completion").Add(float64(completionTokens))
	}
}

// RecordUpstreamError increments upstream_errors_total.
func RecordUpstreamError(channelType string) {
	MetricUpstreamErrorsTotal.WithLabelValues(channelType).Inc()
}
