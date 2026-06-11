package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	if responsesResponse.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResponse.GetQuality())
		c.Set("image_generation_call_size", responsesResponse.GetSize())
	}

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := dto.Usage{}
	if responsesResponse.Usage != nil {
		usage.PromptTokens = responsesResponse.Usage.InputTokens
		usage.CompletionTokens = responsesResponse.Usage.OutputTokens
		usage.TotalTokens = responsesResponse.Usage.TotalTokens
		if responsesResponse.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = responsesResponse.Usage.InputTokensDetails.CachedTokens
		}
	}
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return &usage, nil
	}
	// 解析 Tools 用量
	for _, tool := range responsesResponse.Tools {
		buildToolinfo, ok := info.ResponsesUsageInfo.BuiltInTools[common.Interface2String(tool["type"])]
		if !ok || buildToolinfo == nil {
			logger.LogError(c, fmt.Sprintf("BuiltInTools not found for tool type: %v", tool["type"]))
			continue
		}
		buildToolinfo.CallCount++
	}
	return &usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	// 上游必须以 response.completed / response.failed / response.incomplete 之一收口；
	// 没看到终止事件就断流（超时/EOF/扫描错误）时，由我们补发合成 response.failed，
	// 避免客户端拿到静默关闭的半截流（对外表现为吊死或 stream disconnected）
	var sawTerminalEvent bool
	var upstreamResponseId string
	var failureLogged bool

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Error(err)
			return
		}
		if streamResponse.Response != nil && streamResponse.Response.ID != "" {
			upstreamResponseId = streamResponse.Response.ID
		}
		sendResponsesStreamData(c, streamResponse, data)
		switch streamResponse.Type {
		case "response.failed", "response.incomplete", "error", "response.error":
			sawTerminalEvent = true
			info.StreamStatus.RecordError("upstream " + streamResponse.Type)
			if !failureLogged {
				failureLogged = true
				msg := extractResponsesFailureMessage(streamResponse, data)
				recordResponsesStreamErrorLog(c, "upstream_"+strings.ReplaceAll(streamResponse.Type, ".", "_"), msg)
			}
		case "response.completed":
			sawTerminalEvent = true
			if streamResponse.Response != nil {
				if streamResponse.Response.Usage != nil {
					if streamResponse.Response.Usage.InputTokens != 0 {
						usage.PromptTokens = streamResponse.Response.Usage.InputTokens
					}
					if streamResponse.Response.Usage.OutputTokens != 0 {
						usage.CompletionTokens = streamResponse.Response.Usage.OutputTokens
					}
					if streamResponse.Response.Usage.TotalTokens != 0 {
						usage.TotalTokens = streamResponse.Response.Usage.TotalTokens
					}
					if streamResponse.Response.Usage.InputTokensDetails != nil {
						usage.PromptTokensDetails.CachedTokens = streamResponse.Response.Usage.InputTokensDetails.CachedTokens
					}
				}
				if streamResponse.Response.HasImageGenerationCall() {
					c.Set("image_generation_call", true)
					c.Set("image_generation_call_quality", streamResponse.Response.GetQuality())
					c.Set("image_generation_call_size", streamResponse.Response.GetSize())
				}
			}
		case "response.output_text.delta":
			// 处理输出文本
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			// 函数调用处理
			if streamResponse.Item != nil {
				switch streamResponse.Item.Type {
				case dto.BuildInCallWebSearchCall:
					if info != nil && info.ResponsesUsageInfo != nil && info.ResponsesUsageInfo.BuiltInTools != nil {
						if webSearchTool, exists := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; exists && webSearchTool != nil {
							webSearchTool.CallCount++
						}
					}
				}
			}
		}
	})

	// 失败收尾：上游没发终止事件就断流（空闲超时 / 中途掐断 / EOF），补发一条
	// 合成 response.failed 让客户端立刻收口，而不是静默关连接。
	// 刻意不向框架返回 error：流已部分写出，错误会触发渠道重试（第二条流拼进
	// 同一响应）和 processChannelError 渠道惩罚，比静默断流更糟
	if !sawTerminalEvent && info.StreamStatus != nil {
		var failCode string
		switch info.StreamStatus.EndReason {
		case relaycommon.StreamEndReasonTimeout:
			failCode = "upstream_idle_timeout"
		case relaycommon.StreamEndReasonScannerErr:
			failCode = "upstream_stream_error"
		case relaycommon.StreamEndReasonEOF, relaycommon.StreamEndReasonDone:
			failCode = "upstream_ended_without_terminal_event"
		}
		if failCode != "" {
			info.StreamStatus.RecordError(failCode)
			msg := fmt.Sprintf("stream aborted by gateway: %s (request_id: %s)", failCode, c.GetString(common.RequestIdKey))
			if info.StreamStatus.EndError != nil {
				msg = fmt.Sprintf("%s, cause: %s", msg, info.StreamStatus.EndError.Error())
			}
			sendSyntheticResponsesFailedEvent(c, upstreamResponseId, failCode, msg)
			if !failureLogged {
				failureLogged = true
				recordResponsesStreamErrorLog(c, failCode, msg)
			}
		}
	}

	if usage.CompletionTokens == 0 {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// 非正常结束，使用输出文本的 token 数量
			completionTokens := service.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
		}
	}

	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return usage, nil
}

