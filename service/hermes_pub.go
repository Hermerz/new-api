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
type BillingEvent struct {
	UserID           int    `json:"user_id"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	Cost             int    `json:"cost"` // quota units (same scale as wallet balance)
	Timestamp        int64  `json:"timestamp"`
}

// PublishBillingEvent publishes a billing event to Redis asynchronously.
// Non-fatal: errors are logged but do not affect the billing result.
func PublishBillingEvent(userID int, model string, promptTokens, completionTokens, cost int) {
	if common.RDB == nil {
		return
	}
	event := BillingEvent{
		UserID:           userID,
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		Cost:             cost,
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
