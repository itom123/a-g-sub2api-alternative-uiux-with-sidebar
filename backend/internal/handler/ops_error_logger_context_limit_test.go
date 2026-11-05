package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func recordOpsUpstreamFailure(t *testing.T, status int, upstreamStatus int, message string) *service.OpsInsertErrorLogInput {
	t.Helper()
	setupOpsErrorLogTestQueue(t, 1)
	gin.SetMode(gin.TestMode)

	ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(OpsErrorLoggerMiddleware(ops))
	router.POST("/v1/messages", func(c *gin.Context) {
		c.Set(opsAccountIDKey, int64(2))
		c.Set(service.OpsUpstreamStatusCodeKey, upstreamStatus)
		c.Set(service.OpsUpstreamErrorMessageKey, message)
		c.Set(service.OpsUpstreamErrorsKey, []*service.OpsUpstreamErrorEvent{{
			UpstreamStatusCode: upstreamStatus,
			Message:            message,
		}})
		c.JSON(status, gin.H{"type": "error", "error": gin.H{"type": "api_error", "message": message}})
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/messages", nil))
	require.Equal(t, status, w.Code)
	job := <-opsErrorLogQueue
	require.NotNil(t, job.entry)
	return job.entry
}

// Production 2026-11-01: 26 "exceeds the context window" failures were relayed
// as 502 and counted as provider outages.
func TestOpsErrorLoggerContextWindowFailureIsClientOutsideSLA(t *testing.T) {
	for _, tc := range []struct {
		name           string
		status         int
		upstreamStatus int
		message        string
	}{
		{"OpenAI context window relayed as 502", http.StatusBadGateway, http.StatusBadGateway, "Your input exceeds the context window of this model. Please adjust your input and try again."},
		{"Anthropic prompt too long", http.StatusBadRequest, http.StatusBadRequest, "prompt is too long: 1146297 tokens > 1000000 maximum"},
		{"OpenAI maximum context length", http.StatusBadRequest, http.StatusBadRequest, "This model's maximum context length is 400000 tokens. However, your messages resulted in 512000 tokens."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := recordOpsUpstreamFailure(t, tc.status, tc.upstreamStatus, tc.message)
			require.Equal(t, tc.status, entry.StatusCode)
			require.Equal(t, "request", entry.ErrorPhase)
			require.Equal(t, "client", entry.ErrorOwner)
			require.Equal(t, "client_request", entry.ErrorSource)
			require.True(t, entry.IsBusinessLimited)
			require.Equal(t, "P3", entry.Severity)
			// Upstream details stay available for diagnosis.
			require.NotNil(t, entry.UpstreamStatusCode)
			require.Equal(t, tc.upstreamStatus, *entry.UpstreamStatusCode)
			require.NotNil(t, entry.UpstreamErrorMessage)
		})
	}
}

func TestOpsErrorLoggerOverloadStaysProviderFailure(t *testing.T) {
	entry := recordOpsUpstreamFailure(t, http.StatusBadGateway, http.StatusServiceUnavailable, "Our servers are currently overloaded. Please try again later.")
	require.Equal(t, "upstream", entry.ErrorPhase)
	require.Equal(t, "provider", entry.ErrorOwner)
	require.False(t, entry.IsBusinessLimited)
}

func TestLogOpsStreamErrorContextWindowFailureIsClientOutsideSLA(t *testing.T) {
	setupOpsErrorLogTestQueue(t, 1)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	service.MarkOpsStreamFailure(c, "api_error", "", "Your input exceeds the context window of this model. Please adjust your input and try again.", http.StatusBadGateway)
	logOpsStreamError(c, service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil), http.StatusOK)

	job := <-opsErrorLogQueue
	require.NotNil(t, job.entry)
	require.Equal(t, "client", job.entry.ErrorOwner)
	require.True(t, job.entry.IsBusinessLimited)
}
