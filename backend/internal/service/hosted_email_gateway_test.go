//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHostedCloudflareEmailGatewaySignsStructuredPayload(t *testing.T) {
	const secret = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY"
	fixedTime := time.Unix(1_792_345_678, 0).UTC()
	const nonce = "0123456789abcdefghijklmnopqrstuv"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, hostedEmailGatewayPath, r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "application/json", r.Header.Get("Accept"))
		require.Equal(t, "1792345678", r.Header.Get("X-Email-Timestamp"))
		require.Equal(t, nonce, r.Header.Get("X-Email-Nonce"))
		expectedDeliveryID, err := hostedEmailGatewayOpaqueDeliveryID([]byte(secret), "payment:123456")
		require.NoError(t, err)
		require.Equal(t, expectedDeliveryID, r.Header.Get("X-Email-Delivery-ID"))
		require.NotEqual(t, "payment:123456", r.Header.Get("X-Email-Delivery-ID"))

		var payload hostedCloudflareEmailGatewayPayload
		rawBody, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, hostedEmailGatewaySignature([]byte(secret), "1792345678", nonce, rawBody), r.Header.Get("X-Email-Signature"))
		require.NoError(t, json.Unmarshal(rawBody, &payload))
		require.Equal(t, NotificationEmailEventAuthVerifyCode, payload.Event)
		require.Equal(t, "person@example.com", payload.Recipient)
		require.Equal(t, "en", payload.Locale)
		require.Equal(t, map[string]string{
			"recipient_name":     "Person",
			"verification_code":  "654321",
			"expires_in_minutes": "15",
		}, payload.Data)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	endpoint, err := url.Parse(server.URL + hostedEmailGatewayPath)
	require.NoError(t, err)
	gateway := newHostedCloudflareEmailGateway(endpoint, []byte(secret), server.Client())
	gateway.now = func() time.Time { return fixedTime }
	gateway.nonce = func() (string, error) { return nonce, nil }

	err = gateway.SendTransactionalEmail(context.Background(), TransactionalEmailEvent{
		Event:      NotificationEmailEventAuthVerifyCode,
		Recipient:  "Person <person@example.com>",
		Locale:     "en",
		DeliveryID: "payment:123456",
		Data: map[string]string{
			"recipient_name":     "Person",
			"verification_code":  "654321",
			"expires_in_minutes": "15",
		},
	})
	require.NoError(t, err)
}

func TestHostedCloudflareEmailGatewayRejectsUnsafeBusinessDeliveryIDBeforeSending(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		attempts++
	}))
	defer server.Close()

	endpoint, err := url.Parse(server.URL + hostedEmailGatewayPath)
	require.NoError(t, err)
	gateway := newHostedCloudflareEmailGateway(endpoint, []byte("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY"), server.Client())

	err = gateway.SendTransactionalEmail(context.Background(), TransactionalEmailEvent{
		Event:      NotificationEmailEventAuthVerifyCode,
		Recipient:  "person@example.com",
		Locale:     "en",
		DeliveryID: "delivery\r\ninjection",
		Data: map[string]string{
			"recipient_name":     "Person",
			"verification_code":  "654321",
			"expires_in_minutes": "15",
		},
	})
	require.ErrorContains(t, err, "delivery id must be a trimmed single-line value")
	require.Zero(t, attempts)
}

func TestHostedCloudflareEmailGatewayRejectsUnboundedOrUnsupportedContent(t *testing.T) {
	reportData := hostedOpsScheduledReportGatewayData()
	reportData["report_html"] = "<p>not permitted</p>"
	payload, err := hostedEmailGatewayPayloadForEvent(TransactionalEmailEvent{
		Event:     NotificationEmailEventOpsScheduledReport,
		Recipient: "person@example.com",
		Locale:    "en",
		Data:      reportData,
	})
	require.ErrorContains(t, err, "unsupported Hosted email data field")
	require.Empty(t, payload)

	reportData = hostedOpsScheduledReportGatewayData()
	payload, err = hostedEmailGatewayPayloadForEvent(TransactionalEmailEvent{
		Event:     NotificationEmailEventOpsScheduledReport,
		Recipient: "person@example.com",
		Locale:    "en",
		Data:      reportData,
	})
	require.NoError(t, err)
	require.Equal(t, "daily_summary", payload.Data["report_type"])
	require.NotContains(t, payload.Data, "report_html")

	_, err = hostedEmailGatewayPayloadForEvent(TransactionalEmailEvent{
		Event:     NotificationEmailEventAuthVerifyCode,
		Recipient: "person@example.com",
		Locale:    "en",
		Data: map[string]string{
			"recipient_name":     "Person",
			"verification_code":  "654321",
			"expires_in_minutes": "15",
			"html":               "<p>not permitted</p>",
		},
	})
	require.ErrorContains(t, err, "unsupported Hosted email data field")
}

