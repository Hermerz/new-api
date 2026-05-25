package service

// hermes_metrics.go — AR8: 5 Prometheus metrics for Hermes observability.
// Metrics are registered at package init and updated by relay/billing code.
// GET /metrics is served by promhttp.Handler() (see router/api-router.go).

import (
	"strconv"

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

	// MetricChannelRequestsTotal counts relay attempts per channel, labelled by
	// channel_id and outcome ("success" | "error"). Used to compute per-channel
	// success rate for the #37 P1(d) alert and #37 P1(c) admin UI health
	// indicator. Distinct from MetricRequestsTotal (which is per-model, ignores
	// channel) — this is the channel-routing-level view, not the user-facing
	// request-level view.
	//
	// IMPORTANT — granularity difference (alert runbook should call this out):
	// this metric is per-channel-attempt, NOT per-user-request. A user request
	// that retries 2 times before succeeding on the 3rd channel produces 3
	// increments here (2 errors + 1 success, across 3 channel_ids), while
	// MetricRequestsTotal records just 1. So "ch3 success_rate=0% over 5m"
	// here can coexist with "user-level requests succeeded fine" (#78 retry
	// fell back to ch5). The two metrics answer different questions:
	//  - MetricRequestsTotal: "is the customer experience OK?"
	//  - MetricChannelRequestsTotal: "is channel N healthy in isolation?"
	//
	// Cardinality: channels typically < 50 × 2 outcomes = < 100 series per pod.
	MetricChannelRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "newapi",
		Subsystem: "relay",
		Name:      "channel_requests_total",
		Help:      "Total relay attempts per channel, labelled by outcome.",
	}, []string{"channel_id", "outcome"}) // outcome: "success" | "error"

	// MetricChannelErrorsTotal breaks down channel errors by HTTP status code
	// for finer-grained alerting (e.g., 401 = auth broken, 503 = upstream
	// degraded, 504 = upstream timeout). Sum across status_code equals the
	// `outcome="error"` row in MetricChannelRequestsTotal.
	//
	// Cardinality: channels (< 50) × distinct status codes (~10) = < 500 series.
	MetricChannelErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "newapi",
		Subsystem: "relay",
		Name:      "channel_errors_total",
		Help:      "Total relay errors per channel, broken down by upstream HTTP status code.",
	}, []string{"channel_id", "status_code"})
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

// RecordChannelSuccess increments channel_requests_total{outcome="success"}.
// Call when a relay attempt for the given channel returned successfully — i.e.,
// no upstream error. Hermerz/Hermes#37 P1(b).
func RecordChannelSuccess(channelID int) {
	MetricChannelRequestsTotal.WithLabelValues(strconv.Itoa(channelID), "success").Inc()
}

// RecordChannelError increments channel_requests_total{outcome="error"} and
// channel_errors_total{status_code=<code>}. statusCode is the HTTP status code
// returned by the upstream channel (or 0 if not an HTTP-shaped error — coded as
// "unknown" to avoid an empty label value).
// Hermerz/Hermes#37 P1(b).
func RecordChannelError(channelID int, statusCode int) {
	channelLabel := strconv.Itoa(channelID)
	MetricChannelRequestsTotal.WithLabelValues(channelLabel, "error").Inc()
	codeLabel := "unknown"
	if statusCode >= 100 && statusCode <= 599 {
		codeLabel = strconv.Itoa(statusCode)
	}
	MetricChannelErrorsTotal.WithLabelValues(channelLabel, codeLabel).Inc()
}