// extractResponsesFailureMessage 从上游失败事件里提取人类可读的错误信息
func extractResponsesFailureMessage(streamResponse dto.ResponsesStreamResponse, raw string) string {
	if streamResponse.Response != nil {
		if oaiErr := streamResponse.Response.GetOpenAIError(); oaiErr != nil && oaiErr.Message != "" {
			return oaiErr.Message
		}
	}
	if len(raw) > 300 {
		raw = raw[:300]
	}
	return raw
}

// sendSyntheticResponsesFailedEvent 以合规 Responses SSE 形状补发终止失败事件。
// 流已部分写出，无法改写 HTTP 状态码，只能以流内事件收口；不发 [DONE]，
// 避免客户端把失败流误判为正常完成
func sendSyntheticResponsesFailedEvent(c *gin.Context, responseId string, code string, message string) {
	if c.Request != nil && c.Request.Context().Err() != nil {
		return // 客户端已断开，写了也没人收
	}
	if responseId == "" {
		responseId = "resp_" + c.GetString(common.RequestIdKey)
	}
	payload := map[string]any{
		"type": "response.failed",
		"response": map[string]any{
			"id":     responseId,
			"object": "response",
			"status": "failed",
			"error": map[string]any{
				"code":    code,
				"message": message,
			},
		},
	}
	data, err := common.Marshal(payload)
	if err != nil {
		return
	}
	helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.failed"}, string(data))
}

// recordResponsesStreamErrorLog 流中途失败时记一条错误日志（每条流最多一条）。
// 刻意不走 processChannelError / 渠道 auto-disable：上游容量类 mid-stream
// 失败不应享受与请求期错误同等的渠道惩罚，防止误禁用
func recordResponsesStreamErrorLog(c *gin.Context, code string, message string) {
	if !constant.ErrorLogEnabled {
		return
	}
	other := map[string]interface{}{
		"error_type":   "responses_stream_failure",
		"error_code":   code,
		"channel_id":   c.GetInt("channel_id"),
		"channel_name": c.GetString("channel_name"),
		"channel_type": c.GetInt("channel_type"),
		"failure_kind": classifyStreamFailureKind(message),
	}
	if c.Request != nil && c.Request.URL != nil {
		other["request_path"] = c.Request.URL.Path
	}
	startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if startTime.IsZero() {
		startTime = time.Now()
	}
	model.RecordErrorLog(c, c.GetInt("id"), c.GetInt("channel_id"), c.GetString("original_model"), c.GetString("token_name"),
		message, c.GetInt("token_id"), int(time.Since(startTime).Seconds()), true, c.GetString("group"), other)
}

// classifyStreamFailureKind 区分上游容量类失败（并发/限流）与其它失败，
// 供后续按类别统计与告警
func classifyStreamFailureKind(message string) string {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "concurrenc") || strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many") {
		return "upstream_capacity"
	}
	return "upstream_failure"
}
