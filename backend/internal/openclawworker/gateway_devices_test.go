package openclawworker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

func TestGatewayDeviceInventoryStripsSecrets(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "1")
	run := func(_ context.Context, tenant string, args ...string) ([]byte, error) {
		if tenant != "u1-123456789abc" || args[3] != "device.pair.list" {
			t.Fatal("unexpected destination")
		}
		return []byte(`{"pending":[],"paired":[{"deviceId":"device-12345678","displayName":"Owner browser","remoteIp":"10.1.2.3","publicKey":"private-topology","tokens":{"operator":{"token":"sensitive-credential"}},"lastSeenAtMs":123}]}`), nil
	}
	r, err := gatewayDeviceOperation(context.Background(), "u1-123456789abc", openclawgateway.Request{Action: "list"}, run)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "sensitive") || strings.Contains(string(b), "10.1.2.3") || strings.Contains(string(b), "publicKey") {
		t.Fatal("device inventory leaked a private field")
	}
	if len(r.Devices.Paired) != 1 || r.Devices.Paired[0].LastSeenAt != 123 {
		t.Fatal("safe device metadata lost")
	}
}

func TestGatewayDeviceApprovalBindsExactRequest(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "1")
	mutations := 0
	run := func(_ context.Context, tenant string, args ...string) ([]byte, error) {
		if tenant != "u1-123456789abc" {
			t.Fatal("wrong tenant")
		}
		if args[3] == "device.pair.list" {
			return []byte(`{"pending":[{"requestId":"request-current","deviceId":"device-12345678","role":"operator","scopes":["operator.read"]}],"paired":[]}`), nil
		}
		mutations++
		if args[0] != "node" || args[3] != gatewayApproveRPC || args[4] != `{"requestId":"request-current"}` || !strings.Contains(args[3], "scopes:['operator.admin']") || !strings.Contains(args[3], "requireLocalBackendSharedAuth:true") {
			t.Fatal("approval did not target the reviewed native request")
		}
		return []byte(`{"requestId":"request-current","device":{"deviceId":"device-12345678"},"token":"must-never-leave"}`), nil
	}
	for _, request := range []openclawgateway.Request{
		{Action: "approve", RequestID: "request-superseded", DeviceID: "device-12345678"},
		{Action: "approve", RequestID: "request-current", DeviceID: "other-device-123"},
		{Action: "approve", RequestID: "--latest", DeviceID: "device-12345678"},
		{Action: "delete", RequestID: "request-current", DeviceID: "device-12345678"},
	} {
		if _, err := gatewayDeviceOperation(context.Background(), "u1-123456789abc", request, run); err == nil {
			t.Fatal("unsafe request accepted")
		}
	}
	if mutations != 0 {
		t.Fatal("unreviewed native request was mutated")
	}
	r, err := gatewayDeviceOperation(context.Background(), "u1-123456789abc", openclawgateway.Request{Action: "approve", RequestID: "request-current", DeviceID: "device-12345678"}, run)
	if err != nil || mutations != 1 {
		t.Fatal("reviewed request was not approved")
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "must-never-leave") {
		t.Fatal("native mutation output leaked")
	}
}
