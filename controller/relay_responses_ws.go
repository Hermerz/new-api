package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	wsReadTimeout   = 30 * time.Second
	maxBufSize      = 1 << 20 // 1 MiB
)

// wsResponseWriter intercepts SSE writes (from gin's c.Render) and forwards
// each "data:" JSON line as a WebSocket text message to Codex CLI.
type wsResponseWriter struct {
	gin.ResponseWriter
	ws     *websocket.Conn
	buf    bytes.Buffer
	closed bool // set when the WS write side is no longer usable
}

func (w *wsResponseWriter) Write(data []byte) (int, error) {
	if w.closed {
		return len(data), nil
	}

	// Cap buffer growth from a stuck/misbehaving upstream. Silently resetting
	// would split an SSE event mid-line and corrupt subsequent parsing, so
	// instead close the connection and stop forwarding.
	if w.buf.Len() > maxBufSize {
		logger.LogError(nil, "ws response buffer overflow, closing connection")
		_ = w.ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "buffer overflow"))
		w.closed = true
		w.buf.Reset()
		return len(data), nil
	}

	// Normalize \r\n\r\n → \n\n (RFC-compliant SSE line endings)
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")

	w.buf.WriteString(normalized)
	for {
		s := w.buf.String()
		idx := strings.Index(s, "\n\n")
		if idx == -1 {
			break
		}
		event := s[:idx]
		// advance past event + separator
		_ = w.buf.Next(idx + 2)

		for _, line := range strings.Split(event, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "data: ") {
				jsonData := strings.TrimPrefix(trimmed, "data: ")
				if jsonData != "[DONE]" {
					if err := w.ws.WriteMessage(websocket.TextMessage, []byte(jsonData)); err != nil {
						logger.LogError(nil, "ws response write error: "+err.Error())
						w.closed = true
						return len(data), nil
					}
				}
			}
		}
	}
	return len(data), nil
}

func (w *wsResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *wsResponseWriter) WriteHeader(statusCode int) {}

func (w *wsResponseWriter) Flush() {}

// RelayResponsesWS upgrades GET /v1/responses to WebSocket, reads the JSON
// request from Codex CLI, and runs the normal HTTP relay pipeline with SSE
// output converted to WebSocket text messages.
func RelayResponsesWS(c *gin.Context) {
	ws, err := upgraderResponsesWS.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.LogError(c, "responses ws upgrade failed: "+err.Error())
		return
	}

	writer := &wsResponseWriter{
		ResponseWriter: c.Writer,
		ws:             ws,
	}

	defer func() {
		if !writer.closed {
			remaining := strings.TrimSpace(writer.buf.String())
			if remaining != "" {
				_ = ws.WriteMessage(websocket.TextMessage, []byte(remaining))
			}
		}
		ws.Close()
	}()

	// Read the JSON request from the first WebSocket message. This must run
	// before the disconnect-watcher goroutine below — gorilla/websocket allows
	// only one concurrent reader, so spawning the watcher first would race
	// with this read for the request payload.
	_ = ws.SetReadDeadline(time.Now().Add(wsReadTimeout))
	_, message, err := ws.ReadMessage()
	if err != nil {
		// Send a close frame so the client knows why we're disconnecting
		_ = ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseUnsupportedData, "failed to read request"))
		return
	}
	// Clear the deadline so the disconnect watcher can block until the client
	// actually disconnects, not just when the next 30s window elapses.
	_ = ws.SetReadDeadline(time.Time{})

	var req dto.OpenAIResponsesRequest
	if err := json.Unmarshal(message, &req); err != nil {
		_ = ws.WriteMessage(websocket.TextMessage,
			[]byte(`{"error":{"message":"invalid JSON request body","type":"invalid_request_error"}}`))
		return
	}

	// Watch for client disconnect — cancel upstream request so we don't keep
	// consuming and billing tokens for a stream nobody receives. Started after
	// the request read so there is only ever one ReadMessage in flight.
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	go func() {
		ws.ReadMessage() // blocks until disconnect or follow-up message
		cancel()
	}()
	c.Request = c.Request.WithContext(ctx)

	// Force streaming — WebSocket is inherently streaming; stream:false is
	// an HTTP-mode flag that has no meaning over WSS transport.
	streamTrue := true
	req.Stream = &streamTrue

	body, err := json.Marshal(req)
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage,
			[]byte(`{"error":{"message":"failed to encode request","type":"server_error"}}`))
		return
	}

	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Writer = writer

	// Run the normal relay pipeline. SSE output goes through our writer →
	// WebSocket messages. Errors go through c.JSON() → buffer → flushed in defer.
	Relay(c, types.RelayFormatOpenAIResponses)
}
