package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNotificationEmailUnsubscribeGETConfirmsWithoutMutatingAndPOSTOneClickUnsubscribes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &unsubscribeHandlerSettingRepo{values: map[string]string{
		"notification_email_unsubscribe_secret": "test-unsubscribe-secret",
	}}
	notificationEmailService := service.NewNotificationEmailService(repo, nil)
	h := NewSettingHandler(nil, "test")
	h.SetNotificationEmailService(notificationEmailService)

	router := gin.New()
	router.GET("/api/v1/settings/email-unsubscribe", h.UnsubscribeNotificationEmail)
	router.POST("/api/v1/settings/email-unsubscribe", h.UnsubscribeNotificationEmailOneClick)

	token := unsubscribeHandlerTestToken(t, "test-unsubscribe-secret", "member@example.com", service.NotificationEmailEventBalanceLow)
	path := "/api/v1/settings/email-unsubscribe?token=" + url.QueryEscape(token)

	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, getRecorder.Code)
	require.Contains(t, getRecorder.Body.String(), "Confirm unsubscribe")
	require.Contains(t, getRecorder.Body.String(), "method=\"post\"")
	require.Contains(t, getRecorder.Body.String(), "name=\"List-Unsubscribe\" value=\"One-Click\"")
	require.NotContains(t, getRecorder.Body.String(), "member@example.com")
	require.Equal(t, "no-store, max-age=0", getRecorder.Header().Get("Cache-Control"))
	require.Equal(t, "no-referrer", getRecorder.Header().Get("Referrer-Policy"))
	require.Equal(t, 0, repo.setCalls)

	form := url.Values{notificationEmailOneClickField: []string{notificationEmailOneClickValue}}.Encode()
	postRecorder := httptest.NewRecorder()
	postRequest := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(postRecorder, postRequest)
	require.Equal(t, http.StatusOK, postRecorder.Code)
	require.Contains(t, postRecorder.Body.String(), "Unsubscribed")
	require.Equal(t, 1, repo.setCalls)

	repeatRecorder := httptest.NewRecorder()
	repeatRequest := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form))
	repeatRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	router.ServeHTTP(repeatRecorder, repeatRequest)
	require.Equal(t, http.StatusOK, repeatRecorder.Code)
	require.Equal(t, 1, repo.setCalls)
}

func TestNotificationEmailUnsubscribePOSTRequiresOneClickForm(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &unsubscribeHandlerSettingRepo{values: map[string]string{
		"notification_email_unsubscribe_secret": "test-unsubscribe-secret",
	}}
	h := NewSettingHandler(nil, "test")
	h.SetNotificationEmailService(service.NewNotificationEmailService(repo, nil))
	router := gin.New()
	router.POST("/api/v1/settings/email-unsubscribe", h.UnsubscribeNotificationEmailOneClick)

	token := unsubscribeHandlerTestToken(t, "test-unsubscribe-secret", "member@example.com", service.NotificationEmailEventBalanceLow)
	path := "/api/v1/settings/email-unsubscribe?token=" + url.QueryEscape(token)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader("List-Unsubscribe=Nope"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, 0, repo.setCalls)
}

func unsubscribeHandlerTestToken(t *testing.T, secret, email, event string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"email": email,
		"event": event,
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	require.NoError(t, err)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, err = mac.Write([]byte(encodedPayload))
	require.NoError(t, err)
	return encodedPayload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

type unsubscribeHandlerSettingRepo struct {
	values   map[string]string
	setCalls int
}

func (r *unsubscribeHandlerSettingRepo) Get(ctx context.Context, key string) (*service.Setting, error) {
	value, err := r.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return &service.Setting{Key: key, Value: value}, nil
}

func (r *unsubscribeHandlerSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (r *unsubscribeHandlerSettingRepo) Set(_ context.Context, key, value string) error {
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = value
	r.setCalls++
	return nil
}

func (r *unsubscribeHandlerSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r *unsubscribeHandlerSettingRepo) SetMultiple(ctx context.Context, values map[string]string) error {
	for key, value := range values {
		if err := r.Set(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (r *unsubscribeHandlerSettingRepo) GetAll(_ context.Context) (map[string]string, error) {
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, nil
}

func (r *unsubscribeHandlerSettingRepo) Delete(_ context.Context, key string) error {
	if _, ok := r.values[key]; !ok {
		return service.ErrSettingNotFound
	}
	delete(r.values, key)
	return nil
}

var _ service.SettingRepository = (*unsubscribeHandlerSettingRepo)(nil)
