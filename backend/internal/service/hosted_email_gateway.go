package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	hostedEmailGatewayURLEnv        = "HOSTED_EMAIL_GATEWAY_URL"
	hostedEmailGatewaySecretFileEnv = "HOSTED_EMAIL_GATEWAY_HMAC_SECRET_FILE"
	// Fixed Cloudflare-rendered templates intentionally replace the editable
	// admin templates for supported events, so activation must be explicit.
	hostedEmailGatewayFixedTemplatesEnv = "HOSTED_EMAIL_GATEWAY_FIXED_TEMPLATES"

	hostedEmailGatewayPath             = "/v1/transactional"
	hostedEmailGatewayTimeout          = 15 * time.Second
	hostedEmailGatewayMaxDataValue     = 8 * 1024
	hostedEmailGatewayMaxPayloadBytes  = 64 * 1024
	hostedEmailGatewayMaxDeliveryID    = 512
	hostedEmailGatewayMinSecretBytes   = sha256.Size
	hostedEmailGatewayDeliveryIDPrefix = "g1_"
)

// TransactionalEmailEvent is the narrow, domain-bound request accepted by the
// dedicated Hosted Cloudflare Worker. It deliberately has no From, CC, BCC,
// attachment, subject, text, or HTML fields.
type TransactionalEmailEvent struct {
	Event     string
	Recipient string
	Locale    string
	Data      map[string]string
	// DeliveryID is an internal business idempotency key. It is never sent
	// verbatim: the gateway derives an opaque HMAC value for the Worker header.
	DeliveryID string
}

// TransactionalEmailGateway is implemented by an outbound provider that can
// deliver allowlisted transactional events.
type TransactionalEmailGateway interface {
	SendTransactionalEmail(context.Context, TransactionalEmailEvent) error
}

type hostedCloudflareEmailGateway struct {
	endpoint *url.URL
	secret   []byte
	client   *http.Client
	now      func() time.Time
	nonce    func() (string, error)
}

type hostedCloudflareEmailGatewayPayload struct {
	Event     string            `json:"event"`
	Recipient string            `json:"recipient"`
	Locale    string            `json:"locale"`
	Data      map[string]string `json:"data"`
}

var hostedCloudflareEmailGatewayEventFields = map[string][]string{
	NotificationEmailEventAuthVerifyCode: {
		"recipient_name", "verification_code", "expires_in_minutes",
	},
	NotificationEmailEventAuthPasswordReset: {
		"recipient_name", "reset_url", "expires_in_minutes",
	},
	NotificationEmailEventNotificationEmailVerifyCode: {
		"recipient_name", "verification_code", "expires_in_minutes",
	},
	NotificationEmailEventSubscriptionPurchaseSuccess: {
		"recipient_name", "subscription_group", "subscription_days", "expiry_time", "order_id",
	},
	NotificationEmailEventSubscriptionExpiryReminder: {
		"recipient_name", "subscription_group", "expiry_time", "days_remaining", "unsubscribe_url",
	},
	NotificationEmailEventBalanceLow: {
		"recipient_name", "current_balance", "threshold", "recharge_url", "unsubscribe_url",
	},
	NotificationEmailEventBalanceRechargeSuccess: {
		"recipient_name", "recharge_amount", "current_balance", "order_id",
	},
	NotificationEmailEventAccountQuotaAlert: {
		"recipient_name", "account_id", "account_name", "platform", "quota_dimension", "quota_used", "quota_limit", "quota_remaining", "quota_threshold",
	},
	NotificationEmailEventContentModerationViolation: {
		"recipient_name", "triggered_at", "group_name", "moderation_category", "moderation_score", "violation_count", "ban_threshold",
	},
	NotificationEmailEventContentModerationDisabled: {
		"recipient_name", "triggered_at", "group_name", "moderation_category", "moderation_score", "violation_count", "ban_threshold",
	},
	NotificationEmailEventCyberPolicyNotice: {
		"recipient_name", "triggered_at", "model", "group_name", "upstream_message",
	},
	NotificationEmailEventOpsAlert: {
		"recipient_name", "rule_name", "severity", "alert_status", "metric_type", "operator", "metric_value", "threshold_value", "triggered_at", "alert_description",
	},
	NotificationEmailEventOpsScheduledReport: hostedOpsScheduledReportGatewayFields,
}

