package openclawworker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

func enrollmentBridgeFixture() (openclawgateway.EnrollmentCommand, openclawgateway.EnrollmentBridgeResult) {
	c := openclawgateway.EnrollmentCommand{Action: "register", ID: "enrollment-fixture", Client: "macos", TicketDigest: strings.Repeat("a", 64), CreatedAtMS: 1800000000000, ExpiresAtMS: 1800000300000}
	r := openclawgateway.EnrollmentBridgeResult{Protocol: "ag-enrollment-v1", Enrollment: &openclawgateway.EnrollmentReceipt{ID: c.ID, Client: c.Client, State: openclawgateway.EnrollmentCreated, CreatedAtMS: c.CreatedAtMS, ExpiresAtMS: c.ExpiresAtMS}}
	return c, r
}

func TestEnrollmentRetirementRequiresSeparateMaintenanceGateAndExactAcknowledgement(t *testing.T) {
	c, r := enrollmentBridgeFixture()
	r.Enrollment.State = openclawgateway.EnrollmentExpired
	r.Enrollment.RetirementReady = true
	c = openclawgateway.EnrollmentCommand{Action: "retire", ID: c.ID, ArchivedReceipt: r.Enrollment}
	calls := 0
	run := func(context.Context, string, []byte, ...string) ([]byte, error) {
		calls++
		return json.Marshal(openclawgateway.EnrollmentBridgeResult{Protocol: "ag-enrollment-v1", RetiredID: c.ID})
	}
	t.Setenv("OPENCLAW_DEVICE_ENROLLMENT_RETENTION_ENABLED", "")
	if _, err := gatewayEnrollmentOperation(context.Background(), "u42-123456789abc", c, run); err == nil || calls != 0 {
		t.Fatal("disabled retirement ran")
	}
	t.Setenv("OPENCLAW_DEVICE_ENROLLMENT_RETENTION_ENABLED", "1")
	if _, err := gatewayEnrollmentOperation(context.Background(), "u42-123456789abc", c, run); err != nil || calls != 1 {
		t.Fatal("private retirement rejected", err)
	}
	for _, bad := range []openclawgateway.EnrollmentBridgeResult{
		{Protocol: "ag-enrollment-v1", RetiredID: "another-request"},
		{Protocol: "ag-enrollment-v1", RetiredID: c.ID, Enrollment: r.Enrollment},
		{Protocol: "ag-enrollment-v1", RetiredID: c.ID, CleanupRequestID: "another-request"},
	} {
		if _, err := gatewayEnrollmentOperation(context.Background(), "u42-123456789abc", c, func(context.Context, string, []byte, ...string) ([]byte, error) { return json.Marshal(bad) }); err == nil {
			t.Fatal("invalid retirement acknowledgement accepted")
		}
	}
}

func TestEnrollmentBridgeUsesBoundedPrivateStdinAndAllowlistedReceipt(t *testing.T) {
	t.Setenv("OPENCLAW_DEVICE_ENROLLMENT_ENABLED", "1")
	c, receipt := enrollmentBridgeFixture()
	run := func(ctx context.Context, tenant string, input []byte, args ...string) ([]byte, error) {
		if tenant != "u42-123456789abc" || strings.Join(args, " ") != "node /app/dist/ag-enrollment/authority-cli.mjs u42-123456789abc" {
			t.Fatal("unexpected execution boundary")
		}
		var got openclawgateway.EnrollmentCommand
		if json.Unmarshal(input, &got) != nil || got.TicketDigest != c.TicketDigest {
			t.Fatal("digest missing from private stdin")
		}
		out, _ := json.Marshal(receipt)
		// Native output may contain future sensitive fields; the bridge drops them.
		return append(out[:len(out)-1], []byte(`,"deviceToken":"must-never-leak"}`)...), nil
	}
	result, err := gatewayEnrollmentOperation(context.Background(), "u42-123456789abc", c, run)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(result)
	if strings.Contains(string(out), "must-never-leak") || strings.Contains(string(out), c.TicketDigest) {
		t.Fatal("secret crossed worker result boundary")
	}
}

func TestEnrollmentBridgeWithdrawnGatePreservesStatusAndCancellation(t *testing.T) {
	t.Setenv("OPENCLAW_DEVICE_ENROLLMENT_ENABLED", "")
	c, r := enrollmentBridgeFixture()
	calls := 0
	run := func(context.Context, string, []byte, ...string) ([]byte, error) { calls++; return json.Marshal(r) }
	if _, err := gatewayEnrollmentOperation(context.Background(), "u42-123456789abc", c, run); err == nil || calls != 0 {
		t.Fatal("disabled registration ran")
	}
	c.Action = "status"
	if _, err := gatewayEnrollmentOperation(context.Background(), "u42-123456789abc", c, run); err != nil || calls != 1 {
		t.Fatal("status unavailable after withdrawal", err)
	}
	c.Action = "cancel"
	r.Enrollment.State = openclawgateway.EnrollmentCancelled
	if _, err := gatewayEnrollmentOperation(context.Background(), "u42-123456789abc", c, run); err != nil || calls != 2 {
		t.Fatal("cancel unavailable after withdrawal", err)
	}
}

func TestEnrollmentBridgeRejectsChangedIdentityMalformedAndFailedReceipts(t *testing.T) {
	t.Setenv("OPENCLAW_DEVICE_ENROLLMENT_ENABLED", "1")
	c, r := enrollmentBridgeFixture()
	r.Enrollment.ID = "another-enrollment"
	changed, _ := json.Marshal(r)
	for _, out := range [][]byte{changed, []byte(`{}`), []byte(`{"enrollment":{}}`), []byte(`{} {}`), []byte(strings.Repeat("x", (256<<10)+1))} {
		run := func(context.Context, string, []byte, ...string) ([]byte, error) { return out, nil }
		if _, err := gatewayEnrollmentOperation(context.Background(), "u42-123456789abc", c, run); err == nil {
			t.Fatal("invalid native receipt accepted")
		}
	}
	if _, err := gatewayEnrollmentOperation(context.Background(), "u42-123456789abc", c, func(context.Context, string, []byte, ...string) ([]byte, error) {
		return nil, errors.New("native failure with private data")
	}); err == nil || strings.Contains(err.Error(), "private data") {
		t.Fatal("native error not sanitized")
	}
}

func TestEnrollmentEndpointRequiresPrivateAuthenticationAndStrictInput(t *testing.T) {
	w := &Worker{Token: strings.Repeat("x", 32)}
	for _, tc := range []struct {
		body, auth string
		code       int
	}{
		{`{"action":"status","id":"enrollment-fixture"}`, "", http.StatusUnauthorized},
		{`{"action":"status","id":"enrollment-fixture","shell":"no"}`, "Bearer " + w.Token, http.StatusBadRequest},
		{`{"action":"status","id":"enrollment-fixture"} {}`, "Bearer " + w.Token, http.StatusBadRequest},
		{strings.Repeat("x", 17<<10), "Bearer " + w.Token, http.StatusBadRequest},
	} {
		r := httptest.NewRequest(http.MethodPost, "/v1/cells/u42-123456789abc/enrollment", strings.NewReader(tc.body))
		r.Header.Set("Authorization", tc.auth)
		rw := httptest.NewRecorder()
		w.ServeHTTP(rw, r)
		if rw.Code != tc.code {
			t.Fatalf("got %d want %d", rw.Code, tc.code)
		}
	}
}
