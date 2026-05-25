package service

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

// TestRecordChannelSuccess verifies a success increment lands on the
// {channel_id=<n>, outcome="success"} series only (no error breakdown).
func TestRecordChannelSuccess(t *testing.T) {
	MetricChannelRequestsTotal.Reset()
	MetricChannelErrorsTotal.Reset()

	RecordChannelSuccess(42)
	RecordChannelSuccess(42)
	RecordChannelSuccess(7)

	assert.Equal(t, 2.0,
		testutil.ToFloat64(MetricChannelRequestsTotal.WithLabelValues("42", "success")))
	assert.Equal(t, 1.0,
		testutil.ToFloat64(MetricChannelRequestsTotal.WithLabelValues("7", "success")))
	assert.Equal(t, 0.0,
		testutil.ToFloat64(MetricChannelRequestsTotal.WithLabelValues("42", "error")))
}

// TestRecordChannelError verifies error path increments both counters with
// matching channel labels + the right status_code bucket.
func TestRecordChannelError(t *testing.T) {
	MetricChannelRequestsTotal.Reset()
	MetricChannelErrorsTotal.Reset()

	RecordChannelError(42, 503)
	RecordChannelError(42, 503)
	RecordChannelError(42, 401)
	RecordChannelError(7, 500)

	assert.Equal(t, 3.0,
		testutil.ToFloat64(MetricChannelRequestsTotal.WithLabelValues("42", "error")))
	assert.Equal(t, 1.0,
		testutil.ToFloat64(MetricChannelRequestsTotal.WithLabelValues("7", "error")))

	assert.Equal(t, 2.0,
		testutil.ToFloat64(MetricChannelErrorsTotal.WithLabelValues("42", "503")))
	assert.Equal(t, 1.0,
		testutil.ToFloat64(MetricChannelErrorsTotal.WithLabelValues("42", "401")))
	assert.Equal(t, 1.0,
		testutil.ToFloat64(MetricChannelErrorsTotal.WithLabelValues("7", "500")))
}

// TestRecordChannelError_UnknownStatusCode verifies that out-of-range status
// codes (e.g., 0 for network/timeout errors) get bucketed under "unknown"
// rather than producing an empty label value.
func TestRecordChannelError_UnknownStatusCode(t *testing.T) {
	MetricChannelRequestsTotal.Reset()
	MetricChannelErrorsTotal.Reset()

	RecordChannelError(42, 0)
	RecordChannelError(42, -1)
	RecordChannelError(42, 999)

	assert.Equal(t, 3.0,
		testutil.ToFloat64(MetricChannelRequestsTotal.WithLabelValues("42", "error")))
	assert.Equal(t, 3.0,
		testutil.ToFloat64(MetricChannelErrorsTotal.WithLabelValues("42", "unknown")))
}
