package openclawworker

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
)

func (w *Worker) gatewayConfigPatch(ctx context.Context, tenant string) (map[string]any, error) {
	origin, err := gatewayPublicOrigin(os.Getenv("OPENCLAW_GATEWAY_PUBLIC_ORIGIN_TEMPLATE"), tenant)
	if err != nil {
		return nil, err
	}
	data, err := w.cellCommand(ctx, tenant, "openclaw", "config", "get", "gateway.controlUi", "--json")
	if err != nil {
		return nil, errors.New("gateway policy unavailable")
	}
	patch, err := gatewaySecureConfig(data, origin.String())
	if err != nil {
		return nil, err
	}
	// Rootless Podman hairpins host port traffic through this cell's own IP.
	// Trust that exact isolated endpoint, never its subnet or a forwarded claim.
	network, err := w.hostCommand(ctx, "podman", "inspect", "--format", `{"name":{{json .Name}},"networks":{{json .NetworkSettings.Networks}}}`, "openclaw-cell-"+tenant)
	if err != nil {
		return nil, errors.New("gateway proxy identity unavailable")
	}
	peer, err := gatewayProxyPeer(network, tenant)
	if err != nil {
		return nil, err
	}
	patch["trustedProxies"] = []string{"127.0.0.1", peer}
	return patch, nil
}

func gatewayProxyPeer(data []byte, tenant string) (string, error) {
	invalid := errors.New("gateway proxy identity unavailable")
	var cell struct {
		Name     string `json:"name"`
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"networks"`
	}
	if !tenantPattern.MatchString(tenant) || len(data) > 16<<10 || json.Unmarshal(data, &cell) != nil || strings.TrimPrefix(cell.Name, "/") != "openclaw-cell-"+tenant || len(cell.Networks) != 1 {
		return "", invalid
	}
	endpoint, ok := cell.Networks["openclaw-cell-"+tenant+"-net"]
	ip := net.ParseIP(endpoint.IPAddress)
	if !ok || ip == nil || ip.To4() == nil || !ip.IsPrivate() {
		return "", invalid
	}
	return ip.String(), nil
}

func gatewaySecureConfig(data []byte, origin string) (map[string]any, error) {
	var ui struct {
		AllowedOrigins    []string `json:"allowedOrigins"`
		HostFallback      bool     `json:"dangerouslyAllowHostHeaderOriginFallback"`
		DisableDeviceAuth bool     `json:"dangerouslyDisableDeviceAuth"`
		AllowInsecureAuth bool     `json:"allowInsecureAuth"`
	}
	if json.Unmarshal(data, &ui) != nil || ui.HostFallback || ui.DisableDeviceAuth || ui.AllowInsecureAuth {
		return nil, errors.New("unsafe gateway policy")
	}
	found := false
	for _, allowed := range ui.AllowedOrigins {
		if strings.Contains(allowed, "*") {
			return nil, errors.New("unsafe gateway origin")
		}
		found = found || allowed == origin
	}
	if !found {
		ui.AllowedOrigins = append(ui.AllowedOrigins, origin)
	}
	return map[string]any{
		"trustedProxies": []string{"127.0.0.1"},
		// The deployed runtime removed allowInsecureAuth from its schema. Reject
		// unsafe legacy input above, but do not emit even a false obsolete key.
		"controlUi": map[string]any{"allowedOrigins": ui.AllowedOrigins, "dangerouslyAllowHostHeaderOriginFallback": false, "dangerouslyDisableDeviceAuth": false},
	}, nil
}
