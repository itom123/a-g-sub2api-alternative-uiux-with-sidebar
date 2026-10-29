package openclawworker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

var gatewayIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{7,127}$`)

type gatewayCellCommand func(context.Context, string, ...string) ([]byte, error)

// The generic gateway-call CLI infers only operator.pairing for approve. Native
// owner approvals require operator.admin, as in the native devices CLI. Use its
// RPC facade directly so it cannot fall back to a different pending request.
const gatewayApproveRPC = `
import {callGatewayFromCliRuntime} from '/app/dist/gateway-rpc.runtime.js';
try {
 const result = await callGatewayFromCliRuntime('device.pair.approve',
  {expectUrl:'ws://127.0.0.1:18789',json:true,timeout:'10000'},
  JSON.parse(process.argv[1]),
  {scopes:['operator.admin'],sharedStateMode:'read-only',requireLocalBackendSharedAuth:true});
 process.stdout.write(JSON.stringify(result));
} catch { process.stderr.write('Native device approval failed'); process.exitCode=1; }
`

func gatewayDeviceList(ctx context.Context, tenant string, run gatewayCellCommand) (*openclawgateway.Inventory, error) {
	out, err := run(ctx, tenant, "openclaw", "gateway", "call", "device.pair.list", "--expect-url", "ws://127.0.0.1:18789", "--json")
	if err != nil {
		return nil, errors.New("device inventory unavailable")
	}
	var inventory openclawgateway.Inventory
	if len(out) > 256<<10 || json.Unmarshal(out, &inventory) != nil || inventory.Pending == nil || inventory.Paired == nil {
		return nil, errors.New("invalid native device inventory")
	}
	// Decode an explicit allowlist of metadata; tokens, public keys and network
	// addresses from either CLI or future runtime fields never cross this boundary.
	return &inventory, nil
}

func (w *Worker) gatewayDevices(ctx context.Context, tenant string, request openclawgateway.Request) (Result, error) {
	result, err := gatewayDeviceOperation(ctx, tenant, request, w.cellCommand)
	if err == nil && result.Devices != nil {
		w.enrollmentMu.RLock()
		result.EnrollmentBindings = make([]openclawgateway.EnrollmentBindingTransport, 0)
		for i := range result.Devices.Pending {
			if binding, ok := w.enrollmentByDevice[tenant+"/"+result.Devices.Pending[i].DeviceID]; ok {
				result.Devices.Pending[i].EnrollmentID = binding.EnrollmentID
				result.EnrollmentBindings = append(result.EnrollmentBindings, binding.Transport())
			}
		}
		for i := range result.Devices.Paired {
			if binding, ok := w.enrollmentByDevice[tenant+"/"+result.Devices.Paired[i].DeviceID]; ok {
				result.Devices.Paired[i].EnrollmentID = binding.EnrollmentID
				result.EnrollmentBindings = append(result.EnrollmentBindings, binding.Transport())
			}
		}
		w.enrollmentMu.RUnlock()
	}
	if err == nil && request.Action != "list" {
		w.enrollmentMu.RLock()
		if binding, ok := w.enrollmentByDevice[tenant+"/"+request.DeviceID]; ok {
			result.EnrollmentBindings = []openclawgateway.EnrollmentBindingTransport{binding.Transport()}
		}
		w.enrollmentMu.RUnlock()
	}
	return result, err
}

func gatewayDeviceOperation(ctx context.Context, tenant string, request openclawgateway.Request, run gatewayCellCommand) (Result, error) {
	if os.Getenv("OPENCLAW_GATEWAY_ENABLED") != "1" {
		return Result{}, errors.New("gateway access disabled")
	}
	if !tenantPattern.MatchString(tenant) {
		return Result{}, errors.New("invalid tenant identity")
	}
	if request.Action != "list" && request.Action != "approve" && request.Action != "reject" && request.Action != "remove" {
		return Result{}, errors.New("unsupported device action")
	}
	if request.Action != "list" && (!gatewayIDPattern.MatchString(request.DeviceID) || ((request.Action == "approve" || request.Action == "reject") && !gatewayIDPattern.MatchString(request.RequestID))) {
		return Result{}, errors.New("invalid device identity")
	}
	inventory, err := gatewayDeviceList(ctx, tenant, run)
	if err != nil {
		return Result{}, err
	}
	if request.Action == "list" {
		return Result{State: "ready", Devices: inventory}, nil
	}
	method := "device.pair." + request.Action
	params := map[string]string{}
	found := false
	if request.Action == "remove" {
		for _, d := range inventory.Paired {
			found = found || d.DeviceID == request.DeviceID
		}
		params["deviceId"] = request.DeviceID
	} else {
		for _, d := range inventory.Pending {
			found = found || (d.RequestID == request.RequestID && d.DeviceID == request.DeviceID)
		}
		params["requestId"] = request.RequestID
	}
	if !found {
		return Result{}, errors.New("device request changed; refresh inventory")
	}
	body, _ := json.Marshal(params)
	// Exact native RPC avoids the CLI approve fallback, which can substitute a
	// newer repair request. The owner must explicitly review every new request ID.
	var out []byte
	if request.Action == "approve" {
		out, err = run(ctx, tenant, "node", "--input-type=module", "-e", gatewayApproveRPC, string(body))
	} else {
		out, err = run(ctx, tenant, "openclaw", "gateway", "call", method, "--params", string(body), "--expect-url", "ws://127.0.0.1:18789", "--json")
	}
	if err != nil {
		return Result{}, errors.New("native device action failed")
	}
	var receipt struct {
		RequestID string `json:"requestId"`
		DeviceID  string `json:"deviceId"`
		Device    struct {
			DeviceID string `json:"deviceId"`
		} `json:"device"`
	}
	if len(out) > 256<<10 || json.Unmarshal(out, &receipt) != nil {
		return Result{}, errors.New("native device action was not confirmed")
	}
	deviceID := receipt.DeviceID
	if request.Action == "approve" {
		deviceID = receipt.Device.DeviceID
	}
	if deviceID != request.DeviceID || (request.Action != "remove" && receipt.RequestID != request.RequestID) {
		return Result{}, errors.New("native device action identity mismatch")
	}
	// Native mutation output can contain credentials. Return only the verified
	// outcome; all other fields are discarded unconditionally.
	return Result{State: "updated"}, nil
}