func TestNotificationEmailServiceUsesStructuredScheduledReportGateway(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	emailService := NewEmailService(repo, nil)
	gateway := &transactionalEmailGatewayStub{}
	emailService.SetTransactionalEmailGateway(gateway)
	notifications := NewNotificationEmailService(repo, emailService)

	now := time.Date(2026, time.July, 19, 1, 0, 26, 0, time.UTC)
	report := &opsScheduledReport{ReportType: "daily_summary", TimeRange: 24 * time.Hour}
	variables := opsSummaryReportEmailVariables(report, now, nil, "en")
	err := notifications.Send(context.Background(), NotificationEmailSendInput{
		Event:          NotificationEmailEventOpsScheduledReport,
		Locale:         "en",
		RecipientEmail: "ops@example.com",
		RecipientName:  "Ops",
		Variables:      variables,
		RawHTMLVariables: map[string]string{
			"report_html": "<h2>must not reach the Worker</h2>",
		},
	})
	require.NoError(t, err)
	require.Len(t, gateway.events, 1)
	require.Equal(t, NotificationEmailEventOpsScheduledReport, gateway.events[0].Event)
	require.Equal(t, "Ops", gateway.events[0].Data["recipient_name"])
	require.Equal(t, "daily_summary", gateway.events[0].Data["report_type"])
	require.NotContains(t, gateway.events[0].Data, "report_html")
	for _, field := range hostedOpsScheduledReportGatewayFields {
		require.NotEmptyf(t, gateway.events[0].Data[field], "missing scheduled report field %s", field)
	}
}

func TestHostedCloudflareEmailGatewayEnvironmentUsesSecretFileAndPinsEndpoint(t *testing.T) {
	const secret = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY"
	secretFile := filepath.Join(t.TempDir(), "email-gateway-hmac")
	require.NoError(t, os.WriteFile(secretFile, []byte(secret+"\n"), 0o600))

	t.Setenv(hostedEmailGatewayURLEnv, "https://mail.example.com"+hostedEmailGatewayPath)
	t.Setenv(hostedEmailGatewaySecretFileEnv, secretFile)
	gateway, err := NewHostedCloudflareEmailGatewayFromEnvironment()
	require.NoError(t, err)
	require.NotNil(t, gateway)
	require.Equal(t, []byte(secret), gateway.(*hostedCloudflareEmailGateway).secret)

	t.Setenv(hostedEmailGatewayURLEnv, "http://mail.example.com"+hostedEmailGatewayPath)
	_, err = NewHostedCloudflareEmailGatewayFromEnvironment()
	require.ErrorContains(t, err, "must be an HTTPS URL")

	t.Setenv(hostedEmailGatewayURLEnv, "https://mail.example.net"+hostedEmailGatewayPath)
	_, err = NewHostedCloudflareEmailGatewayFromEnvironment()
	require.ErrorContains(t, err, "host must be example.com")
}

func TestHostedCloudflareEmailGatewayRequiresExplicitFixedTemplateMode(t *testing.T) {
	secretFile := filepath.Join(t.TempDir(), "email-gateway-hmac")
	require.NoError(t, os.WriteFile(secretFile, []byte("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY\n"), 0o600))
	t.Setenv(hostedEmailGatewayURLEnv, "https://mail.example.com"+hostedEmailGatewayPath)
	t.Setenv(hostedEmailGatewaySecretFileEnv, secretFile)
	t.Setenv(hostedEmailGatewayFixedTemplatesEnv, "")

	_, err := ProvideEmailService(nil, nil)
	require.ErrorContains(t, err, hostedEmailGatewayFixedTemplatesEnv+" must be true")

	t.Setenv(hostedEmailGatewayFixedTemplatesEnv, "true")
	service, err := ProvideEmailService(nil, nil)
	require.NoError(t, err)
	require.NotNil(t, service.transactionalGateway)
}

func TestHostedCloudflareEmailGatewayRequiresBase64URLTextSecret(t *testing.T) {
	secretFile := filepath.Join(t.TempDir(), "email-gateway-hmac")
	t.Setenv(hostedEmailGatewayURLEnv, "https://mail.example.com"+hostedEmailGatewayPath)
	t.Setenv(hostedEmailGatewaySecretFileEnv, secretFile)

	for _, secret := range [][]byte{
		[]byte("0123456789abcdef0123456789abcdef"),
		[]byte("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="),
		[]byte{0x00, 0xff, 0x10},
	} {
		require.NoError(t, os.WriteFile(secretFile, secret, 0o600))
		_, err := NewHostedCloudflareEmailGatewayFromEnvironment()
		require.ErrorContains(t, err, "base64url")
	}
}

