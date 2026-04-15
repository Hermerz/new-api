package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetCacheStats wipes the in-memory map before each test.
func resetCacheStats(t *testing.T) {
	t.Helper()
	CacheUserCacheStatsLock.Lock()
	CacheUserCacheStats = make(map[string]*UserCacheStats)
	CacheUserCacheStatsLock.Unlock()
	// Clean the DB table.
	t.Cleanup(func() {
		DB.Exec("DELETE FROM user_cache_stats")
		CacheUserCacheStatsLock.Lock()
		CacheUserCacheStats = make(map[string]*UserCacheStats)
		CacheUserCacheStatsLock.Unlock()
	})
}

// ── In-memory aggregation ─────────────────────────────────────────────────────

// TestLogUserCacheStats_NewEntry verifies a first request creates a stats entry.
func TestLogUserCacheStats_NewEntry(t *testing.T) {
	resetCacheStats(t)

	ts := time.Now().Unix()
	LogUserCacheStats(1, "alice", "gpt-4o", 1000, 200, 500, ts)

	CacheUserCacheStatsLock.Lock()
	defer CacheUserCacheStatsLock.Unlock()

	assert.Len(t, CacheUserCacheStats, 1)
	for _, s := range CacheUserCacheStats {
		assert.EqualValues(t, 1, s.TotalRequests)
		assert.EqualValues(t, 1000, s.PromptTokens)
		assert.EqualValues(t, 200, s.CachedTokens)
		assert.EqualValues(t, 500, s.CompletionTokens)
		assert.EqualValues(t, 1, s.CacheHitCount, "cacheTokens>0 → hit")
	}
}

// TestLogUserCacheStats_NoCacheTokens verifies cache_hit_count stays 0 when
// cacheTokens == 0 (prompt-cache miss).
func TestLogUserCacheStats_NoCacheTokens(t *testing.T) {
	resetCacheStats(t)

	ts := time.Now().Unix()
	LogUserCacheStats(1, "alice", "gpt-4o", 800, 0, 400, ts)

	CacheUserCacheStatsLock.Lock()
	defer CacheUserCacheStatsLock.Unlock()

	for _, s := range CacheUserCacheStats {
		assert.EqualValues(t, 0, s.CacheHitCount, "no cached tokens → no hit")
	}
}

// TestLogUserCacheStats_Aggregation verifies two requests in the same hour bucket
// accumulate correctly.
func TestLogUserCacheStats_Aggregation(t *testing.T) {
	resetCacheStats(t)

	// Both timestamps truncated to the same hour.
	hourBase := time.Now().Truncate(time.Hour).Unix()
	LogUserCacheStats(1, "alice", "gpt-4o", 1000, 200, 500, hourBase+10)
	LogUserCacheStats(1, "alice", "gpt-4o", 600, 0, 300, hourBase+20)

	CacheUserCacheStatsLock.Lock()
	defer CacheUserCacheStatsLock.Unlock()

	assert.Len(t, CacheUserCacheStats, 1, "same user+model+hour → single bucket")
	for _, s := range CacheUserCacheStats {
		assert.EqualValues(t, 2, s.TotalRequests)
		assert.EqualValues(t, 1600, s.PromptTokens)
		assert.EqualValues(t, 200, s.CachedTokens)
		assert.EqualValues(t, 800, s.CompletionTokens)
		assert.EqualValues(t, 1, s.CacheHitCount, "only first request had cache tokens")
	}
}

// TestLogUserCacheStats_DifferentHour verifies requests in different hours create
// separate buckets.
func TestLogUserCacheStats_DifferentHour(t *testing.T) {
	resetCacheStats(t)

	hour1 := time.Now().Truncate(time.Hour).Unix()
	hour2 := hour1 + 3600
	LogUserCacheStats(1, "alice", "gpt-4o", 500, 100, 200, hour1+5)
	LogUserCacheStats(1, "alice", "gpt-4o", 500, 100, 200, hour2+5)

	CacheUserCacheStatsLock.Lock()
	defer CacheUserCacheStatsLock.Unlock()

	assert.Len(t, CacheUserCacheStats, 2, "different hours → two buckets")
}

