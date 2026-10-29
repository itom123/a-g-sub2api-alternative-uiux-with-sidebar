package openclawgateway

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

const enrollmentTicketPrefix = "ag-enroll-v1."

type enrollmentTicketClaims struct {
	Tenant       string `json:"tenant"`
	EnrollmentID string `json:"enrollment_id"`
	ExpiresAtMS  int64  `json:"expires_at_ms"`
}

// NewEnrollmentTicket creates an admission credential, never a gateway owner
// credential. The native admission adapter must recognize this separate
// namespace and require the exact owner's approval before granting access.
func NewEnrollmentTicket() (string, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", err
	}
	return enrollmentTicketPrefix + base64.RawURLEncoding.EncodeToString(secret[:]), nil
}

func NewEnrollmentAdmissionTicket(tenant, enrollmentID string, expiresAt time.Time, secret []byte) (string, error) {
	if !TenantPattern.MatchString(tenant) || !enrollmentRequestID(enrollmentID) || len(secret) < 32 || expiresAt.IsZero() {
		return "", ErrEnrollmentTransition
	}
	claims, err := json.Marshal(enrollmentTicketClaims{Tenant: tenant, EnrollmentID: enrollmentID, ExpiresAtMS: expiresAt.UnixMilli()})
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(claims)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(enrollmentTicketPrefix + encoded))
	return enrollmentTicketPrefix + encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func VerifyEnrollmentAdmissionTicket(ticket, tenant string, now time.Time, secret []byte) (string, bool) {
	if len(secret) < 32 || !TenantPattern.MatchString(tenant) || !strings.HasPrefix(ticket, enrollmentTicketPrefix) {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(ticket, enrollmentTicketPrefix), ".")
	if len(parts) != 2 {
		return "", false
	}
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	supplied, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(enrollmentTicketPrefix + parts[0]))
	if !hmac.Equal(supplied, mac.Sum(nil)) {
		return "", false
	}
	var claims enrollmentTicketClaims
	if json.Unmarshal(claimsBytes, &claims) != nil || claims.Tenant != tenant || !enrollmentRequestID(claims.EnrollmentID) || claims.ExpiresAtMS <= now.UnixMilli() {
		return "", false
	}
	return claims.EnrollmentID, true
}

// EnrollmentTicketDigest is the only ticket representation permitted in
// controller persistence. Ticket verification is not device authentication:
// the gateway still has to verify a fresh device signature before binding it.
func EnrollmentTicketDigest(ticket string) ([32]byte, error) {
	var empty [32]byte
	if !strings.HasPrefix(ticket, enrollmentTicketPrefix) {
		return empty, ErrEnrollmentTransition
	}
	encoded := strings.TrimPrefix(ticket, enrollmentTicketPrefix)
	if strings.Contains(encoded, ".") {
		parts := strings.Split(encoded, ".")
		if len(parts) != 2 {
			return empty, ErrEnrollmentTransition
		}
		claims, claimsErr := base64.RawURLEncoding.DecodeString(parts[0])
		signature, signatureErr := base64.RawURLEncoding.DecodeString(parts[1])
		if claimsErr != nil || signatureErr != nil || len(claims) == 0 || len(signature) != sha256.Size {
			return empty, ErrEnrollmentTransition
		}
	} else {
		secret, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil || len(secret) != 32 || base64.RawURLEncoding.EncodeToString(secret) != encoded {
			return empty, ErrEnrollmentTransition
		}
	}
	return sha256.Sum256([]byte(ticket)), nil
}

// EnrollmentSetupCode uses the pinned native iOS SetupPayload wire shape.
// Its token is only an AG_> admission ticket, never a gateway operator token.
// Native device proof must verify the original signed token and scopes before
// the enrollment authority admits it; the relay must preserve those bytes.
func EnrollmentSetupCode(e Enrollment, originTemplate, ticket string, now time.Time) (string, error) {
	if (e.Client != "ios" && e.Client != "macos") || e.State != EnrollmentCreated || e.Binding != nil || e.CreatedAt.IsZero() || now.Before(e.CreatedAt) || !now.Before(e.ExpiresAt) || !e.ExpiresAt.Equal(e.CreatedAt.Add(EnrollmentTTL)) {
		return "", ErrEnrollmentTransition
	}
	if _, err := EnrollmentTicketDigest(ticket); err != nil {
		return "", err
	}
	origin, err := PublicOrigin(originTemplate, e.Tenant)
	if err != nil {
		return "", ErrEnrollmentTransition
	}
	origin.Scheme = "wss"
	query := origin.Query()
	query.Set("clawhub_enrollment", e.ID)
	origin.RawQuery = query.Encode()
	payload := struct {
		URL            string `json:"url"`
		ExpiresAtMS    int64  `json:"expiresAtMs"`
		Token          string `json:"token,omitempty"`
		BootstrapToken string `json:"bootstrapToken,omitempty"`
	}{URL: origin.String(), ExpiresAtMS: e.ExpiresAt.UnixMilli()}
	if e.Client == "ios" {
		payload.BootstrapToken = ticket
	} else {
		payload.Token = ticket
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	// The official iOS client accepts both the bare payload and the
	// `oc-pair://` wrapper. The wrapper lets iOS route a camera scan to
	// OpenClaw instead of treating the payload as a generic web link. Keep the
	// Mac setup code bare because its editor expects the pasted payload.
	if e.Client == "ios" {
		return "oc-pair://" + encoded, nil
	}
	return encoded, nil
}
