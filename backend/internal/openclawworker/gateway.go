package openclawworker

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

func gatewayOriginAllowed(data []byte, origin string) bool {
	var config struct {
		AllowedOrigins    []string `json:"allowedOrigins"`
		HostFallback      bool     `json:"dangerouslyAllowHostHeaderOriginFallback"`
		DisableDeviceAuth bool     `json:"dangerouslyDisableDeviceAuth"`
		AllowInsecureAuth bool     `json:"allowInsecureAuth"`
	}
	if json.Unmarshal(data, &config) != nil || config.HostFallback || config.DisableDeviceAuth || config.AllowInsecureAuth {
		return false
	}
	found := false
	for _, allowed := range config.AllowedOrigins {
		if strings.Contains(allowed, "*") {
			return false
		}
		found = found || allowed == origin
	}
	return found
}

// Require a unique host for each cell; no paths, credentials, ports or wildcards.
func gatewayPublicOrigin(template, tenant string) (*url.URL, error) {
	return openclawgateway.PublicOrigin(template, tenant)
}

// Decode only the native owner bootstrap. Never return dashboard's legacy `url`,
// which may contain the cell's shared operator token. Errors omit all CLI output.
func gatewayBrowserHandoff(data []byte, origin *url.URL, now time.Time) (Result, error) {
	invalid := errors.New("native browser handoff unavailable")
	var p struct {
		OK         bool   `json:"ok"`
		BrowserURL string `json:"browserUrl"`
		ExpiresAt  int64  `json:"browserBootstrapExpiresAtMs"`
	}
	if len(data) > 64<<10 || json.Unmarshal(data, &p) != nil || !p.OK {
		return Result{}, invalid
	}
	expires := time.UnixMilli(p.ExpiresAt)
	if !expires.After(now.Add(5*time.Second)) || expires.After(now.Add(10*time.Minute+15*time.Second)) {
		return Result{}, invalid
	}
	u, err := url.Parse(p.BrowserURL)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() != "18789" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Path != "/" || u.RawPath != "" {
		return Result{}, invalid
	}
	f, err := url.ParseQuery(u.Fragment)
	if err != nil || len(f) != 3 || len(f["bootstrapToken"]) != 1 || len(f["bootstrapProfile"]) != 1 || len(f["gatewayUrl"]) != 1 || len(f.Get("bootstrapToken")) < 32 || f.Get("bootstrapProfile") != "owner" {
		return Result{}, invalid
	}
	ws, err := url.Parse(f.Get("gatewayUrl"))
	if err != nil || ws.Scheme != "ws" || ws.Host != u.Host || ws.User != nil || ws.RawQuery != "" || ws.Fragment != "" || (ws.Path != "" && ws.Path != "/") {
		return Result{}, invalid
	}
	ws.Scheme, ws.Host = "wss", origin.Host
	f.Set("gatewayUrl", ws.String())
	// URL.Fragment is decoded text; assigning query-encoded values there escapes
	// percent signs a second time. Browsers parse location.hash only once.
	u.Scheme, u.Host, u.Fragment, u.RawFragment = "https", origin.Host, "", ""
	return Result{State: "ready", BrowserURL: u.String() + "#" + f.Encode()}, nil
}
