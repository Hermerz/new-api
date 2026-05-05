package service

// hermes_pub.go — AR6: publish billing events to Redis after quota settlement.
// Hermes WebSocket pipeline (Story 6.5) subscribes to ws:pub:{user_id} to push
// real-time usage updates to the user's browser.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
)

const hermesPubKeyPrefix = "ws:pub:"

// BillingEvent is the payload published to Redis after each successful settlement.
// All token counts are integers; Quota uses the same internal unit as wallet balance
// (500000 quota = $1) — NOT a money amount in USD/CNY.
type BillingEvent struct {
	UserID           int    `json:"user_id"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	CacheReadTokens  int    `json:"cache_read_tokens"`
	CacheWriteTokens int    `json:"cache_write_tokens"`
	Quota            int    `json:"quota"` // quota units consumed (same scale as wallet balance)
	Timestamp        int64  `json:"timestamp"`
}

// PublishBillingEvent publishes a billing event to Redis asynchronously.
// Non-fatal: errors are logged but do not affect the billing result.
// For non-text relays without token semantics (mjproxy, violation_fee), pass 0 for token args.
// `quota` is the quota unit consumed by this request (NOT money — see BillingEvent doc).
func PublishBillingEvent(userID int, model string, promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens, quota int) {
	if common.RDB == nil {
		return
	}
	event := BillingEvent{
		UserID:           userID,
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		CacheReadTokens:  cacheReadTokens,
		CacheWriteTokens: cacheWriteTokens,
		Quota:            quota,
		Timestamp:        time.Now().UnixMilli(),
	}
	gopool.Go(func() {
		payload, err := json.Marshal(event)
		if err != nil {
			common.SysError(fmt.Sprintf("hermes_pub marshal error: %v", err))
			return
		}
		key := fmt.Sprintf("%s%d", hermesPubKeyPrefix, userID)
		if err := common.RDB.Publish(context.Background(), key, payload).Err(); err != nil {
			common.SysError(fmt.Sprintf("hermes_pub publish error user_id=%d: %v", userID, err))
		}
	})
}
