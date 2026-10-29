//go:build unit

package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClientSendMessageRequestShape(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer srv.Close()

	c := NewClientWithBaseURL(srv.URL)
	err := c.SendMessage(context.Background(), Bot{Token: "123:abc"}, 42, "hello")
	require.NoError(t, err)
	require.Equal(t, "/bot123:abc/sendMessage", gotPath)
	require.Equal(t, float64(42), gotBody["chat_id"])
	require.Equal(t, "hello", gotBody["text"])
}

func TestClientGetUpdatesAsksForJoinRequests(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":7,"chat_join_request":{"chat":{"id":-100,"type":"supergroup"},"from":{"id":5,"first_name":"A"},"user_chat_id":5,"date":1}}]}`))
	}))
	defer srv.Close()

	updates, err := NewClientWithBaseURL(srv.URL).GetUpdates(context.Background(), Bot{Token: "t"}, 3, 0)
	require.NoError(t, err)
	require.Equal(t, []any{"message", "chat_join_request"}, gotBody["allowed_updates"])
	require.Len(t, updates, 1)
	req := updates[0].ChatJoinRequest
	require.NotNil(t, req)
	require.Equal(t, int64(-100), req.Chat.ID)
	require.Equal(t, int64(5), req.From.ID)
	require.Equal(t, int64(5), req.UserChatID)
}

func TestClientApproveChatJoinRequestShape(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	require.NoError(t, NewClientWithBaseURL(srv.URL).ApproveChatJoinRequest(context.Background(), Bot{Token: "t"}, -100, 5))
	require.Equal(t, "/bott/approveChatJoinRequest", gotPath)
	require.Equal(t, float64(-100), gotBody["chat_id"])
	require.Equal(t, float64(5), gotBody["user_id"])
}

func TestClientParsesAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":409,"description":"Conflict: terminated by other getUpdates request"}`))
	}))
	defer srv.Close()

	_, err := NewClientWithBaseURL(srv.URL).GetUpdates(context.Background(), Bot{Token: "t"}, 0, 0)
	require.Error(t, err)
	require.True(t, IsConflict(err))
	require.False(t, IsUnauthorized(err))
}

func TestClientRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":3}}`))
	}))
	defer srv.Close()

	err := NewClientWithBaseURL(srv.URL).SendMessage(context.Background(), Bot{Token: "t"}, 1, "x")
	require.Equal(t, 3*time.Second, RetryAfter(err))
}

func TestClientTransportErrorsDoNotLeakToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // connection refused

	token := "987654:SECRET-TOKEN"
	_, err := NewClientWithBaseURL(url).GetMe(context.Background(), Bot{Token: token})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "SECRET-TOKEN")
	require.Contains(t, err.Error(), "<redacted>")
}

func TestClientRejectsEmptyToken(t *testing.T) {
	_, err := NewClient().GetMe(context.Background(), Bot{})
	require.Error(t, err)
}
