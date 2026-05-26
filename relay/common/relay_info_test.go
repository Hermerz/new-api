package common

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

// TestResetForChannelRetry_ClearsPerAttemptFields verifies that every field
// documented as per-attempt state in ResetForChannelRetry's doc returns to its
// zero value after the call. Hermerz/Hermes#79.
func TestResetForChannelRetry_ClearsPerAttemptFields(t *testing.T) {
	info := &RelayInfo{
		// Channel-config overrides (Codex H-2)
		RuntimeHeadersOverride:    map[string]interface{}{"X-Trace": "abc"},
		UseRuntimeHeadersOverride: true,
		ParamOverrideAudit:        []string{"replace .temperature 0.7 -> 0.2"},

		// Conversion bookkeeping (Codex H-2)
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,

		// Per-attempt streaming counters
		FirstResponseTime:     time.Now(),
		isFirstResponse:       true,
		SendResponseCount:     7,
		ReceivedResponseCount: 9,
		StreamStatus:          NewStreamStatus(),
		AudioUsage:            true,

		// Intermediate conversion state — only ThinkingContentInfo (value type)
		// is reset; pointer-embedded ClaudeConvertInfo/ResponsesUsageInfo are
		// intentionally preserved (nilling them would panic OpenAI stream path
		// when it reads promoted ClaudeConvertInfo fields on a Claude-format
		// request retried onto an OpenAI channel — Codex review finding).
		ThinkingContentInfo: ThinkingContentInfo{IsFirstThinkingContent: true},
	}

	info.ResetForChannelRetry()

	// Channel-config overrides
	require.Nil(t, info.RuntimeHeadersOverride)
	require.False(t, info.UseRuntimeHeadersOverride)
	require.Nil(t, info.ParamOverrideAudit)

	// Conversion bookkeeping
	require.Nil(t, info.RequestConversionChain)
	require.Equal(t, types.RelayFormat(""), info.FinalRequestRelayFormat)

	// Per-attempt streaming counters
	require.True(t, info.FirstResponseTime.IsZero())
	require.False(t, info.isFirstResponse)
	require.Equal(t, 0, info.SendResponseCount)
	require.Equal(t, 0, info.ReceivedResponseCount)
	require.Nil(t, info.StreamStatus)
	require.False(t, info.AudioUsage)

	// ThinkingContentInfo (value type) zeroed
	require.Equal(t, ThinkingContentInfo{}, info.ThinkingContentInfo)
}

// TestResetForChannelRetry_PreservesPointerEmbeddedStructs verifies that
// *ClaudeConvertInfo and *ResponsesUsageInfo are intentionally NOT nilled
// by the reset, because adaptors (notably OpenAI stream path) deref their
// promoted fields without nil-check and would panic on retry of a
// Claude-format request onto an OpenAI channel. Hermerz/Hermes#79.
func TestResetForChannelRetry_PreservesPointerEmbeddedStructs(t *testing.T) {
	claudeInfo := &ClaudeConvertInfo{LastMessagesType: LastMessageTypeText, Index: 5}
	responsesInfo := &ResponsesUsageInfo{BuiltInTools: map[string]*BuildInToolInfo{"web_search": {ToolName: "web_search"}}}
	info := &RelayInfo{
		ClaudeConvertInfo:  claudeInfo,
		ResponsesUsageInfo: responsesInfo,
	}

	info.ResetForChannelRetry()

	require.Same(t, claudeInfo, info.ClaudeConvertInfo)
	require.Same(t, responsesInfo, info.ResponsesUsageInfo)
}

// TestResetForChannelRetry_PreservesCrossAttemptFields verifies that fields
// documented as cross-attempt invariants are NOT touched by ResetForChannelRetry.
// Hermerz/Hermes#79.
func TestResetForChannelRetry_PreservesCrossAttemptFields(t *testing.T) {
	startTime := time.Now().Add(-time.Minute)
	channelMeta := &ChannelMeta{ChannelId: 42, ChannelBaseUrl: "https://example.com"}
	priorErr := types.NewError(nil, types.ErrorCodeBadResponseStatusCode)
	info := &RelayInfo{
		// Identity (cross-attempt)
		TokenId:    100,
		TokenKey:   "tok-key",
		TokenGroup: "internal",
		UserId:     2,
		UserGroup:  "internal",
		UsingGroup: "internal",
		UserEmail:  "user@example.com",
		UserQuota:  500_000,

		// Request-level
		StartTime:       startTime,
		RequestId:       "req-xyz",
		IsStream:        true,
		RelayMode:       1,
		OriginModelName: "gpt-5.5",
		RequestURLPath:  "/v1/responses",
		RelayFormat:     types.RelayFormatOpenAIResponses,
		ReasoningEffort: "medium",

		// Retry-loop managed
		RetryIndex: 3,
		LastError:  priorErr,

		// ChannelMeta is reset by InitChannelMeta separately; ResetForChannelRetry
		// must not touch it.
		ChannelMeta: channelMeta,

		// Billing session
		BillingSource: "wallet",
	}

	info.ResetForChannelRetry()

	// Identity preserved
	require.Equal(t, 100, info.TokenId)
	require.Equal(t, "tok-key", info.TokenKey)
	require.Equal(t, "internal", info.TokenGroup)
	require.Equal(t, 2, info.UserId)
	require.Equal(t, "internal", info.UserGroup)
	require.Equal(t, "internal", info.UsingGroup)
	require.Equal(t, "user@example.com", info.UserEmail)
	require.Equal(t, 500_000, info.UserQuota)

	// Request-level preserved
	require.Equal(t, startTime, info.StartTime)
	require.Equal(t, "req-xyz", info.RequestId)
	require.True(t, info.IsStream)
	require.Equal(t, 1, info.RelayMode)
	require.Equal(t, "gpt-5.5", info.OriginModelName)
	require.Equal(t, "/v1/responses", info.RequestURLPath)
	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.RelayFormat)
	require.Equal(t, "medium", info.ReasoningEffort)

	// Retry-loop managed fields preserved (caller updates them explicitly)
	require.Equal(t, 3, info.RetryIndex)
	require.Equal(t, priorErr, info.LastError)

	// ChannelMeta preserved (reset by InitChannelMeta, not by us)
	require.Same(t, channelMeta, info.ChannelMeta)

	// Billing preserved
	require.Equal(t, "wallet", info.BillingSource)
}

// TestResetForChannelRetry_NoPanicOnFresh verifies that calling
// ResetForChannelRetry on a freshly-constructed RelayInfo (all per-attempt
// fields already at zero) is a safe no-op. Important because the retry loop
// may call this defensively at the start of each candidate iteration.
func TestResetForChannelRetry_NoPanicOnFresh(t *testing.T) {
	info := &RelayInfo{}
	require.NotPanics(t, func() {
		info.ResetForChannelRetry()
	})

	// Confirm fields are still zero-valued
	require.Nil(t, info.RuntimeHeadersOverride)
	require.False(t, info.UseRuntimeHeadersOverride)
	require.Nil(t, info.RequestConversionChain)
	require.Equal(t, types.RelayFormat(""), info.FinalRequestRelayFormat)
	require.Equal(t, 0, info.SendResponseCount)
}
