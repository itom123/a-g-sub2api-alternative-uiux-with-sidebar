package openclawworker

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGatewayProxyPeerRequiresExactIsolatedCell(t *testing.T) {
	tenant := "u1-0123456789ab"
	valid := `{"name":"/openclaw-cell-u1-0123456789ab","networks":{"openclaw-cell-u1-0123456789ab-net":{"IPAddress":"10.89.3.2"}}}`
	peer, err := gatewayProxyPeer([]byte(valid), tenant)
	if err != nil || peer != "10.89.3.2" {
		t.Fatalf("valid private peer: %q %v", peer, err)
	}
	for _, input := range []string{
		strings.ReplaceAll(valid, "10.89.3.2", "127.0.0.1"),
		strings.ReplaceAll(valid, "10.89.3.2", "198.18.0.1"),
		strings.ReplaceAll(valid, "10.89.3.2", "::1"),
		strings.ReplaceAll(valid, tenant, "u2-abcdef012345"),
		strings.ReplaceAll(valid, "-net", "-other"),
		strings.ReplaceAll(valid, "10.89.3.2", ""),
	} {
		if _, err := gatewayProxyPeer([]byte(input), tenant); err == nil {
			t.Fatalf("accepted unverified peer: %s", input)
		}
	}
}

func TestGatewayFutureCellPolicyPreservesExistingOrigin(t *testing.T) {
	patch, err := gatewaySecureConfig([]byte(`{"allowedOrigins":["http://127.0.0.1:19100"]}`), "https://u1-0123456789ab.claw.example")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(patch["controlUi"])
	controlUI, ok := patch["controlUi"].(map[string]any)
	if !ok {
		t.Fatalf("controlUi patch has unexpected type %T", patch["controlUi"])
	}
	if _, obsolete := controlUI["allowInsecureAuth"]; obsolete {
		t.Fatal("emitted an obsolete setting rejected by the deployed runtime")
	}
	if !gatewayOriginAllowed(data, "http://127.0.0.1:19100") || !gatewayOriginAllowed(data, "https://u1-0123456789ab.claw.example") {
		t.Fatal("lost existing or native gateway origin")
	}
	if _, err := gatewaySecureConfig([]byte(`{"allowedOrigins":["*"]}`), "https://u1-0123456789ab.claw.example"); err == nil {
		t.Fatal("accepted wildcard origin inheritance")
	}
}

func TestGatewayFutureCellPolicyRejectsUnsafeLegacyInput(t *testing.T) {
	for _, setting := range []string{"allowInsecureAuth", "dangerouslyDisableDeviceAuth", "dangerouslyAllowHostHeaderOriginFallback"} {
		t.Run(setting, func(t *testing.T) {
			input, _ := json.Marshal(map[string]any{"allowedOrigins": []string{"http://127.0.0.1:19100"}, setting: true})
			if _, err := gatewaySecureConfig(input, "https://u1-0123456789ab.claw.example"); err == nil {
				t.Fatal("accepted an unsafe gateway configuration")
			}
		})
	}
}
