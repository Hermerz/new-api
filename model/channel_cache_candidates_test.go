package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withInMemoryCacheChannels swaps in a test fixture for the channel cache and
// restores originals on cleanup. Channel ids are arbitrary; priorities + groups
// follow each test's parameter.
func withInMemoryCacheChannels(t *testing.T, channels []*Channel) {
	t.Helper()

	origEnabled := common.MemoryCacheEnabled
	channelSyncLock.Lock()
	origIDM := channelsIDM
	origGrpMap := group2model2channels
	channelSyncLock.Unlock()

	common.MemoryCacheEnabled = true

	channelSyncLock.Lock()
	channelsIDM = make(map[int]*Channel)
	group2model2channels = make(map[string]map[string][]int)
	for _, ch := range channels {
		channelsIDM[ch.Id] = ch
		// Per channel, use its Group field (comma-separated) and Models field.
		// For test simplicity we hardcode group="testgroup" + model="testmodel"
		// when caller leaves Channel.Group / Models empty.
		group := ch.Group
		if group == "" {
			group = "testgroup"
		}
		modelName := "testmodel"
		if _, ok := group2model2channels[group]; !ok {
			group2model2channels[group] = make(map[string][]int)
		}
		group2model2channels[group][modelName] = append(group2model2channels[group][modelName], ch.Id)
	}
	channelSyncLock.Unlock()

	t.Cleanup(func() {
		common.MemoryCacheEnabled = origEnabled
		channelSyncLock.Lock()
		channelsIDM = origIDM
		group2model2channels = origGrpMap
		channelSyncLock.Unlock()
	})
}

func mkChannel(id int, priority int64, group string) *Channel {
	p := priority
	return &Channel{
		Id:       id,
		Priority: &p,
		Group:    group,
	}
}

// TestGetAllSatisfiedChannels_PriorityDesc verifies higher priority comes first
// in the returned slice.
func TestGetAllSatisfiedChannels_PriorityDesc(t *testing.T) {
	withInMemoryCacheChannels(t, []*Channel{
		mkChannel(1, 0, "testgroup"),
		mkChannel(2, 100, "testgroup"),
		mkChannel(3, 50, "testgroup"),
	})

	got, err := GetAllSatisfiedChannels("testgroup", "testmodel")
	require.NoError(t, err)
	require.Len(t, got, 3)

	priorities := []int64{got[0].GetPriority(), got[1].GetPriority(), got[2].GetPriority()}
	assert.Equal(t, []int64{100, 50, 0}, priorities, "channels must be sorted priority DESC")
	assert.Equal(t, 2, got[0].Id, "channel with priority 100 must be first")
	assert.Equal(t, 3, got[1].Id)
	assert.Equal(t, 1, got[2].Id)
}

// TestGetAllSatisfiedChannels_AllChannelsInTier verifies that when multiple
// channels share the same priority, they ALL appear in the result (not just
// one weighted-random pick like the legacy retry-indexed selection).
func TestGetAllSatisfiedChannels_AllChannelsInTier(t *testing.T) {
	withInMemoryCacheChannels(t, []*Channel{
		mkChannel(10, 50, "testgroup"),
		mkChannel(11, 50, "testgroup"),
		mkChannel(12, 50, "testgroup"),
		mkChannel(13, 0, "testgroup"),
	})

	got, err := GetAllSatisfiedChannels("testgroup", "testmodel")
	require.NoError(t, err)
	require.Len(t, got, 4, "all 4 channels (3 in priority-50 tier + 1 in priority-0) must appear")

	// First 3 must be the priority-50 tier (any order due to shuffle), last must be priority-0
	firstTierIDs := map[int]bool{got[0].Id: true, got[1].Id: true, got[2].Id: true}
	assert.True(t, firstTierIDs[10] && firstTierIDs[11] && firstTierIDs[12],
		"priority-50 tier must contain channels 10, 11, 12 in any order; got %v", firstTierIDs)
	assert.Equal(t, 13, got[3].Id, "priority-0 channel must come last")
}

