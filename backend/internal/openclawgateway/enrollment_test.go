package openclawgateway

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func enrollmentFixture(t *testing.T) (Enrollment, EnrollmentBinding, time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	e, err := NewEnrollment("enrollment-12345678", 42, 7, "u42-123456789abc", "ios", now)
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	key[0] = 17
	digest := sha256.Sum256(key)
	return e, EnrollmentBinding{RequestID: "request-12345678", DeviceID: hex.EncodeToString(digest[:]), PublicKey: base64.RawURLEncoding.EncodeToString(key), Roles: []string{"node", "operator"}, Scopes: []string{"operator.read", "operator.write"}}, now
}

func TestEnrollmentRequiresExactOwnerApprovalAndDelivery(t *testing.T) {
	e, b, now := enrollmentFixture(t)
	if e.Connected(b, now) == nil {
		t.Fatal("unapproved device connected")
	}
	if err := e.Bind(b, now); err != nil {
		t.Fatal(err)
	}
	if err := e.Bind(b, now); err != nil {
		t.Fatal("same-device handshake retry must be safe", err)
	}
	for _, owner := range [][2]int64{{43, 7}, {42, 8}} {
		if e.Approve(owner[0], owner[1], b, now) == nil {
			t.Fatal("another owner/workspace approved device")
		}
	}
	for _, change := range []func(*EnrollmentBinding){
		func(b *EnrollmentBinding) { b.RequestID = "request-replaced" },
		func(b *EnrollmentBinding) { b.DeviceID = strings.Repeat("f", 64) },
		func(b *EnrollmentBinding) { b.Roles = []string{"operator"} },
		func(b *EnrollmentBinding) { b.Scopes = []string{"operator.read"} },
	} {
		replacement := b
		change(&replacement)
		if e.Bind(replacement, now) == nil || e.Approve(42, 7, replacement, now) == nil {
			t.Fatal("changed request accepted")
		}
	}
	if err := e.Approve(42, 7, b, now); err != nil {
		t.Fatal(err)
	}
	if e.State != EnrollmentApproved {
		t.Fatal("approval falsely recorded as connected")
	}
	if e.Approve(42, 7, b, now) == nil {
		t.Fatal("approval replay accepted")
	}
	if err := e.Connected(b, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if e.Expire(now.Add(24*time.Hour)) || e.State != EnrollmentConnected {
		t.Fatal("bootstrap expiry changed an established device")
	}
}

func TestEnrollmentRejectsEscalationAndMalformedIdentity(t *testing.T) {
	for _, change := range []func(*EnrollmentBinding){
		func(b *EnrollmentBinding) { b.Scopes = []string{"operator.admin"} },
		func(b *EnrollmentBinding) { b.Scopes = []string{"operator.pairing"} },
		func(b *EnrollmentBinding) { b.Scopes = []string{"operator.read", "operator.read"} },
		func(b *EnrollmentBinding) { b.Roles = []string{"operator", "operator"} },
		func(b *EnrollmentBinding) { b.Roles = []string{"node"} },
		func(b *EnrollmentBinding) { b.Roles = []string{"admin"} },
		func(b *EnrollmentBinding) { b.PublicKey += "=" },
		func(b *EnrollmentBinding) { b.PublicKey = "not-an-ed25519-key" },
		func(b *EnrollmentBinding) { b.DeviceID = strings.Repeat("0", 64) },
		func(b *EnrollmentBinding) { b.RequestID = "../../device" },
	} {
		e, b, now := enrollmentFixture(t)
		change(&b)
		if e.Bind(b, now) == nil || e.State != EnrollmentCreated || e.Binding != nil {
			t.Fatal("unsafe identity or permissions bound")
		}
	}
	e, b, now := enrollmentFixture(t)
	e.Client = "macos"
	if e.Bind(b, now) == nil {
		t.Fatal("macOS enrollment accepted mobile node role")
	}
}

func TestEnrollmentExpiryIsEnforcedAtEveryTransition(t *testing.T) {
	for _, state := range []EnrollmentState{EnrollmentCreated, EnrollmentPending, EnrollmentApproved} {
		t.Run(string(state), func(t *testing.T) {
			e, b, now := enrollmentFixture(t)
			if state != EnrollmentCreated {
				if err := e.Bind(b, now); err != nil {
					t.Fatal(err)
				}
			}
			if state == EnrollmentApproved {
				if err := e.Approve(42, 7, b, now); err != nil {
					t.Fatal(err)
				}
			}
			expiry := now.Add(EnrollmentTTL)
			if e.Bind(b, expiry) == nil || e.Approve(42, 7, b, expiry) == nil || e.Connected(b, expiry) == nil || e.Close(42, 7, EnrollmentCancelled, expiry) == nil || e.State != EnrollmentExpired {
				t.Fatal("expired enrollment remained usable")
			}
		})
	}
}

func TestEnrollmentClosedRequestsCannotBeReopened(t *testing.T) {
	for _, state := range []EnrollmentState{EnrollmentCancelled, EnrollmentRejected} {
		e, b, now := enrollmentFixture(t)
		if err := e.Bind(b, now); err != nil {
			t.Fatal(err)
		}
		if e.Close(43, 7, state, now) == nil {
			t.Fatal("other owner closed enrollment")
		}
		if err := e.Close(42, 7, state, now); err != nil {
			t.Fatal(err)
		}
		if e.Bind(b, now) == nil || e.Approve(42, 7, b, now) == nil || e.Connected(b, now) == nil || e.State != state {
			t.Fatal("terminal enrollment reopened")
		}
	}
}

func TestEnrollmentBindingDoesNotAliasCallerOrExposePrivateMetadata(t *testing.T) {
	e, b, now := enrollmentFixture(t)
	if err := e.Bind(b, now); err != nil {
		t.Fatal(err)
	}
	b.Roles[0], b.Scopes[0] = "admin", "operator.admin"
	if e.Binding.Roles[0] != "node" || e.Binding.Scopes[0] != "operator.read" {
		t.Fatal("caller mutated the reviewed grant")
	}
	encoded, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{e.Binding.PublicKey, e.Tenant, "owner_id", "instance_id", "public_key", "token"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("enrollment API leaked private metadata", private)
		}
	}
}

func TestEnrollmentBindingCarriesClaimedNativeNameAndPlatform(t *testing.T) {
	e, b, _ := enrollmentFixture(t)
	b.EnrollmentID = e.ID
	b.DisplayName = "Studio Mac"
	b.Platform = "macOS"
	if !validEnrollmentBinding(b, e.Client) {
		t.Fatal("claimed native metadata rejected")
	}
	r := EnrollmentReceipt{ID: e.ID, Client: e.Client, State: EnrollmentPending, CreatedAtMS: e.CreatedAt.UnixMilli(), ExpiresAtMS: e.ExpiresAt.UnixMilli(), Binding: func() *EnrollmentBindingTransport { v := b.Transport(); return &v }()}
	if r.Validate() != nil || r.Binding.DisplayName != "Studio Mac" || r.Binding.Platform != "macOS" {
		t.Fatal("claimed native metadata was not retained")
	}
	b.DisplayName = string(make([]byte, 129))
	if validEnrollmentBinding(b, e.Client) {
		t.Fatal("oversized claimed name accepted")
	}
}
