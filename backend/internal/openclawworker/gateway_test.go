package openclawworker

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGatewayRequiresExplicitSecureOrigin(t *testing.T) {
	origin := "https://u1-123456789abc.gateway.example.com"
	for _, config := range []string{`{}`, `{"allowedOrigins":["*"]}`, `{"allowedOrigins":["https://other.example.com"]}`, `{"allowedOrigins":["` + origin + `"],"dangerouslyDisableDeviceAuth":true}`, `{"allowedOrigins":["` + origin + `"],"dangerouslyAllowHostHeaderOriginFallback":true}`} {
		if gatewayOriginAllowed([]byte(config), origin) {
			t.Fatal("accepted an insecure Control UI configuration")
		}
	}
	if !gatewayOriginAllowed([]byte(`{"allowedOrigins":["`+origin+`"]}`), origin) {
		t.Fatal("rejected explicit origin")
	}
}

func TestGatewayPublicOriginIsolation(t *testing.T) {
	tenant := "u1-123456789abc"
	for _, template := range []string{
		"", "http://{tenant}.example.com", "https://shared.example.com",
		"https://{tenant}.example.com/", "https://{tenant}.example.com?x=1",
		"https://{tenant}.example.com#fragment", "https://{tenant}.example.com:19201",
		"https://{tenant}.example.com@evil.example", "https://{tenant}.example.com/{tenant}",
		"https://{tenant}.*.example.com", "https://{tenant}.example.com?",
	} {
		if _, err := gatewayPublicOrigin(template, tenant); err == nil {
			t.Errorf("accepted unsafe origin configuration %q", template)
		}
	}
	if _, err := gatewayPublicOrigin("https://{tenant}.gateway.example.com", "../../management"); err == nil {
		t.Fatal("accepted invalid tenant")
	}
	a, err := gatewayPublicOrigin("https://{tenant}.gateway.example.com", tenant)
	if err != nil {
		t.Fatal(err)
	}
	b, err := gatewayPublicOrigin("https://{tenant}.gateway.example.com", "u2-abcdef123456")
	if err != nil || a.Host == b.Host {
		t.Fatal("tenant destinations are not isolated")
	}
}

func TestGatewayBrowserHandoff(t *testing.T) {
	now := time.Unix(1800000000, 0)
	origin, _ := gatewayPublicOrigin("https://{tenant}.gateway.example.com", "u1-123456789abc")
	fragment := url.Values{"bootstrapToken": {strings.Repeat("b", 40)}, "bootstrapProfile": {"owner"}, "gatewayUrl": {"ws://127.0.0.1:18789"}}
	base := "http://127.0.0.1:18789/#"
	payload := func(browser string, expires int64) []byte {
		out, _ := json.Marshal(map[string]any{"ok": true, "browserUrl": browser, "browserBootstrapExpiresAtMs": expires, "url": "http://127.0.0.1/#token=shared-secret-must-never-leave"})
		return out
	}
	expiry := now.Add(10 * time.Minute).UnixMilli()
	result, err := gatewayBrowserHandoff(payload(base+fragment.Encode(), expiry), origin, now)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(result.BrowserURL)
	if err != nil || u.Scheme != "https" || u.Host != origin.Host || strings.Contains(result.BrowserURL, "127.0.0.1") || strings.Contains(result.BrowserURL, "shared-secret") {
		t.Fatal("handoff leaked a private destination or credential")
	}
	// Match browser URLSearchParams(location.hash.slice(1)); Go's Fragment has
	// already decoded percent escapes and would hide double encoding.
	f, _ := url.ParseQuery(strings.SplitN(result.BrowserURL, "#", 2)[1])
	if f.Get("gatewayUrl") != "wss://"+origin.Host || f.Get("bootstrapToken") != fragment.Get("bootstrapToken") {
		t.Fatal("native bootstrap or WSS destination changed incorrectly")
	}
	for _, expiry := range []int64{0, now.Add(-time.Second).UnixMilli(), now.Add(time.Hour).UnixMilli()} {
		if _, err := gatewayBrowserHandoff(payload(base+fragment.Encode(), expiry), origin, now); err == nil {
			t.Error("accepted expired or unbounded bootstrap")
		}
	}
	for _, pair := range [][2]string{{"token", "shared-secret"}, {"bootstrapToken", "short"}, {"bootstrapProfile", "node"}, {"gatewayUrl", "ws://192.168.1.1:19201"}} {
		f, _ := url.ParseQuery(fragment.Encode())
		f.Set(pair[0], pair[1])
		if _, err := gatewayBrowserHandoff(payload(base+f.Encode(), expiry), origin, now); err == nil {
			t.Errorf("accepted unsafe native field %s", pair[0])
		}
	}
	for _, browser := range []string{"https://evil.example/#" + fragment.Encode(), "http://127.0.0.1:18789/?token=secret#" + fragment.Encode(), base + fragment.Encode() + "&bootstrapToken=duplicate"} {
		if _, err := gatewayBrowserHandoff(payload(browser, expiry), origin, now); err == nil {
			t.Error("accepted unsafe browser URL")
		}
	}
}
