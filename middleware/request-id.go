package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"runtime/debug"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

var _bp = func() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Path != "" {
		h := sha256.Sum256([]byte(bi.Main.Path))
		return hex.EncodeToString(h[:4])
	}
	return common.GetRandomString(8)
}()

func RequestId() func(c *gin.Context) {
	return func(c *gin.Context) {
		id := common.GetTimeString() + _bp + common.GetRandomString(8)
		c.Set(common.RequestIdKey, id)
		ctx := context.WithValue(c.Request.Context(), common.RequestIdKey, id)
		c.Request = c.Request.WithContext(ctx)
		c.Header(common.RequestIdKey, id)
		// 对客户文档化的请求 ID 头。上游链路里可能有多个 new-api 实例都回
		// X-Oneapi-Request-Id，加品牌前缀让客户能明确指认本网关这一跳
		c.Header(common.ZetaRequestIdHeader, id)
		c.Next()
	}
}
