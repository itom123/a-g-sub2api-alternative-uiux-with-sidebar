package service

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

type openClawGatewayDeviceClient interface {
	GatewayDevices(context.Context, string, openclawgateway.Request) (OpenClawFleetResult, error)
}

func (c *HTTPOpenClawFleetClient) GatewayDevices(ctx context.Context, tenant string, request openclawgateway.Request) (OpenClawFleetResult, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return OpenClawFleetResult{}, err
	}
	return c.callBody(ctx, "devices", tenant, bytes.NewReader(body))
}

func (s *OpenClawService) GatewayDevices(ctx context.Context, userID, instanceID int64, request openclawgateway.Request) (*openclawgateway.Inventory, error) {
	if !openClawGatewayEnabled(userID) {
		return nil, ErrOpenClawProviderUnavailable
	}
	instance, err := s.GetInstance(ctx, userID, instanceID)
	if err != nil {
		return nil, err
	}
	if instance.DeletionScheduledAt != nil {
		return nil, ErrOpenClawDeletionScheduled
	}
	if s.fleet == nil || !s.fleet.Enabled() || (instance.Status != "ready" && instance.Status != "running") {
		return nil, ErrOpenClawProviderUnavailable
	}
	owner, err := s.gatewayTenantOwner(ctx, instance.TenantSlug)
	if err != nil || owner != userID {
		return nil, ErrOpenClawProviderUnavailable
	}
	client, ok := s.fleet.(openClawGatewayDeviceClient)
	if !ok {
		return nil, ErrOpenClawProviderUnavailable
	}
	if request.Action == "approve" || request.Action == "reject" {
		handled, err := s.gatewayEnrollmentDecision(ctx, userID, instanceID, request)
		if err != nil {
			return nil, err
		}
		if handled {
			request = openclawgateway.Request{Action: "list"}
		}
	}
	result, err := client.GatewayDevices(ctx, instance.TenantSlug, request)
	if err != nil {
		return nil, ErrOpenClawProviderUnavailable
	}
	if request.Action == "list" && len(result.EnrollmentBindings) > 0 {
		store := openclawgateway.EnrollmentStore{DB: s.db}
		for _, binding := range result.EnrollmentBindings {
			bindingValue := binding.Binding()
			if bindingValue.EnrollmentID == "" {
				continue
			}
			_, _ = store.Mutate(ctx, userID, instanceID, bindingValue.EnrollmentID, openclawgateway.EnrollmentMutation{Action: "bind", Binding: bindingValue}, time.Now().UTC())
		}
	}
	return result.Devices, nil
}

// A missing in-memory worker annotation never permits enrollment approval via
// the legacy RPC. Durable records determine whether the private authority owns
// this request, including terminal/replaced requests after process restarts.
func (s *OpenClawService) gatewayEnrollmentDecision(ctx context.Context, userID, instanceID int64, request openclawgateway.Request) (bool, error) {
	store := openclawgateway.EnrollmentStore{DB: s.db}
	ids, err := store.ApprovalCandidates(ctx, userID, instanceID, request.DeviceID, request.RequestID)
	if err != nil {
		return false, ErrOpenClawProviderUnavailable
	}
	for _, id := range ids {
		e, err := s.GatewayEnrollmentStatus(ctx, userID, instanceID, id)
		if err != nil {
			return false, ErrOpenClawProviderUnavailable
		}
		if e.Binding == nil {
			// A scan may have reached native pairing just before expiry or a
			// controller restart. Reconcile even terminal unbound records before
			// deciding that a request belongs to the legacy approval path.
			bridge, ok := s.fleet.(openClawEnrollmentClient)
			if !ok {
				return false, ErrOpenClawProviderUnavailable
			}
			native, err := bridge.GatewayEnrollment(ctx, e.Tenant, openclawgateway.EnrollmentCommand{Action: "status", ID: id})
			if err != nil || native == nil || native.Enrollment == nil || !native.Enrollment.Matches(*e) {
				return false, ErrOpenClawProviderUnavailable
			}
			if native.Enrollment.Binding != nil {
				b := native.Enrollment.Binding.Binding()
				e.Binding = &b
			}
		}
		if e.Binding == nil || (e.Binding.DeviceID != request.DeviceID && e.Binding.RequestID != request.RequestID) {
			continue
		}
		if e.Binding.DeviceID != request.DeviceID || e.Binding.RequestID != request.RequestID {
			return true, openclawgateway.ErrEnrollmentTransition
		}
		bridge, ok := s.fleet.(openClawEnrollmentClient)
		if !ok {
			return true, ErrOpenClawProviderUnavailable
		}
		if request.Action == "approve" {
			if !openClawEnrollmentEnabled(userID) {
				return true, ErrOpenClawEnrollmentDisabled
			}
			if e.State == openclawgateway.EnrollmentPending {
				approved, err := store.Mutate(ctx, userID, instanceID, id, openclawgateway.EnrollmentMutation{Action: "approve", Binding: *e.Binding}, time.Now().UTC())
				if err != nil {
					return true, err
				}
				e = &approved
			}
			if e.State != openclawgateway.EnrollmentApproved {
				return true, openclawgateway.ErrEnrollmentTransition
			}
		} else if e.State != openclawgateway.EnrollmentPending && e.State != openclawgateway.EnrollmentRejected {
			return true, openclawgateway.ErrEnrollmentTransition
		}
		binding := e.Binding.Transport()
		binding.EnrollmentID = id
		native, err := bridge.GatewayEnrollment(ctx, e.Tenant, openclawgateway.EnrollmentCommand{Action: request.Action, ID: id, Binding: &binding})
		expected := openclawgateway.EnrollmentApproved
		if request.Action == "reject" {
			expected = openclawgateway.EnrollmentRejected
		}
		if err != nil || native == nil || native.Enrollment == nil || !native.Enrollment.Matches(*e) || native.Enrollment.State != expected || native.Enrollment.Binding == nil {
			return true, ErrOpenClawProviderUnavailable
		}
		// Validate the native receipt against the exact controller-reviewed grant.
		probe := *e
		probe.State = openclawgateway.EnrollmentPending
		if probe.Approve(userID, instanceID, native.Enrollment.Binding.Binding(), time.Now().UTC()) != nil {
			return true, openclawgateway.ErrEnrollmentTransition
		}
		if request.Action == "reject" && e.State != openclawgateway.EnrollmentRejected {
			_, err = store.Mutate(ctx, userID, instanceID, id, openclawgateway.EnrollmentMutation{Action: "reject"}, time.Now().UTC())
			if err != nil {
				return true, err
			}
		}
		return true, nil
	}
	return false, nil
}
