// Package telegram is a minimal Telegram Bot API client: just the calls the
// Sub2API bot needs (getMe, getUpdates, sendMessage, setMyCommands and
// approving or declining group join requests).
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
)

const (
	defaultBaseURL = "https://api.telegram.org"
	requestTimeout = 15 * time.Second
	maxBodyBytes   = 4 << 20
)

// Bot identifies the bot to call and how to reach Telegram.
type Bot struct {
	Token    string
	ProxyURL string // optional http/https/socks5 proxy
}

type User struct {
	ID           int64  `json:"id"`
	IsBot        bool   `json:"is_bot"`
	FirstName    string `json:"first_name"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

// ChatJoinRequest is a request to join a group where the bot is an admin.
type ChatJoinRequest struct {
	Chat Chat `json:"chat"`
	From User `json:"from"`
	// UserChatID is a private chat the bot may message for 5 minutes,
	// even if the user never started the bot.
	UserChatID int64 `json:"user_chat_id"`
}

type Update struct {
	UpdateID        int64            `json:"update_id"`
	Message         *Message         `json:"message"`
	ChatJoinRequest *ChatJoinRequest `json:"chat_join_request"`
}

type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// APIError is an error reported by the Bot API itself.
type APIError struct {
	Code        int
	Description string
	RetryAfter  int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram api error %d: %s", e.Code, e.Description)
}

// IsConflict reports a 409: another getUpdates consumer or an active webhook.
func IsConflict(err error) bool { return apiErrorCode(err) == http.StatusConflict }

// IsUnauthorized reports a revoked or invalid bot token.
func IsUnauthorized(err error) bool { return apiErrorCode(err) == http.StatusUnauthorized }

// IsNotFound is what Telegram returns for a malformed bot token.
func IsNotFound(err error) bool { return apiErrorCode(err) == http.StatusNotFound }

// IsForbidden reports that the user blocked the bot or never started it.
func IsForbidden(err error) bool { return apiErrorCode(err) == http.StatusForbidden }

// RetryAfter returns the flood-control wait Telegram asked for, if any.
func RetryAfter(err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return time.Duration(apiErr.RetryAfter) * time.Second
	}
	return 0
}

func apiErrorCode(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return 0
}

// API is the subset of the Bot API used by Sub2API.
type API interface {
	GetMe(ctx context.Context, bot Bot) (*User, error)
	GetUpdates(ctx context.Context, bot Bot, offset int64, timeoutSeconds int) ([]Update, error)
	SendMessage(ctx context.Context, bot Bot, chatID int64, text string) error
	SetMyCommands(ctx context.Context, bot Bot, commands []BotCommand, languageCode string) error
	ApproveChatJoinRequest(ctx context.Context, bot Bot, chatID, userID int64) error
	DeclineChatJoinRequest(ctx context.Context, bot Bot, chatID, userID int64) error
}

// Client calls the real Bot API.
type Client struct {
	baseURL string
}

func NewClient() *Client { return &Client{baseURL: defaultBaseURL} }

// NewClientWithBaseURL is used by tests to point at a fake server.
func NewClientWithBaseURL(baseURL string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/")}
}

func (c *Client) GetMe(ctx context.Context, bot Bot) (*User, error) {
	var me User
	if err := c.call(ctx, bot, "getMe", map[string]any{}, &me, requestTimeout); err != nil {
		return nil, err
	}
	return &me, nil
}

func (c *Client) GetUpdates(ctx context.Context, bot Bot, offset int64, timeoutSeconds int) ([]Update, error) {
	payload := map[string]any{
		"offset":          offset,
		"timeout":         timeoutSeconds,
		"allowed_updates": []string{"message", "chat_join_request"},
	}
	var updates []Update
	timeout := time.Duration(timeoutSeconds)*time.Second + requestTimeout
	if err := c.call(ctx, bot, "getUpdates", payload, &updates, timeout); err != nil {
		return nil, err
	}
	return updates, nil
}

func (c *Client) SendMessage(ctx context.Context, bot Bot, chatID int64, text string) error {
	payload := map[string]any{
		"chat_id":              chatID,
		"text":                 text,
		"link_preview_options": map[string]any{"is_disabled": true},
	}
	return c.call(ctx, bot, "sendMessage", payload, nil, requestTimeout)
}

func (c *Client) SetMyCommands(ctx context.Context, bot Bot, commands []BotCommand, languageCode string) error {
	payload := map[string]any{"commands": commands}
	if languageCode != "" {
		payload["language_code"] = languageCode
	}
	return c.call(ctx, bot, "setMyCommands", payload, nil, requestTimeout)
}

func (c *Client) ApproveChatJoinRequest(ctx context.Context, bot Bot, chatID, userID int64) error {
	payload := map[string]any{"chat_id": chatID, "user_id": userID}
	return c.call(ctx, bot, "approveChatJoinRequest", payload, nil, requestTimeout)
}

func (c *Client) DeclineChatJoinRequest(ctx context.Context, bot Bot, chatID, userID int64) error {
	payload := map[string]any{"chat_id": chatID, "user_id": userID}
	return c.call(ctx, bot, "declineChatJoinRequest", payload, nil, requestTimeout)
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (c *Client) call(ctx context.Context, bot Bot, method string, payload any, out any, timeout time.Duration) error {
	if strings.TrimSpace(bot.Token) == "" {
		return errors.New("telegram bot token is empty")
	}
	httpClient, err := httpclient.GetClient(httpclient.Options{ProxyURL: bot.ProxyURL, Timeout: timeout})
	if err != nil {
		return fmt.Errorf("telegram proxy: %w", err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := c.baseURL + "/bot" + bot.Token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return redactToken(err, bot.Token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		// net/http errors embed the request URL, which contains the token.
		return redactToken(err, bot.Token)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return redactToken(err, bot.Token)
	}
	var parsed apiResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("telegram %s: unexpected response (http %d)", method, resp.StatusCode)
	}
	if !parsed.OK {
		code := parsed.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return &APIError{Code: code, Description: parsed.Description, RetryAfter: parsed.Parameters.RetryAfter}
	}
	if out != nil && len(parsed.Result) > 0 {
		if err := json.Unmarshal(parsed.Result, out); err != nil {
			return fmt.Errorf("telegram %s: decode result: %w", method, err)
		}
	}
	return nil
}

func redactToken(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), token, "<redacted>"))
}
