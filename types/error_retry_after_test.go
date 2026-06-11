package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRetryAfterHeaderValue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", (*NewAPIError)(nil).RetryAfterHeaderValue())
	assert.Equal(t, "", (&NewAPIError{}).RetryAfterHeaderValue())
	assert.Equal(t, "30", (&NewAPIError{RetryAfter: "30"}).RetryAfterHeaderValue())
	assert.Equal(t, "30", (&NewAPIError{RetryAfter: " 30 "}).RetryAfterHeaderValue())
	// 数值秒数 cap 到 120，防上游离谱大值
	assert.Equal(t, "120", (&NewAPIError{RetryAfter: "86400"}).RetryAfterHeaderValue())
	assert.Equal(t, "", (&NewAPIError{RetryAfter: "-5"}).RetryAfterHeaderValue())
	// HTTP-date 格式原样透传
	assert.Equal(t, "Wed, 21 Oct 2026 07:28:00 GMT", (&NewAPIError{RetryAfter: "Wed, 21 Oct 2026 07:28:00 GMT"}).RetryAfterHeaderValue())
}