func TestHostedCloudflareEmailGatewayRejectsNonFirstPartyActionURLsBeforeSigning(t *testing.T) {
	_, err := hostedEmailGatewayPayloadForEvent(TransactionalEmailEvent{
		Event:     NotificationEmailEventAuthPasswordReset,
		Recipient: "person@example.com",
		Locale:    "en",
		Data: map[string]string{
			"recipient_name":     "Person",
			"reset_url":          "https://example.net/reset",
			"expires_in_minutes": "15",
		},
	})
	require.ErrorContains(t, err, "host must be example.com or api.example.com")

	payload, err := hostedEmailGatewayPayloadForEvent(TransactionalEmailEvent{
		Event:     NotificationEmailEventAuthPasswordReset,
		Recipient: "person@example.com",
		Locale:    "en",
		Data: map[string]string{
			"recipient_name":     "Person",
			"reset_url":          "https://api.example.com/reset?token=abc",
			"expires_in_minutes": "15",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://api.example.com/reset?token=abc", payload.Data["reset_url"])
}

func TestNotificationEmailServiceUsesHostedGatewayWithoutSMTP(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	emailService := NewEmailService(repo, nil)
	gateway := &transactionalEmailGatewayStub{}
	emailService.SetTransactionalEmailGateway(gateway)
	notifications := NewNotificationEmailService(repo, emailService)

	err := notifications.Send(context.Background(), NotificationEmailSendInput{
		Event:          NotificationEmailEventAuthVerifyCode,
		Locale:         "en-US",
		RecipientEmail: "person@example.com",
		RecipientName:  "Person",
		Variables: map[string]string{
			"verification_code":  "654321",
			"expires_in_minutes": "15",
		},
	})
	require.NoError(t, err)
	require.Len(t, gateway.events, 1)
	require.Equal(t, NotificationEmailEventAuthVerifyCode, gateway.events[0].Event)
	require.Equal(t, "person@example.com", gateway.events[0].Recipient)
	require.Equal(t, "en", gateway.events[0].Locale)
	require.Equal(t, map[string]string{
		"recipient_name":     "Person",
		"verification_code":  "654321",
		"expires_in_minutes": "15",
	}, gateway.events[0].Data)
	require.NotEmpty(t, gateway.events[0].DeliveryID)
	require.NotContains(t, gateway.events[0].DeliveryID, "person@example.com")

	configured, err := emailService.IsEmailConfigured(context.Background())
	require.NoError(t, err)
	require.True(t, configured)
}

func TestNotificationEmailGatewayDeliveryIDIsStablePerBusinessEvent(t *testing.T) {
	sourceEvent := TransactionalEmailEvent{
		Event:     NotificationEmailEventSubscriptionPurchaseSuccess,
		Recipient: "person@example.com",
		Locale:    "en",
		Data:      map[string]string{"order_id": "123"},
	}
	deliveryKey := notificationEmailDeliveryKey(sourceEvent.Event, "payment_order", "123", sourceEvent.Recipient, "")
	require.Equal(t, "business:"+deliveryKey, notificationEmailGatewayDeliveryID("", deliveryKey, sourceEvent))

	authEvent := TransactionalEmailEvent{
		Event:     NotificationEmailEventAuthVerifyCode,
		Recipient: "Person@Example.com",
		Locale:    "en",
		Data: map[string]string{
			"recipient_name":     "Person",
			"verification_code":  "654321",
			"expires_in_minutes": "15",
		},
	}
	first := notificationEmailGatewayDeliveryID("", "", authEvent)
	second := notificationEmailGatewayDeliveryID("", "", authEvent)
	require.Equal(t, first, second)
	require.NotContains(t, first, "person@example.com")

	authEvent.Data["verification_code"] = "654322"
	require.NotEqual(t, first, notificationEmailGatewayDeliveryID("", "", authEvent))
	require.Equal(t, "manual-business-id", notificationEmailGatewayDeliveryID("manual-business-id", deliveryKey, authEvent))
}

func TestNotificationEmailServiceDoesNotFallBackAfterGatewayDeliveryFailure(t *testing.T) {
	smtpServer := startNotificationEmailTestSMTPServer(t)
	repo := newNotificationEmailMemorySettingRepo()
	for key, value := range smtpServer.settings() {
		require.NoError(t, repo.Set(context.Background(), key, value))
	}
	emailService := NewEmailService(repo, nil)
	emailService.SetTransactionalEmailGateway(&transactionalEmailGatewayStub{err: errors.New("worker unavailable")})
	notifications := NewNotificationEmailService(repo, emailService)

	err := notifications.Send(context.Background(), NotificationEmailSendInput{
		Event:          NotificationEmailEventAuthVerifyCode,
		Locale:         "en",
		RecipientEmail: "person@example.com",
		RecipientName:  "Person",
		Variables: map[string]string{
			"verification_code":  "654321",
			"expires_in_minutes": "15",
		},
	})
	require.ErrorContains(t, err, "worker unavailable")
	require.Zero(t, smtpServer.messageCount())
}

type transactionalEmailGatewayStub struct {
	mu     sync.Mutex
	events []TransactionalEmailEvent
	err    error
}

func hostedOpsScheduledReportGatewayData() map[string]string {
	data := make(map[string]string, len(hostedOpsScheduledReportGatewayFields))
	for _, field := range hostedOpsScheduledReportGatewayFields {
		data[field] = "-"
	}
	data["recipient_name"] = "Ops"
	data["report_type"] = "daily_summary"
	data["report_start_time"] = "2026-07-18T01:00:26Z"
	data["report_end_time"] = "2026-07-19T01:00:26Z"
	return data
}

func (s *transactionalEmailGatewayStub) SendTransactionalEmail(_ context.Context, event TransactionalEmailEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return s.err
}