const hostedOpsScheduledReportMaxErrorRows = 10

// hostedOpsScheduledReportGatewayFields is intentionally a fixed scalar
// contract. In particular, it has no HTML or free-form MIME field: error
// entries are capped to ten single-line text rows and are escaped by the
// Worker when it renders them.
var hostedOpsScheduledReportGatewayFields = func() []string {
	fields := []string{
		"recipient_name",
		"report_type",
		"report_start_time",
		"report_end_time",
		"report_total_requests",
		"report_success_count",
		"report_sla_error_count",
		"report_business_limited_count",
		"report_sla",
		"report_error_rate",
		"report_upstream_error_rate",
		"report_upstream_error_count_excl_429_529",
		"report_upstream_429_count",
		"report_upstream_529_count",
		"report_latency_p50",
		"report_latency_p99",
		"report_ttft_p50",
		"report_ttft_p99",
		"report_tokens",
		"report_qps_current",
		"report_qps_peak",
		"report_qps_avg",
		"report_tps_current",
		"report_tps_peak",
		"report_tps_avg",
		"report_error_total",
		"report_accounts_total",
		"report_accounts_available",
		"report_accounts_rate_limited",
		"report_accounts_error",
	}
	for index := 1; index <= hostedOpsScheduledReportMaxErrorRows; index++ {
		fields = append(fields, fmt.Sprintf("report_error_row_%d", index))
	}
	return fields
}()

// NewHostedCloudflareEmailGatewayFromEnvironment enables the dedicated Hosted
// Worker only when both settings are present. The HMAC secret is loaded from a
// mounted file so it is never stored in the settings database or submitted by
// the administrator UI. It is a narrow Worker credential, not a Cloudflare
// account or SMTP token.
func NewHostedCloudflareEmailGatewayFromEnvironment() (TransactionalEmailGateway, error) {
	rawEndpoint := strings.TrimSpace(os.Getenv(hostedEmailGatewayURLEnv))
	secretFile := strings.TrimSpace(os.Getenv(hostedEmailGatewaySecretFileEnv))
	if rawEndpoint == "" && secretFile == "" {
		return nil, nil
	}
	if rawEndpoint == "" || secretFile == "" {
		return nil, fmt.Errorf("%s and %s must be configured together", hostedEmailGatewayURLEnv, hostedEmailGatewaySecretFileEnv)
	}

	endpoint, err := validateHostedEmailGatewayEndpoint(rawEndpoint)
	if err != nil {
		return nil, err
	}
	secret, err := os.ReadFile(secretFile) // #nosec G304 -- deployment configuration controls this local secret path.
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", hostedEmailGatewaySecretFileEnv, err)
	}
	secret = bytes.TrimSpace(secret)
	if err := validateHostedEmailGatewaySigningSecret(secret); err != nil {
		return nil, fmt.Errorf("%s %w", hostedEmailGatewaySecretFileEnv, err)
	}

	return newHostedCloudflareEmailGateway(endpoint, secret, nil), nil
}

// validateHostedEmailGatewaySigningSecret accepts textual base64url keys only.
// Cloudflare Worker secrets are JavaScript strings, so keeping an arbitrary raw
// binary file here would make the backend HMAC bytes differ from the Worker's
// UTF-8 encoded secret. The encoded text itself is the HMAC key on both sides.
func validateHostedEmailGatewaySigningSecret(secret []byte) error {
	if len(secret) == 0 {
		return errors.New("must contain unpadded base64url text encoding at least 32 random bytes")
	}
	for _, value := range secret {
		if !((value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z') || (value >= '0' && value <= '9') || value == '-' || value == '_') {
			return errors.New("must contain unpadded base64url text encoding at least 32 random bytes")
		}
	}
	decoded, err := base64.RawURLEncoding.DecodeString(string(secret))
	if err != nil || len(decoded) < hostedEmailGatewayMinSecretBytes || base64.RawURLEncoding.EncodeToString(decoded) != string(secret) {
		return errors.New("must contain canonical unpadded base64url text encoding at least 32 random bytes")
	}
	return nil
}

func newHostedCloudflareEmailGateway(endpoint *url.URL, secret []byte, client *http.Client) *hostedCloudflareEmailGateway {
	if client == nil {
		client = &http.Client{
			Timeout: hostedEmailGatewayTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				// A redirect could expose the signed body to an unintended origin.
				return http.ErrUseLastResponse
			},
		}
	}
	return &hostedCloudflareEmailGateway{
		endpoint: endpoint,
		secret:   append([]byte(nil), secret...),
		client:   client,
		now:      time.Now,
		nonce:    generateHostedEmailGatewayNonce,
	}
}

