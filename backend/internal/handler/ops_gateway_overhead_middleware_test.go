package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpsErrorLoggerMiddlewareRecordsGatewayOverhead(t *testing.T) {
	setupOpsErrorLogTestQueue(t, 1)
	gin.SetMode(gin.TestMode)

	ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(OpsErrorLoggerMiddleware(ops))
	router.POST("/v1/messages", func(c *gin.Context) {
		time.Sleep(15 * time.Millisecond) // gateway pre-processing
		service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, 12)
		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, 30)
		c.JSON(http.StatusOK, gin.H{"type": "message"})
	})
	router.POST("/v1/models", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": []string{}})
	})

	start := time.Now().Add(-time.Minute)
	for _, path := range []string{"/v1/messages", "/v1/models"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		require.Equal(t, http.StatusOK, w.Code)
	}

	got := ops.GatewayOverhead(start, time.Now().Add(time.Second))
	require.NotNil(t, got)
	require.Equal(t, int64(1), got.SampleCount, "requests without gateway timing are not counted")
	// Pre-processing is measured from the middleware, not taken from the handler's value.
	require.GreaterOrEqual(t, *got.ProcessingP90, 15)
	require.Less(t, *got.ProcessingP90, 15+150)
	require.Equal(t, 30, *got.RoutingP90)
	require.Equal(t, *got.ProcessingP90+30, *got.P50)
	require.Zero(t, *got.UploadP90)
}

// opsUploadingBody is a request body whose bytes arrive late.
type opsUploadingBody struct {
	r     io.Reader
	delay time.Duration
}

func (b *opsUploadingBody) Read(p []byte) (int, error) {
	if b.delay > 0 {
		time.Sleep(b.delay)
		b.delay = 0
	}
	return b.r.Read(p)
}

func TestOpsErrorLoggerMiddlewareSeparatesBodyUpload(t *testing.T) {
	for _, tc := range []struct {
		name string
		// preread mimics the model-allowlist and composite middleware, which read
		// the body before the handler starts its own timer.
		preread bool
	}{{name: "handler reads body"}, {name: "middleware pre-reads body", preread: true}} {
		t.Run(tc.name, func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 1)
			gin.SetMode(gin.TestMode)

			ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			router.Use(OpsErrorLoggerMiddleware(ops))
			if tc.preread {
				router.Use(func(c *gin.Context) {
					body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
					require.NoError(t, err)
					requestmodel.ResetRequestBody(c.Request, body)
					c.Next()
				})
			}
			var handlerBody string
			router.POST("/v1/messages", func(c *gin.Context) {
				requestStart := time.Now()
				body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
				require.NoError(t, err)
				handlerBody = string(body)
				service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
				service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, 4)
				c.JSON(http.StatusOK, gin.H{"type": "message"})
			})

			start := time.Now().Add(-time.Minute)
			payload := `{"model":"claude","messages":[]}`
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", &opsUploadingBody{r: strings.NewReader(payload), delay: 80 * time.Millisecond})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, payload, handlerBody)

			got := ops.GatewayOverhead(start, time.Now().Add(time.Second))
			require.Equal(t, int64(1), got.SampleCount)
			require.GreaterOrEqual(t, *got.UploadP90, 80, "waiting for the body is reported as upload")
			require.Less(t, *got.UploadP90, 80+150)
			require.Less(t, *got.ProcessingP90, 40, "upload is not counted as gateway processing")
			require.Equal(t, *got.ProcessingP90+4, *got.P90)
		})
	}
}
