package openclawgateway

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"slices"
	"time"
)

const EnrollmentTTL = 5 * time.Minute

type EnrollmentState string

const (
	EnrollmentCreated   EnrollmentState = "created"
	EnrollmentPending   EnrollmentState = "awaiting_approval"
	EnrollmentApproved  EnrollmentState = "approved"
	EnrollmentConnected EnrollmentState = "connected"
	EnrollmentExpired   EnrollmentState = "expired"
	EnrollmentRejected  EnrollmentState = "rejected"
	EnrollmentCancelled EnrollmentState = "cancelled"
)

var ErrEnrollmentTransition = errors.New("device enrollment changed or is unavailable")

// Enrollment contains no bearer credential. Native admission and owner approval
// must serialize mutations of this record in the same persistent authority.
// This type alone does not authenticate a client or authorize a connection.
type Enrollment struct {
	ID                string             `json:"id"`
	OwnerID           int64              `json:"-"`
	InstanceID        int64              `json:"-"`
	Tenant            string             `json:"-"`
	Client            string             `json:"client"`
	State             EnrollmentState    `json:"state"`
	CreatedAt         time.Time          `json:"created_at"`
	ExpiresAt         time.Time          `json:"expires_at"`
	Binding           *EnrollmentBinding `json:"device,omitempty"`
	DeliveryUncertain bool               `json:"delivery_uncertain,omitempty"`
}

// The public key is retained for exact native approval but never returned by
// the owner API. DeviceID is SHA-256 of the canonical Ed25519 public key.
type EnrollmentBinding struct {
	EnrollmentID string   `json:"-"`
	RequestID    string   `json:"request_id"`
	DeviceID     string   `json:"device_id"`
	DisplayName  string   `json:"display_name,omitempty"`
	Platform     string   `json:"platform,omitempty"`
	PublicKey    string   `json:"-"`
	Roles        []string `json:"roles"`
	Scopes       []string `json:"scopes"`
}

// EnrollmentBindingTransport is used only on the private controller/worker
// bridge. It carries the public key needed for exact approval without exposing
// it through the owner-facing Enrollment JSON shape.
type EnrollmentBindingTransport struct {
	EnrollmentID string   `json:"enrollment_id,omitempty"`
	RequestID    string   `json:"request_id"`
	DeviceID     string   `json:"device_id"`
	DisplayName  string   `json:"display_name,omitempty"`
	Platform     string   `json:"platform,omitempty"`
	PublicKey    string   `json:"public_key"`
	Roles        []string `json:"roles"`
	Scopes       []string `json:"scopes"`
}

func (b EnrollmentBinding) Transport() EnrollmentBindingTransport {
	return EnrollmentBindingTransport{EnrollmentID: b.EnrollmentID, RequestID: b.RequestID, DeviceID: b.DeviceID, DisplayName: b.DisplayName, Platform: b.Platform, PublicKey: b.PublicKey, Roles: b.Roles, Scopes: b.Scopes}
}

func (b EnrollmentBindingTransport) Binding() EnrollmentBinding {
	return EnrollmentBinding{EnrollmentID: b.EnrollmentID, RequestID: b.RequestID, DeviceID: b.DeviceID, DisplayName: b.DisplayName, Platform: b.Platform, PublicKey: b.PublicKey, Roles: b.Roles, Scopes: b.Scopes}
}

func NewEnrollment(id string, ownerID, instanceID int64, tenant, client string, now time.Time) (Enrollment, error) {
	if !enrollmentRequestID(id) || ownerID <= 0 || instanceID <= 0 || !TenantPattern.MatchString(tenant) || (client != "ios" && client != "macos") || now.IsZero() {
		return Enrollment{}, ErrEnrollmentTransition
	}
	return Enrollment{ID: id, OwnerID: ownerID, InstanceID: instanceID, Tenant: tenant, Client: client, State: EnrollmentCreated, CreatedAt: now.UTC(), ExpiresAt: now.UTC().Add(EnrollmentTTL)}, nil
}

