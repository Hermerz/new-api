package openai

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupResponsesStreamTest(t *testing.T, body io.Reader, timeoutSeconds int) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = timeoutSeconds
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	resp := &http.Response{Body: io.NopCloser(body)}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	return c, recorder, resp, info
}

// 上游 stall（发了 created 后不再发任何 data 行）→ 空闲超时后必须给客户端
// 补发合成 response.failed 收口，而不是静默关流
func TestOaiResponsesStreamHandler_TimeoutEmitsSyntheticFailed(t *testing.T) {
	// Not parallel: modifies global constant.StreamingTimeout
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 2
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	pr, pw := io.Pipe()
	go func() {
		fmt.Fprint(pw, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_zombie\"}}\n")
		time.Sleep(10 * time.Second)
		pw.Close()
	}()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	resp := &http.Response{Body: pr}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	done := make(chan struct{})
	go func() {
		usage, apiErr := OaiResponsesStreamHandler(c, info, resp)
		assert.Nil(t, apiErr)
		assert.NotNil(t, usage)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("handler did not return after idle timeout")
	}

	body := recorder.Body.String()
	assert.Contains(t, body, "event: response.failed")
	assert.Contains(t, body, "upstream_idle_timeout")
	assert.Contains(t, body, "resp_zombie", "should reuse the upstream response id")
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason)
	assert.True(t, info.StreamStatus.HasErrors())
}

// 上游发了部分事件后直接 EOF（无 completed/failed/incomplete）→ 不算正常结束，
// 补发 upstream_ended_without_terminal_event
func TestOaiResponsesStreamHandler_EOFWithoutTerminalEmitsSyntheticFailed(t *testing.T) {
	body := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_eof\"}}\n"
	c, recorder, resp, info := setupResponsesStreamTest(t, strings.NewReader(body), 30)

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)
	assert.Nil(t, apiErr)
	assert.NotNil(t, usage)

	out := recorder.Body.String()
	assert.Contains(t, out, "event: response.failed")
	assert.Contains(t, out, "upstream_ended_without_terminal_event")
	require.NotNil(t, info.StreamStatus)
	assert.True(t, info.StreamStatus.HasErrors())
}

// 上游自己发了 response.failed → 原样透传，不能再叠加合成事件（不许双终止）
func TestOaiResponsesStreamHandler_UpstreamFailedForwardedWithoutSynthetic(t *testing.T) {
	var b strings.Builder
	b.WriteString("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_up\"}}\n")
	b.WriteString("data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_up\",\"error\":{\"message\":\"Concurrency limit exceeded for account, please retry later\",\"type\":\"server_error\",\"code\":\"concurrency_limit\"}}}\n")
	c, recorder, resp, info := setupResponsesStreamTest(t, strings.NewReader(b.String()), 30)

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)
	assert.Nil(t, apiErr)
	assert.NotNil(t, usage)

	out := recorder.Body.String()
	assert.Equal(t, 1, strings.Count(out, "event: response.failed"), "must forward exactly one terminal failed event")
	assert.Contains(t, out, "Concurrency limit exceeded")
	assert.NotContains(t, out, "stream aborted by gateway", "no synthetic event after upstream terminal event")
	require.NotNil(t, info.StreamStatus)
	assert.True(t, info.StreamStatus.HasErrors())
}

// 正常 completed 流：usage 正确提取，不得出现任何 response.failed
func TestOaiResponsesStreamHandler_CompletedNormal(t *testing.T) {
	var b strings.Builder
	b.WriteString("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_ok\"}}\n")
	b.WriteString("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ok\",\"usage\":{\"input_tokens\":5,\"output_tokens\":7,\"total_tokens\":12}}}\n")
	c, recorder, resp, info := setupResponsesStreamTest(t, strings.NewReader(b.String()), 30)

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)
	assert.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 5, usage.PromptTokens)
	assert.Equal(t, 7, usage.CompletionTokens)
	assert.Equal(t, 12, usage.TotalTokens)

	out := recorder.Body.String()
	assert.NotContains(t, out, "response.failed")
	require.NotNil(t, info.StreamStatus)
	assert.False(t, info.StreamStatus.HasErrors())
}

func TestClassifyStreamFailureKind(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "upstream_capacity", classifyStreamFailureKind("Concurrency limit exceeded for account, please retry later"))
	assert.Equal(t, "upstream_capacity", classifyStreamFailureKind("Rate limit reached"))
	assert.Equal(t, "upstream_capacity", classifyStreamFailureKind("Too many requests"))
	assert.Equal(t, "upstream_failure", classifyStreamFailureKind("internal server error"))
}
