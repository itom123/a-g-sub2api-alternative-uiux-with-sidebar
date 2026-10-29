package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

var ErrOpenClawEnrollmentDisabled = errors.New("native device enrollment is not enabled")

type openClawEnrollmentClient interface {
	GatewayEnrollment(context.Context, string, openclawgateway.EnrollmentCommand) (*openclawgateway.EnrollmentBridgeResult, error)
}

func (c *HTTPOpenClawFleetClient) GatewayEnrollment(ctx context.Context, tenant string, request openclawgateway.EnrollmentCommand) (*openclawgateway.EnrollmentBridgeResult, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	result, err := c.callBody(ctx, "enrollment", tenant, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	bridge := result.EnrollmentResult
	if bridge == nil || bridge.Protocol != "ag-enrollment-v1" {
		return nil, ErrOpenClawProviderUnavailable
	}
	if request.Action == "retire" {
		if bridge.RetiredID != request.ID || bridge.Enrollment != nil || bridge.Enrollments != nil || bridge.CleanupRequestID != "" {
			return nil, ErrOpenClawProviderUnavailable
		}
		return bridge, nil
	}
	if bridge.RetiredID != "" {
		return nil, ErrOpenClawProviderUnavailable
	}
	if request.Action == "list" {
		if bridge.Enrollments == nil || bridge.Enrollment != nil {
			return nil, ErrOpenClawProviderUnavailable
		}
		for _, r := range bridge.Enrollments {
			if r.Validate() != nil {
				return nil, ErrOpenClawProviderUnavailable
			}
		}
	} else if bridge.Enrollment == nil || bridge.Enrollment.Validate() != nil || bridge.Enrollment.ID != request.ID {
		return nil, ErrOpenClawProviderUnavailable
	}
	return bridge, nil
}

// This is deliberately independent of browser gateway access. A signing secret
// provisions the private bridge; it does not authorize a customer rollout.
func openClawEnrollmentEnabled(userID int64) bool {
	if !openClawGatewayEnabled(userID) || os.Getenv("OPENCLAW_DEVICE_ENROLLMENT_ENABLED") != "1" {
		return false
	}
	for _, allowed := range strings.Split(os.Getenv("OPENCLAW_DEVICE_ENROLLMENT_USER_IDS"), ",") {
		if strings.TrimSpace(allowed) == strconv.FormatInt(userID, 10) {
			return true
		}
	}
	return false
}

type OpenClawEnrollmentResponse struct {
	ID         string                          `json:"id"`
	Client     string                          `json:"client"`
	State      openclawgateway.EnrollmentState `json:"state"`
	SetupCode  string                          `json:"setup_code"`
	GatewayURL string                          `json:"gateway_url"`
	CreatedAt  time.Time                       `json:"created_at"`
	ExpiresAt  time.Time                       `json:"expires_at"`
}

func openClawEnrollmentSecret() []byte {
	if path := strings.TrimSpace(os.Getenv("OPENCLAW_DEVICE_ENROLLMENT_SECRET_FILE")); path != "" {
		if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) >= 32 { // #nosec G304 G703 -- Operator-configured secret file path, never a request parameter.
			return []byte(strings.TrimSpace(string(data)))
		}
	}
	secret := strings.TrimSpace(os.Getenv("OPENCLAW_DEVICE_ENROLLMENT_SECRET"))
	if len(secret) < 32 {
		return nil
	}
	return []byte(secret)
}

func (s *OpenClawService) CreateGatewayEnrollment(ctx context.Context, userID, instanceID int64, client string) (*OpenClawEnrollmentResponse, error) {
	secret := openClawEnrollmentSecret()
	if !openClawEnrollmentEnabled(userID) || len(secret) < 32 || s.db == nil || s.fleet == nil || !s.fleet.Enabled() {
		return nil, ErrOpenClawEnrollmentDisabled
	}
	instance, err := s.GetInstance(ctx, userID, instanceID)
	if err != nil {
		return nil, err
	}
	if instance.DeletionScheduledAt != nil {
		return nil, ErrOpenClawDeletionScheduled
	}
	if instance.Status != "ready" && instance.Status != "running" {
		return nil, ErrOpenClawProviderUnavailable
	}
	owner, err := s.gatewayTenantOwner(ctx, instance.TenantSlug)
	if err != nil || owner != userID {
		return nil, ErrOpenClawProviderUnavailable
	}
	if client != "ios" && client != "macos" {
		return nil, ErrOpenClawProviderUnavailable
	}
	bridge, ok := s.fleet.(openClawEnrollmentClient)
	if !ok {
		return nil, ErrOpenClawEnrollmentDisabled
	}
	if os.Getenv("OPENCLAW_DEVICE_ENROLLMENT_RETENTION_ENABLED") == "1" {
		if err := s.retireGatewayEnrollmentHistory(ctx, userID, instanceID, instance.TenantSlug, bridge); err != nil {
			return nil, ErrOpenClawProviderUnavailable
		}
	}
	now := time.Now().UTC()
	store := openclawgateway.EnrollmentStore{DB: s.db, TicketSecret: secret}
	enrollment, ticket, err := store.Create(ctx, userID, instanceID, instance.TenantSlug, client, now)
	if err != nil {
		return nil, err
	}
	registration, err := openclawgateway.EnrollmentRegistration(enrollment, ticket)
	if err != nil {
		return nil, err
	}
	registered, err := bridge.GatewayEnrollment(ctx, instance.TenantSlug, registration)
	if err != nil || registered == nil || !registered.SessionDelivery || registered.Enrollment == nil || !registered.Enrollment.Matches(enrollment) || registered.Enrollment.State != openclawgateway.EnrollmentCreated {
		// No QR has been disclosed. A lost remote response can leave only an
		// unguessable, short-lived registration; cancel the controller record.
		_, _ = store.Mutate(ctx, userID, instanceID, enrollment.ID, openclawgateway.EnrollmentMutation{Action: "cancel"}, time.Now().UTC())
		return nil, ErrOpenClawProviderUnavailable
	}
	template := strings.TrimSpace(os.Getenv("OPENCLAW_GATEWAY_PUBLIC_ORIGIN_TEMPLATE"))
	code, err := openclawgateway.EnrollmentSetupCode(enrollment, template, ticket, now)
	if err != nil {
		return nil, err
	}
	origin, err := openclawgateway.PublicOrigin(template, enrollment.Tenant)
	if err != nil {
		return nil, ErrOpenClawProviderUnavailable
	}
	return &OpenClawEnrollmentResponse{ID: enrollment.ID, Client: client, State: enrollment.State, SetupCode: code, GatewayURL: "wss://" + origin.Host, CreatedAt: enrollment.CreatedAt, ExpiresAt: enrollment.ExpiresAt}, nil
}