// TestLogUserCacheStats_DifferentModel verifies different models produce separate
// buckets even for the same user+hour.
func TestLogUserCacheStats_DifferentModel(t *testing.T) {
	resetCacheStats(t)

	ts := time.Now().Unix()
	LogUserCacheStats(1, "alice", "gpt-4o", 500, 100, 200, ts)
	LogUserCacheStats(1, "alice", "claude-3-5-sonnet", 500, 100, 200, ts)

	CacheUserCacheStatsLock.Lock()
	defer CacheUserCacheStatsLock.Unlock()

	assert.Len(t, CacheUserCacheStats, 2)
}

// ── DB round-trip ─────────────────────────────────────────────────────────────

// TestSaveAndGetUserCacheStats verifies the full in-memory → DB → query cycle.
func TestSaveAndGetUserCacheStats(t *testing.T) {
	resetCacheStats(t)

	hourBase := time.Now().Truncate(time.Hour).Unix()
	LogUserCacheStats(7, "bob", "gpt-4o", 2000, 400, 1000, hourBase)
	LogUserCacheStats(7, "bob", "gpt-4o", 1000, 0, 500, hourBase+30)
	// Different user — should NOT appear in bob's query.
	LogUserCacheStats(8, "carol", "gpt-4o", 500, 50, 250, hourBase)

	SaveUserCacheStatsCache()

	stats, err := GetUserCacheStatsByUserId(7, hourBase-1, hourBase+7200)
	require.NoError(t, err)
	require.Len(t, stats, 1, "bob has one hour-bucket for gpt-4o")

	s := stats[0]
	assert.EqualValues(t, 7, s.UserID)
	assert.EqualValues(t, 2, s.TotalRequests)
	assert.EqualValues(t, 3000, s.PromptTokens)
	assert.EqualValues(t, 400, s.CachedTokens)
	assert.EqualValues(t, 1500, s.CompletionTokens)
	assert.EqualValues(t, 1, s.CacheHitCount)
}

// TestSaveUserCacheStatsCache_Incremental verifies that calling Save twice
// (with fresh data in memory) does an atomic increment on the DB row rather
// than double-writing.
func TestSaveUserCacheStatsCache_Incremental(t *testing.T) {
	resetCacheStats(t)

	hourBase := time.Now().Truncate(time.Hour).Unix()

	// First batch.
	LogUserCacheStats(9, "dan", "gpt-4o", 1000, 200, 500, hourBase)
	SaveUserCacheStatsCache()

	// Second batch (memory was wiped by Save).
	LogUserCacheStats(9, "dan", "gpt-4o", 600, 0, 300, hourBase)
	SaveUserCacheStatsCache()

	stats, err := GetUserCacheStatsByUserId(9, hourBase-1, hourBase+3600)
	require.NoError(t, err)
	require.Len(t, stats, 1)

	s := stats[0]
	assert.EqualValues(t, 2, s.TotalRequests, "incremental save must not reset count")
	assert.EqualValues(t, 1600, s.PromptTokens)
	assert.EqualValues(t, 200, s.CachedTokens)
	assert.EqualValues(t, 800, s.CompletionTokens)
	assert.EqualValues(t, 1, s.CacheHitCount)
}

// TestGetUserCacheStatsByUserId_EmptyRange verifies an empty slice (not error) when
// no records exist for the queried time range.
func TestGetUserCacheStatsByUserId_EmptyRange(t *testing.T) {
	resetCacheStats(t)

	stats, err := GetUserCacheStatsByUserId(999, 0, 1)
	require.NoError(t, err)
	assert.Empty(t, stats)
}
