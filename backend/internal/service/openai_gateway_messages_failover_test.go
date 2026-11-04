package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const (
	messagesTestCreated  = `data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_1","model":"gpt-6.1-sol","status":"in_progress","output":[]}}`
	messagesTestDelta    = `data: {"type":"response.output_text.delta","sequence_number":2,"output_index":0,"content_index":0,"delta":"Hello"}`
	messagesTestComplete = `data: {"type":"response.completed","sequence_number":3,"response":{"id":"resp_1","model":"gpt-6.1-sol","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":1}}}`
	// The live 2026-11-01 overload: HTTP 200, then a bare in-stream error.
	messagesTestOverload = "event: error\n" +
		`data: {"type":"error","sequence_number":3,"error":{"code":"server_is_overloaded","type":"service_unavailable_error","message":"Our servers are currently overloaded. Please try again later.","headers":{"retry-after":"7"}}}`
	messagesTestContextWindow = "event: response.failed\n" +
		`data: {"type":"response.failed","sequence_number":3,"response":{"id":"resp_1","model":"gpt-6.1-sol","status":"failed","output":[],"error":{"code":"","message":"Your input exceeds the context window of this model. Please adjust your input and try again."}}}`
)

func messagesTestUpstream(body io.Reader) *httpUpstreamRecorder {
	return &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_messages"}},
		Body:       io.NopCloser(body),
	}}
}

func messagesTestSSE(frames ...string) io.Reader {
	return strings.NewReader(strings.Join(frames, "\n\n") + "\n\n")
}

func forwardMessagesForTest(t *testing.T, svc *OpenAIGatewayService, stream bool) (*httptest.ResponseRecorder, *gin.Context, *OpenAIForwardResult, error) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-6.1-sol","max_tokens":1024,"messages":[{"role":"user","content":"hi"}],"stream":` + map[bool]string{true: "true", false: "false"}[stream] + `}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	result, err := svc.ForwardAsAnthropic(context.Background(), c, compatCyberOAuthAccount(), body, "", "gpt-6.1-sol")
	return rec, c, result, err
}

// Live 2026-11-01: 57 overloads on one account reached clients because
// message_start had already been sent when the in-stream error arrived.
func TestForwardAsAnthropicOverloadBeforeContentFailsOverWithoutOutput(t *testing.T) {
	svc := &OpenAIGatewayService{httpUpstream: messagesTestUpstream(messagesTestSSE(messagesTestCreated, messagesTestOverload))}
	rec, c, _, err := forwardMessagesForTest(t, svc, true)

	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "overload before content must be retryable on another account, got %v", err)
	require.True(t, failoverErr.IsOpenAICapacityShed())
	require.False(t, c.Writer.Written(), "held message_start must not reach the client")
	require.Empty(t, rec.Body.String())
}

// An upstream stream that ends after response.created, before any content, is
// now retryable on another account (nothing reached the client).
func TestForwardAsAnthropicTruncatedBeforeContentFailsOver(t *testing.T) {
	svc := &OpenAIGatewayService{httpUpstream: messagesTestUpstream(messagesTestSSE(messagesTestCreated))}
	rec, _, _, err := forwardMessagesForTest(t, svc, true)

	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "got %v", err)
	require.Empty(t, rec.Body.String())
}

func TestForwardAsAnthropicSuccessKeepsEventOrder(t *testing.T) {
	svc := &OpenAIGatewayService{httpUpstream: messagesTestUpstream(messagesTestSSE(messagesTestCreated, messagesTestDelta, messagesTestComplete))}
	rec, _, result, err := forwardMessagesForTest(t, svc, true)

	require.NoError(t, err)
	require.NotNil(t, result)
	body := rec.Body.String()
	require.True(t, strings.HasPrefix(body, "event: message_start\n"), "message_start must still come first: %q", body)
	start, block, stop := strings.Index(body, "event: message_start"), strings.Index(body, "event: content_block_start"), strings.Index(body, "event: message_stop")
	require.True(t, start < block && block < stop, "events out of order: %q", body)
	require.Equal(t, 1, strings.Count(body, "event: message_start"))
	require.Contains(t, body, `"text":"Hello"`)
}

