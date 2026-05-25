package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// affinityFailedTestCacheKey returns a cache key unique to a test name so
// concurrent / repeated runs do not pollute each other (the affinity cache is
// a package-level singleton initialized via sync.Once).
func affinityFailedTestCacheKey(t *testing.T) string {
	t.Helper()
	return channelAffinityCacheNamespace + ":test:" + t.Name()
}

func newAffinityFailedTestContext(cacheKey string) *gin.Context {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	setChannelAffinityContext(ctx, channelAffinityMeta{
		CacheKey:   cacheKey,
		TTLSeconds: 3600,
		RuleName:   "test-rule",
	})
	return ctx
}

// TestMarkChannelAffinityFailed_DeletesEntry verifies the happy path:
// a populated cache entry is removed after MarkChannelAffinityFailed is called.
func TestMarkChannelAffinityFailed_DeletesEntry(t *testing.T) {
	cacheKey := affinityFailedTestCacheKey(t)
	cache := getChannelAffinityCache()
	require.NoError(t, cache.SetWithTTL(cacheKey, 42, time.Hour))

	_, found, err := cache.Get(cacheKey)
	require.NoError(t, err)
	require.True(t, found, "precondition: cache entry should exist before MarkFailed")

	ctx := newAffinityFailedTestContext(cacheKey)
	MarkChannelAffinityFailed(ctx)

	_, found, err = cache.Get(cacheKey)
	require.NoError(t, err)
	require.False(t, found, "cache entry should be deleted after MarkFailed")
}

// TestMarkChannelAffinityFailed_NoOpWithoutContext verifies that calling
// MarkChannelAffinityFailed on a context with no affinity meta (rule did not
// match) is a safe no-op — does not panic, does not error.
func TestMarkChannelAffinityFailed_NoOpWithoutContext(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	// Intentionally do NOT call setChannelAffinityContext — simulates the
	// "no affinity rule matched this request" path.
	require.NotPanics(t, func() {
		MarkChannelAffinityFailed(ctx)
	})
}

// TestMarkChannelAffinityFailed_NoOpForMissingEntry verifies that calling
// MarkChannelAffinityFailed with affinity context set but no actual cache
// entry (e.g., entry already expired or was never written) is a safe no-op.
func TestMarkChannelAffinityFailed_NoOpForMissingEntry(t *testing.T) {
	cacheKey := affinityFailedTestCacheKey(t)
	cache := getChannelAffinityCache()
	// Ensure no entry exists for this key
	_, _ = cache.DeleteMany([]string{cacheKey})

	ctx := newAffinityFailedTestContext(cacheKey)
	require.NotPanics(t, func() {
		MarkChannelAffinityFailed(ctx)
	})

	_, found, err := cache.Get(cacheKey)
	require.NoError(t, err)
	require.False(t, found)
}

// TestMarkChannelAffinityFailed_NilContext verifies the nil-context guard.
func TestMarkChannelAffinityFailed_NilContext(t *testing.T) {
	require.NotPanics(t, func() {
		MarkChannelAffinityFailed(nil)
	})
}
