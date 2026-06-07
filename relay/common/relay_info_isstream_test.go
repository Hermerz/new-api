package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestGenRelayInfoSetsIsStreamContextKey verifies that the relay info generation
// records the request's stream status into the gin context under
// constant.ContextKeyIsStream. This is the value RecordErrorLog reads when
// persisting error logs (see controller/relay.go), so it must reflect the real
// stream status instead of a hardcoded false (#4195).
func TestGenRelayInfoSetsIsStreamContextKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	streamTrue := true
	streamFalse := false

	tests := []struct {
		name     string
		request  dto.Request
		expected bool
	}{
		{
			name:     "streaming request",
			request:  &dto.GeneralOpenAIRequest{Stream: &streamTrue},
			expected: true,
		},
		{
			name:     "non-streaming request",
			request:  &dto.GeneralOpenAIRequest{Stream: &streamFalse},
			expected: false,
		},
		{
			name:     "unset stream defaults to false",
			request:  &dto.GeneralOpenAIRequest{},
			expected: false,
		},
		{
			name:     "nil request defaults to false",
			request:  nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request, _ = http.NewRequest("POST", "/v1/chat/completions", nil)

			GenRelayInfoOpenAI(c, tt.request)

			require.Equal(t, tt.expected, common.GetContextKeyBool(c, constant.ContextKeyIsStream))
		})
	}
}