func enrollmentRequestID(id string) bool {
	if len(id) < 8 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (e *Enrollment) Expire(now time.Time) bool {
	if (e.State == EnrollmentCreated || e.State == EnrollmentPending || e.State == EnrollmentApproved) && !now.Before(e.ExpiresAt) {
		e.State = EnrollmentExpired
		return true
	}
	return false
}

func (e *Enrollment) live(now time.Time) bool {
	e.Expire(now)
	return !now.Before(e.CreatedAt) && now.Before(e.ExpiresAt)
}

// Bind is called only after native signature verification. A second identity or
// replacement request cannot take over an enrollment, even for the same key.
func (e *Enrollment) Bind(binding EnrollmentBinding, now time.Time) error {
	if !e.live(now) || (e.State != EnrollmentCreated && e.State != EnrollmentPending) || !validEnrollmentBinding(binding, e.Client) {
		return ErrEnrollmentTransition
	}
	if e.Binding != nil {
		if !sameEnrollmentBinding(*e.Binding, binding) {
			return ErrEnrollmentTransition
		}
		return nil
	}
	copyBinding := binding
	copyBinding.Roles = slices.Clone(binding.Roles)
	copyBinding.Scopes = slices.Clone(binding.Scopes)
	e.Binding, e.State = &copyBinding, EnrollmentPending
	return nil
}

func validEnrollmentBinding(b EnrollmentBinding, client string) bool {
	if !enrollmentRequestID(b.RequestID) {
		return false
	}
	if len(b.DisplayName) > 128 || len(b.Platform) > 64 {
		return false
	}
	key, err := base64.RawURLEncoding.DecodeString(b.PublicKey)
	if err != nil || len(key) != 32 || base64.RawURLEncoding.EncodeToString(key) != b.PublicKey {
		return false
	}
	digest := sha256.Sum256(key)
	if b.DeviceID != hex.EncodeToString(digest[:]) {
		return false
	}
	if len(b.Roles) == 0 || len(b.Roles) > 2 || len(b.Scopes) > 5 {
		return false
	}
	roles := map[string]bool{}
	for _, role := range b.Roles {
		if roles[role] || (role != "operator" && !(client == "ios" && role == "node")) {
			return false
		}
		roles[role] = true
	}
	scopes := map[string]bool{}
	for _, scope := range b.Scopes {
		if !roles["operator"] || scopes[scope] {
			return false
		}
		switch scope {
		case "operator.read", "operator.write", "operator.approvals", "operator.questions", "operator.talk.secrets":
		default:
			return false
		}
		scopes[scope] = true
	}
	return true
}

func sameEnrollmentBinding(a, b EnrollmentBinding) bool {
	return a.RequestID == b.RequestID && a.DeviceID == b.DeviceID && a.DisplayName == b.DisplayName && a.Platform == b.Platform && a.PublicKey == b.PublicKey && sameEnrollmentSet(a.Roles, b.Roles) && sameEnrollmentSet(a.Scopes, b.Scopes)
}

func sameEnrollmentSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, value := range a {
		if !slices.Contains(b, value) {
			return false
		}
	}
	return true
}

// Approve records the owner decision, not successful credential delivery. The
// adapter must atomically recheck this binding and native pending request before
// issuing credentials; an Approved record is never itself a bearer grant.
func (e *Enrollment) Approve(ownerID, instanceID int64, reviewed EnrollmentBinding, now time.Time) error {
	if !e.live(now) || e.State != EnrollmentPending || e.OwnerID != ownerID || e.InstanceID != instanceID || e.Binding == nil || !validEnrollmentBinding(reviewed, e.Client) || !sameEnrollmentBinding(*e.Binding, reviewed) {
		return ErrEnrollmentTransition
	}
	e.State = EnrollmentApproved
	return nil
}

// Connected requires an authenticated native credential-delivery receipt for
// the exact approved device. Inventory presence alone is not such a receipt.
func (e *Enrollment) Connected(binding EnrollmentBinding, now time.Time) error {
	if !e.live(now) || e.State != EnrollmentApproved || e.Binding == nil || !validEnrollmentBinding(binding, e.Client) || !sameEnrollmentBinding(*e.Binding, binding) {
		return ErrEnrollmentTransition
	}
	e.State = EnrollmentConnected
	return nil
}

func (e *Enrollment) Close(ownerID, instanceID int64, state EnrollmentState, now time.Time) error {
	if !e.live(now) || e.OwnerID != ownerID || e.InstanceID != instanceID || (state != EnrollmentRejected && state != EnrollmentCancelled) || (e.State != EnrollmentCreated && e.State != EnrollmentPending) || (state == EnrollmentRejected && e.State != EnrollmentPending) {
		return ErrEnrollmentTransition
	}
	e.State = state
	return nil
}