func (s *OpenClawService) GatewayEnrollmentStatus(ctx context.Context, userID, instanceID int64, id string) (*openclawgateway.Enrollment, error) {
	if s.db == nil || !openClawGatewayEnabled(userID) {
		return nil, ErrOpenClawEnrollmentDisabled
	}
	if _, err := s.GetInstance(ctx, userID, instanceID); err != nil {
		return nil, err
	}
	store := openclawgateway.EnrollmentStore{DB: s.db}
	deliveryUncertain := false
	result, err := store.Mutate(ctx, userID, instanceID, id, openclawgateway.EnrollmentMutation{Action: "inspect"}, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if result.State == openclawgateway.EnrollmentCreated || result.State == openclawgateway.EnrollmentPending || result.State == openclawgateway.EnrollmentApproved {
		bridge, ok := s.fleet.(openClawEnrollmentClient)
		if !ok || s.fleet == nil || !s.fleet.Enabled() {
			return nil, ErrOpenClawProviderUnavailable
		}
		native, err := bridge.GatewayEnrollment(ctx, result.Tenant, openclawgateway.EnrollmentCommand{Action: "status", ID: id})
		if err != nil || native == nil || native.Enrollment == nil || !native.Enrollment.Matches(result) {
			return nil, ErrOpenClawProviderUnavailable
		}
		deliveryUncertain = native.Enrollment.DeliveryUncertain
		// Only native pending binding is imported here. Approval is an owner
		// decision; inventory or a status poll must never manufacture it.
		if native.Enrollment.State == openclawgateway.EnrollmentPending && native.Enrollment.Binding != nil && (result.State == openclawgateway.EnrollmentCreated || result.State == openclawgateway.EnrollmentPending) {
			result, err = store.Mutate(ctx, userID, instanceID, id, openclawgateway.EnrollmentMutation{Action: "bind", Binding: native.Enrollment.Binding.Binding()}, time.Now().UTC())
			if err != nil {
				return nil, err
			}
		}
		if native.Enrollment.State == openclawgateway.EnrollmentConnected {
			if result.State != openclawgateway.EnrollmentApproved || native.Enrollment.Binding == nil {
				return nil, openclawgateway.ErrEnrollmentTransition
			}
			result, err = store.Mutate(ctx, userID, instanceID, id, openclawgateway.EnrollmentMutation{Action: "connected", Binding: native.Enrollment.Binding.Binding(), DeliveredAt: time.UnixMilli(native.Enrollment.DeliveredAtMS).UTC()}, time.Now().UTC())
			if err != nil {
				return nil, err
			}
		}
	}
	if result.State == openclawgateway.EnrollmentCreated || result.State == openclawgateway.EnrollmentPending || result.State == openclawgateway.EnrollmentApproved {
		result, err = store.Mutate(ctx, userID, instanceID, id, openclawgateway.EnrollmentMutation{Action: "status"}, time.Now().UTC())
		if err != nil {
			return nil, err
		}
	}
	result.DeliveryUncertain = deliveryUncertain && result.State == openclawgateway.EnrollmentApproved
	return &result, nil
}

func (s *OpenClawService) CancelGatewayEnrollment(ctx context.Context, userID, instanceID int64, id string) error {
	if s.db == nil || !openClawGatewayEnabled(userID) {
		return ErrOpenClawEnrollmentDisabled
	}
	current, err := s.GatewayEnrollmentStatus(ctx, userID, instanceID, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	bridge, ok := s.fleet.(openClawEnrollmentClient)
	if !ok || s.fleet == nil || !s.fleet.Enabled() {
		return ErrOpenClawProviderUnavailable
	}
	native, err := bridge.GatewayEnrollment(ctx, current.Tenant, openclawgateway.EnrollmentCommand{Action: "cancel", ID: current.ID})
	if err != nil || native == nil || native.Enrollment == nil || !native.Enrollment.Matches(*current) || native.Enrollment.State != openclawgateway.EnrollmentCancelled {
		return ErrOpenClawProviderUnavailable
	}
	if current.State == openclawgateway.EnrollmentCancelled {
		return nil
	}
	_, err = (openclawgateway.EnrollmentStore{DB: s.db}).Mutate(ctx, userID, instanceID, current.ID, openclawgateway.EnrollmentMutation{Action: "cancel"}, time.Now().UTC())
	return err
}
