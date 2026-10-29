package openclawgateway

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEnrollmentSetupContainsOnlyBoundedAdmissionCredential(t *testing.T) {
	e, _, now := enrollmentFixture(t)
	e.Client = "macos"
	ticket, err := NewEnrollmentTicket()
	if err != nil {
		t.Fatal(err)
	}
	otherTicket, err := NewEnrollmentTicket()
	if err != nil || ticket == otherTicket {
		t.Fatal("enrollment tickets reused entropy")
	}
	digest, err := EnrollmentTicketDigest(ticket)
	if err != nil || digest != sha256.Sum256([]byte(ticket)) {
		t.Fatal("ticket digest mismatch")
	}
	code, err := EnrollmentSetupCode(e, "https://{tenant}.claw.example.org", ticket, now)
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if json.Unmarshal(data, &payload) != nil || len(payload) != 3 || payload["url"] != "wss://u42-123456789abc.claw.example.org?clawhub_enrollment=enrollment-12345678" || payload["token"] != ticket || payload["expiresAtMs"] != float64(now.Add(5*time.Minute).UnixMilli()) {
		t.Fatal("setup payload differs from the pinned native wire contract")
	}
	if strings.Contains(string(data), `"bootstrapToken"`) || strings.Contains(string(data), `"password"`) {
		t.Fatal("unsafe credential in QR payload")
	}
	e.Client = "ios"
	code, err = EnrollmentSetupCode(e, "https://{tenant}.claw.example.org", ticket, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(code, "oc-pair://") {
		t.Fatal("iOS setup code is missing the native pairing wrapper")
	}
	data, err = base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, "oc-pair://"))
	if err != nil {
		t.Fatal(err)
	}
	payload = nil
	if json.Unmarshal(data, &payload) != nil || len(payload) != 3 || payload["bootstrapToken"] != ticket || payload["token"] != nil || payload["url"] != "wss://u42-123456789abc.claw.example.org?clawhub_enrollment=enrollment-12345678" {
		t.Fatal("iOS did not use the native one-time bootstrap handoff field")
	}
}

func TestEnrollmentAdmissionTicketIsTenantBoundAndUnusableAfterExpiry(t *testing.T) {
	secret := []byte(strings.Repeat("s", 32))
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	ticket, err := NewEnrollmentAdmissionTicket("u42-123456789abc", "enrollment-12345678", now.Add(5*time.Minute), secret)
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := VerifyEnrollmentAdmissionTicket(ticket, "u42-123456789abc", now, secret); !ok || id != "enrollment-12345678" {
		t.Fatal("valid admission rejected")
	}
	if _, ok := VerifyEnrollmentAdmissionTicket(ticket, "u43-123456789abc", now, secret); ok {
		t.Fatal("cross-tenant ticket accepted")
	}
	if _, ok := VerifyEnrollmentAdmissionTicket(ticket, "u42-123456789abc", now.Add(5*time.Minute), secret); ok {
		t.Fatal("expired ticket accepted")
	}
	if _, ok := VerifyEnrollmentAdmissionTicket(ticket+"x", "u42-123456789abc", now, secret); ok {
		t.Fatal("ticket tampering accepted")
	}
}

func TestEnrollmentSetupFailsClosedForUnsupportedOrExpiredHandoffs(t *testing.T) {
	ticket, err := NewEnrollmentTicket()
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"", "owner-bootstrap-token", "ag-enroll-v1.short", enrollmentTicketPrefix + strings.Repeat("A", 44), ticket + "=", " " + ticket} {
		if _, err := EnrollmentTicketDigest(input); err == nil {
			t.Fatal("invalid enrollment credential accepted")
		}
	}
	for _, change := range []func(*Enrollment){
		func(e *Enrollment) { e.Client = "android" },
		func(e *Enrollment) { e.State = EnrollmentPending },
		func(e *Enrollment) { e.State = EnrollmentCancelled },
		func(e *Enrollment) { e.ExpiresAt = e.ExpiresAt.Add(time.Second) },
		func(e *Enrollment) { e.Tenant = "other.example.org" },
	} {
		e, _, now := enrollmentFixture(t)
		change(&e)
		if _, err := EnrollmentSetupCode(e, "https://{tenant}.claw.example.org", ticket, now); err == nil {
			t.Fatal("unsafe or unsupported setup encoded")
		}
	}
	e, _, now := enrollmentFixture(t)
	for _, when := range []time.Time{now.Add(-time.Second), e.ExpiresAt, e.ExpiresAt.Add(time.Second)} {
		if _, err := EnrollmentSetupCode(e, "https://{tenant}.claw.example.org", ticket, when); err == nil {
			t.Fatal("setup validity window bypassed")
		}
	}
	for _, template := range []string{"http://{tenant}.claw.example.org", "https://shared.example.org", "https://{tenant}.claw.example.org/path"} {
		if _, err := EnrollmentSetupCode(e, template, ticket, now); err == nil {
			t.Fatal("unsafe gateway origin in setup")
		}
	}
}