// TestGetAllSatisfiedChannels_NoMatch returns nil/nil for unknown group/model.
func TestGetAllSatisfiedChannels_NoMatch(t *testing.T) {
	withInMemoryCacheChannels(t, []*Channel{
		mkChannel(1, 0, "testgroup"),
	})

	got, err := GetAllSatisfiedChannels("nonexistent-group", "testmodel")
	require.NoError(t, err)
	assert.Nil(t, got)

	got, err = GetAllSatisfiedChannels("testgroup", "nonexistent-model")
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestGetAllSatisfiedChannels_SingleChannel returns a 1-element slice.
func TestGetAllSatisfiedChannels_SingleChannel(t *testing.T) {
	withInMemoryCacheChannels(t, []*Channel{
		mkChannel(42, 10, "testgroup"),
	})

	got, err := GetAllSatisfiedChannels("testgroup", "testmodel")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 42, got[0].Id)
}

// TestGetAllSatisfiedChannels_DBFallback covers MemoryCacheEnabled=false path
// (Hermerz/Hermes#78 Codex round 1 Critical 2). Verifies the new
// getAllSatisfiedChannelsFromDB returns all matching enabled channels in
// priority DESC + weight DESC order, deduplicating channel ids.
func TestGetAllSatisfiedChannels_DBFallback(t *testing.T) {
	// AutoMigrate Ability — TestMain skips this since other tests don't use it.
	// Also initialize commonGroupCol since TestMain skips initCol() too.
	require.NoError(t, DB.AutoMigrate(&Ability{}))
	initCol()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM abilities")
		DB.Exec("DELETE FROM channels")
	})

	origEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = origEnabled })

	// Insert 3 channels with distinct (priority, weight).
	channels := []Channel{
		{Id: 100, Name: "lowpri", Status: common.ChannelStatusEnabled, Type: 1},
		{Id: 101, Name: "highpri-heavyweight", Status: common.ChannelStatusEnabled, Type: 1},
		{Id: 102, Name: "highpri-lightweight", Status: common.ChannelStatusEnabled, Type: 1},
	}
	require.NoError(t, DB.Create(&channels).Error)

	abilities := []Ability{
		{Group: "dbtest", Model: "gpt-test", ChannelId: 100, Enabled: true, Priority: int64Ptr(0), Weight: 50},
		{Group: "dbtest", Model: "gpt-test", ChannelId: 101, Enabled: true, Priority: int64Ptr(100), Weight: 90},
		{Group: "dbtest", Model: "gpt-test", ChannelId: 102, Enabled: true, Priority: int64Ptr(100), Weight: 10},
	}
	require.NoError(t, DB.Create(&abilities).Error)

	got, err := GetAllSatisfiedChannels("dbtest", "gpt-test")
	require.NoError(t, err)
	require.Len(t, got, 3, "all 3 channels should be returned")

	// Order: priority DESC + within priority weight DESC.
	assert.Equal(t, 101, got[0].Id, "priority=100 weight=90 must be first")
	assert.Equal(t, 102, got[1].Id, "priority=100 weight=10 must be second")
	assert.Equal(t, 100, got[2].Id, "priority=0 must be last")
}

// TestGetAllSatisfiedChannels_DBFallback_NoMatch returns nil/nil for unknown.
func TestGetAllSatisfiedChannels_DBFallback_NoMatch(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Ability{}))
	// Independently call initCol() so this test can run in isolation; without
	// it commonGroupCol is empty and SQL composition fails. TestMain skips
	// initCol() and DBFallback test above also calls it — duplicating here so
	// test order doesn't matter (Codex round 3 finding).
	initCol()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM abilities")
	})

	origEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = origEnabled })

	got, err := GetAllSatisfiedChannels("nonexistent", "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func int64Ptr(v int64) *int64 { return &v }