func TestForwardAsAnthropicOverloadAfterContentIsOverloadedError(t *testing.T) {
	svc := &OpenAIGatewayService{httpUpstream: messagesTestUpstream(messagesTestSSE(messagesTestCreated, messagesTestDelta, messagesTestOverload))}
	rec, _, _, err := forwardMessagesForTest(t, svc, true)

	var failoverErr *UpstreamFailoverError
	require.Error(t, err)
	require.False(t, errors.As(err, &failoverErr), "content already sent: must not splice another account's stream")
	body := rec.Body.String()
	require.Contains(t, body, "event: message_start")
	require.Contains(t, body, "event: error\ndata: ")
	require.Contains(t, body, `"type":"overloaded_error"`)
}

func TestForwardAsAnthropicContextWindowIsPromptTooLong400(t *testing.T) {
	for _, stream := range []bool{true, false} {
		t.Run(map[bool]string{true: "stream", false: "buffered"}[stream], func(t *testing.T) {
			svc := &OpenAIGatewayService{httpUpstream: messagesTestUpstream(messagesTestSSE(messagesTestCreated, messagesTestContextWindow))}
			rec, _, _, err := forwardMessagesForTest(t, svc, stream)

			var failoverErr *UpstreamFailoverError
			require.Error(t, err)
			require.False(t, errors.As(err, &failoverErr), "context-window failures must not fail over")
			require.Equal(t, http.StatusBadRequest, rec.Code, "client must get a non-retryable 400, body %q", rec.Body.String())
			require.Equal(t, "invalid_request_error", gjson.Get(rec.Body.String(), "error.type").String())
			require.Equal(t, "prompt is too long: Your input exceeds the context window of this model. Please adjust your input and try again.",
				gjson.Get(rec.Body.String(), "error.message").String())
		})
	}
}

// Keepalive pings sent while message_start is held commit the 200 status but
// are not semantic output: failover must stay possible.
func TestForwardAsAnthropicPingsDoNotBlockFailover(t *testing.T) {
	reader, writer := io.Pipe()
	go func() {
		_, _ = io.WriteString(writer, messagesTestCreated+"\n\n")
		time.Sleep(2500 * time.Millisecond)
		_, _ = io.WriteString(writer, messagesTestOverload+"\n\n")
		_ = writer.Close()
	}()
	cfg := &config.Config{}
	cfg.Gateway.StreamKeepaliveInterval = 1
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: messagesTestUpstream(reader)}
	rec, c, _, err := forwardMessagesForTest(t, svc, true)

	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "got %v", err)
	require.Contains(t, rec.Body.String(), "event: ping")
	require.NotContains(t, rec.Body.String(), "message_start")
	require.Equal(t, -1, OpenAICompactKeepaliveAdjustedWrittenSize(c), "only keepalive bytes were written")
}

func TestAnthropicCompatFailedEventResponse(t *testing.T) {
	status, errType, message := anthropicCompatFailedEventResponse([]byte(`{"type":"response.failed","response":{"error":{"code":"server_error","message":"boom"}}}`), "boom")
	require.Equal(t, http.StatusBadGateway, status)
	require.Equal(t, "api_error", errType)
	require.Equal(t, "boom", message)

	status, errType, _ = anthropicCompatFailedEventResponse([]byte(`{"type":"error","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded."}}`), "Our servers are currently overloaded.")
	require.Equal(t, 529, status)
	require.Equal(t, "overloaded_error", errType)

	require.Equal(t, "prompt is too long", anthropicPromptTooLongMessage(" "))
	require.Equal(t, "prompt is too long: 1146297 tokens > 1000000 maximum", anthropicPromptTooLongMessage("prompt is too long: 1146297 tokens > 1000000 maximum"))
	require.Equal(t, "prompt is too long: This model's maximum context length is 400000 tokens.", anthropicPromptTooLongMessage("This model's maximum context length is 400000 tokens."))
}