func validateHostedEmailGatewayEndpoint(raw string) (*url.URL, error) {
	endpoint, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", hostedEmailGatewayURLEnv, err)
	}
	if endpoint.Scheme != "https" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, fmt.Errorf("%s must be an HTTPS URL without credentials, query, or fragment", hostedEmailGatewayURLEnv)
	}
	if endpoint.Path != hostedEmailGatewayPath {
		return nil, fmt.Errorf("%s path must be %s", hostedEmailGatewayURLEnv, hostedEmailGatewayPath)
	}
	if port := endpoint.Port(); port != "" && port != "443" {
		return nil, fmt.Errorf("%s may only use HTTPS port 443", hostedEmailGatewayURLEnv)
	}
	host := strings.TrimSuffix(strings.ToLower(endpoint.Hostname()), ".")
	if host != "example.com" && !strings.HasSuffix(host, ".example.com") {
		return nil, fmt.Errorf("%s host must be example.com or an example.com subdomain", hostedEmailGatewayURLEnv)
	}
	return endpoint, nil
}

func hostedEmailGatewayFixedTemplatesEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(hostedEmailGatewayFixedTemplatesEnv)), "true")
}

func (g *hostedCloudflareEmailGateway) SendTransactionalEmail(ctx context.Context, event TransactionalEmailEvent) error {
	if g == nil || g.endpoint == nil || len(g.secret) == 0 || g.client == nil {
		return errors.New("hosted email gateway is not configured")
	}
	payload, err := hostedEmailGatewayPayloadForEvent(event)
	if err != nil {
		return err
	}
	deliveryID, err := hostedEmailGatewayOpaqueDeliveryID(g.secret, event.DeliveryID)
	if err != nil {
		return err
	}
	rawBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode hosted email event: %w", err)
	}
	if len(rawBody) > hostedEmailGatewayMaxPayloadBytes {
		return fmt.Errorf("hosted email event exceeds %d bytes", hostedEmailGatewayMaxPayloadBytes)
	}

	nonce, err := g.nonce()
	if err != nil {
		return fmt.Errorf("generate hosted email nonce: %w", err)
	}
	timestamp := strconv.FormatInt(g.now().UTC().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint.String(), bytes.NewReader(rawBody))
	if err != nil {
		return fmt.Errorf("create hosted email request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Email-Timestamp", timestamp)
	req.Header.Set("X-Email-Nonce", nonce)
	req.Header.Set("X-Email-Signature", hostedEmailGatewaySignature(g.secret, timestamp, nonce, rawBody))
	if deliveryID != "" {
		// This is deliberately a header rather than a body field. Deployed
		// Workers that predate delivery-id support validate the body exactly but
		// safely ignore unknown headers during the rolling upgrade.
		req.Header.Set("X-Email-Delivery-ID", deliveryID)
	}

	// Do not retry here. A network failure can occur after the Worker has
	// delivered the message; a new nonce would risk a duplicate delivery.
	response, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("send hosted email event: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("hosted email gateway returned status %d", response.StatusCode)
	}
	return nil
}

