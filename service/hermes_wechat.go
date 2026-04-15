package service

// hermes_wechat.go — Story 4.7: WeChat Work bot webhook alert.
//
// Environment variables:
//   HERMES_WECHAT_WEBHOOK_URL  — WeChat Work bot webhook URL (empty = disabled)
//   HERMES_KEY_POOL_ALERT_THRESHOLD — integer; alert when enabled-channel count
//                                     drops below this value (default: 2)

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
)

// KeyPoolAlertThreshold returns the configured alert threshold (default 2).
func KeyPoolAlertThreshold() int64 {
	v := os.Getenv("HERMES_KEY_POOL_ALERT_THRESHOLD")
	if v == "" {
		return 2
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 1 {
		return 2
	}
	return n
}

// wechatTextMsg is the WeChat Work bot text-message payload.
type wechatTextMsg struct {
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
}

// SendWeChatWorkAlert sends a text alert to the configured WeChat Work bot.
// Non-fatal: errors are logged but do not interrupt the caller.
// Runs asynchronously via gopool.Go so it never blocks the relay path.
func SendWeChatWorkAlert(message string) {
	webhookURL := os.Getenv("HERMES_WECHAT_WEBHOOK_URL")
	if webhookURL == "" {
		return
	}
	gopool.Go(func() {
		if err := sendWeChatWork(webhookURL, message); err != nil {
			common.SysError(fmt.Sprintf("hermes_wechat alert error: %v", err))
		}
	})
}

func sendWeChatWork(webhookURL, message string) error {
	msg := wechatTextMsg{MsgType: "text"}
	msg.Text.Content = message
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook status %d", resp.StatusCode)
	}
	return nil
}
