package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

func TestOpenClawEnrollmentPrivateClientChecksProtocolAndExactReceipt(t *testing.T) {
	const tenant = "u42-123456789abc"
	const id = "enrollment-fixture"
	result := openclawgateway.EnrollmentBridgeResult{Protocol: "ag-enrollment-v1", Enrollment: &openclawgateway.EnrollmentReceipt{ID: id, Client: "macos", State: openclawgateway.EnrollmentCreated, CreatedAtMS: 1800000000000, ExpiresAtMS: 1800000300000}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/cells/"+tenant+"/enrollment" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) {
			t.Error("private bridge routing/authentication changed")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var command openclawgateway.EnrollmentCommand
		if json.NewDecoder(r.Body).Decode(&command) != nil || command.Action != "status" || command.ID != id {
			t.Error("invalid private command")
		}
		_ = json.NewEncoder(w).Encode(OpenClawFleetResult{State: "ready", EnrollmentResult: &result})
	}))
	defer server.Close()
	client := NewHTTPOpenClawFleetClient(server.URL, strings.Repeat("x", 32))
	command := openclawgateway.EnrollmentCommand{Action: "status", ID: id}
	got, err := client.GatewayEnrollment(context.Background(), tenant, command)
	if err != nil || got.SessionDelivery {
		t.Fatal("pending-only capability misrepresented", err)
	}
	result.Protocol = "unknown"
	if _, err := client.GatewayEnrollment(context.Background(), tenant, command); err == nil {
		t.Fatal("unknown protocol accepted")
	}
	result.Protocol = "ag-enrollment-v1"
	result.Enrollment.ID = "replacement-enrollment"
	if _, err := client.GatewayEnrollment(context.Background(), tenant, command); err == nil {
		t.Fatal("replacement receipt accepted")
	}
}

func TestOpenClawEnrollmentRolloutRequiresSeparateExplicitAllowlist(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "1")
	t.Setenv("OPENCLAW_GATEWAY_ALL_ELIGIBLE", "1")
	t.Setenv("OPENCLAW_DEVICE_ENROLLMENT_ENABLED", "")
	t.Setenv("OPENCLAW_DEVICE_ENROLLMENT_USER_IDS", "42")
	if openClawEnrollmentEnabled(42) {
		t.Fatal("browser rollout implicitly enabled native enrollment")
	}
	t.Setenv("OPENCLAW_DEVICE_ENROLLMENT_ENABLED", "1")
	if !openClawEnrollmentEnabled(42) || openClawEnrollmentEnabled(4) || openClawEnrollmentEnabled(0) {
		t.Fatal("native rollout ignored the exact owner allowlist")
	}
	t.Setenv("OPENCLAW_DEVICE_ENROLLMENT_USER_IDS", "")
	if openClawEnrollmentEnabled(42) {
		t.Fatal("empty native allowlist enabled every browser owner")
	}
	t.Setenv("OPENCLAW_DEVICE_ENROLLMENT_USER_IDS", "42")
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "")
	if openClawEnrollmentEnabled(42) {
		t.Fatal("native enrollment outlived gateway withdrawal")
	}
}