func hostedEmailGatewayOpaqueDeliveryID(secret []byte, raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if len(raw) > hostedEmailGatewayMaxDeliveryID {
		return "", fmt.Errorf("Hosted email delivery id exceeds %d bytes", hostedEmailGatewayMaxDeliveryID)
	}
	if raw != strings.TrimSpace(raw) || strings.ContainsAny(raw, "\r\n") {
		return "", errors.New("Hosted email delivery id must be a trimmed single-line value")
	}
	for _, value := range raw {
		if value < 0x20 || value == 0x7f {
			return "", errors.New("Hosted email delivery id contains an unsafe control character")
		}
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("hosted-email-delivery-id-v1\x00"))
	_, _ = mac.Write([]byte(raw))
	return hostedEmailGatewayDeliveryIDPrefix + hex.EncodeToString(mac.Sum(nil)), nil
}

func hostedEmailGatewayPayloadForEvent(event TransactionalEmailEvent) (hostedCloudflareEmailGatewayPayload, error) {
	fields, ok := hostedCloudflareEmailGatewayEventFields[event.Event]
	if !ok {
		return hostedCloudflareEmailGatewayPayload{}, fmt.Errorf("Hosted email gateway does not support event %q", event.Event)
	}
	if event.Locale != notificationEmailDefaultLocale && event.Locale != notificationEmailLocaleChinese {
		return hostedCloudflareEmailGatewayPayload{}, fmt.Errorf("unsupported Hosted email locale %q", event.Locale)
	}
	recipient, err := parseSMTPAddress(event.Recipient, "recipient")
	if err != nil {
		return hostedCloudflareEmailGatewayPayload{}, err
	}
	data := make(map[string]string, len(fields))
	allowed := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		allowed[field] = struct{}{}
		value, ok := event.Data[field]
		if !ok {
			return hostedCloudflareEmailGatewayPayload{}, fmt.Errorf("missing Hosted email data field %q for event %q", field, event.Event)
		}
		if len(value) > hostedEmailGatewayMaxDataValue {
			return hostedCloudflareEmailGatewayPayload{}, fmt.Errorf("Hosted email data field %q exceeds %d bytes", field, hostedEmailGatewayMaxDataValue)
		}
		if isHostedEmailGatewayActionURLField(field) {
			var err error
			value, err = normalizeHostedEmailGatewayActionURL(value)
			if err != nil {
				return hostedCloudflareEmailGatewayPayload{}, fmt.Errorf("invalid Hosted email data field %q: %w", field, err)
			}
		}
		data[field] = value
	}
	for field := range event.Data {
		if _, ok := allowed[field]; !ok {
			return hostedCloudflareEmailGatewayPayload{}, fmt.Errorf("unsupported Hosted email data field %q for event %q", field, event.Event)
		}
	}
	return hostedCloudflareEmailGatewayPayload{
		Event:     event.Event,
		Recipient: recipient.Address,
		Locale:    event.Locale,
		Data:      data,
	}, nil
}

func isHostedEmailGatewayActionURLField(field string) bool {
	switch field {
	case "reset_url", "recharge_url", "unsubscribe_url":
		return true
	default:
		return false
	}
}

func normalizeHostedEmailGatewayActionURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || value != raw || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("must be a non-empty single-line first-party HTTPS URL")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", errors.New("must be a valid first-party HTTPS URL")
	}
	if parsed.Scheme != "https" || parsed.User != nil {
		return "", errors.New("must be a first-party HTTPS URL without credentials")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "example.com" && host != "api.example.com" {
		return "", errors.New("host must be example.com or api.example.com")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return "", errors.New("may only use HTTPS port 443")
	}
	return parsed.String(), nil
}

func hostedEmailGatewayEventFromNotification(event, recipient, locale string, variables map[string]string) (TransactionalEmailEvent, bool) {
	fields, ok := hostedCloudflareEmailGatewayEventFields[event]
	if !ok {
		return TransactionalEmailEvent{}, false
	}
	data := make(map[string]string, len(fields))
	for _, field := range fields {
		data[field] = variables[field]
	}
	return TransactionalEmailEvent{
		Event:     event,
		Recipient: recipient,
		Locale:    locale,
		Data:      data,
	}, true
}

func hostedEmailGatewaySignature(secret []byte, timestamp, nonce string, rawBody []byte) string {
	bodyHash := sha256.Sum256(rawBody)
	canonical := timestamp + "." + nonce + "." + hex.EncodeToString(bodyHash[:])
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func generateHostedEmailGatewayNonce() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
