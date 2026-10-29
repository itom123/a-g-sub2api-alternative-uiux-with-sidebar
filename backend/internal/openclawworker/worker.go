// Package openclawworker provides the private UK02 lifecycle bridge. Only the
// control plane can call it; it never proxies customer traffic to a shell.
package openclawworker

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

var tenantPattern = regexp.MustCompile(`^u[1-9][0-9]{0,18}-[a-f0-9]{12}$`)

type Result struct {
	State              string                                       `json:"state"`
	Port               int                                          `json:"port,omitempty"`
	LatencyMS          *int                                         `json:"latency_ms,omitempty"`
	SetupURL           string                                       `json:"setup_url,omitempty"`
	SetupKey           string                                       `json:"setup_key,omitempty"`
	Message            string                                       `json:"message,omitempty"`
	BrowserURL         string                                       `json:"browser_url,omitempty"`
	Devices            *openclawgateway.Inventory                   `json:"devices,omitempty"`
	EnrollmentBindings []openclawgateway.EnrollmentBindingTransport `json:"enrollment_bindings,omitempty"`
	EnrollmentResult   *openclawgateway.EnrollmentBridgeResult      `json:"enrollment_result,omitempty"`
}

type modelAlias struct {
	Alias string `json:"alias"`
	Model string `json:"model"`
}

type modelMapping struct {
	GatewayURL   string       `json:"gateway_url"`
	APIKey       string       `json:"api_key"`
	DefaultModel string       `json:"default_model"`
	Models       []modelAlias `json:"models"`
}

type Worker struct {
	Token              string
	EnrollmentSecret   []byte
	Binary             string
	Image              string
	MaxCells           int
	enrollmentMu       sync.RWMutex
	enrollmentByDevice map[string]openclawgateway.EnrollmentBinding
	mu                 sync.Mutex
}

func (w *Worker) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Cache-Control", "no-store")
	if len(w.Token) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+w.Token)) != 1 {
		http.Error(rw, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/gateway/") {
		w.proxyGateway(rw, r)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if r.Method != http.MethodPost || len(parts) < 4 || len(parts) > 5 || parts[0] != "v1" || parts[1] != "cells" || !tenantPattern.MatchString(parts[2]) {
		http.Error(rw, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}
	tenant, action := parts[2], parts[3]
	connectionKind := ""
	if len(parts) == 5 {
		if parts[3] != "connections" || (parts[4] != "telegram" && parts[4] != "wechat" && parts[4] != "gws") {
			http.Error(rw, `{"error":"unsupported connection"}`, http.StatusBadRequest)
			return
		}
		connectionKind = parts[4]
		action = "connect"
	}
	if action != "create" && action != "start" && action != "stop" && action != "delete" && action != "status" && action != "dashboard" && action != "devices" && action != "connect" && action != "configure" && action != "enrollment" {
		http.Error(rw, `{"error":"unsupported action"}`, http.StatusBadRequest)
		return
	}
	// Serializing lifecycle commands prevents concurrent create capacity races.
	if !w.mu.TryLock() {
		http.Error(rw, `{"error":"worker busy; retry shortly"}`, http.StatusConflict)
		return
	}
	defer w.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	var result Result
	var err error
	switch action {
	case "enrollment":
		var request openclawgateway.EnrollmentCommand
		decoder := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || request.Validate() != nil {
			http.Error(rw, `{"error":"invalid enrollment request"}`, http.StatusBadRequest)
			return
		}
		result.EnrollmentResult, err = gatewayEnrollmentOperation(ctx, tenant, request, w.cellCommandInput)
		result.State = "ready"
	case "devices":
		var request openclawgateway.Request
		if json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&request) != nil {
			http.Error(rw, `{"error":"invalid device request"}`, http.StatusBadRequest)
			return
		}
		result, err = w.gatewayDevices(ctx, tenant, request)
	case "connect":
		var request struct {
			Credential string `json:"credential"`
			Account    string `json:"account"`
			Name       string `json:"name"`
		}
		if decodeErr := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&request); decodeErr != nil || (strings.TrimSpace(request.Credential) == "" && connectionKind != "wechat" && connectionKind != "gws") {
			http.Error(rw, `{"error":"credential required"}`, http.StatusBadRequest)
			return
		}
		result, err = w.connect(ctx, tenant, connectionKind, request.Credential, request.Account, request.Name)
	case "configure":
		var mapping modelMapping
		if decodeErr := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&mapping); decodeErr != nil {
			http.Error(rw, `{"error":"model mapping required"}`, http.StatusBadRequest)
			return
		}
		err = w.configure(ctx, tenant, mapping)
		result = Result{State: "configured"}
	default:
		if action == "dashboard" {
			result, err = w.dashboard(ctx, tenant)
		} else {
			result, err = w.execute(ctx, tenant, action)
		}
	}
	if err != nil {
		// CLI output may contain a one-time gateway token. Never log or return it.
		http.Error(rw, `{"error":"worker operation failed; operator inspection required"}`, http.StatusBadGateway)
		return
	}
	_ = json.NewEncoder(rw).Encode(result)
}

func (w *Worker) dashboard(ctx context.Context, tenant string) (Result, error) {
	if os.Getenv("OPENCLAW_GATEWAY_ENABLED") != "1" {
		return Result{}, errors.New("gateway access disabled")
	}
	origin, err := gatewayPublicOrigin(os.Getenv("OPENCLAW_GATEWAY_PUBLIC_ORIGIN_TEMPLATE"), tenant)
	if err != nil {
		return Result{}, err
	}
	controlUI, err := w.cellCommand(ctx, tenant, "openclaw", "config", "get", "gateway.controlUi", "--json")
	if err != nil || !gatewayOriginAllowed(controlUI, origin.String()) {
		return Result{}, errors.New("gateway origin has not been verified")
	}
	out, err := w.cellCommand(ctx, tenant, "openclaw", "dashboard", "--json")
	if err != nil {
		return Result{}, errors.New("openclaw dashboard handoff unavailable")
	}
	return gatewayBrowserHandoff(out, origin, time.Now())
}

func (w *Worker) command(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, w.Binary, args...) // #nosec G702 -- Operator-configured executable; arguments are not interpreted by a shell.
	cmd.WaitDelay = 3 * time.Second
	return cmd.Output()
}

func (w *Worker) cellCommand(ctx context.Context, tenant string, args ...string) ([]byte, error) {
	container := "openclaw-cell-" + tenant
	command := append([]string{"exec", container}, args...)
	cmd := exec.CommandContext(ctx, "podman", command...) // #nosec G702 -- Validated tenant and separate argv; no shell.
	cmd.WaitDelay = 3 * time.Second
	return cmd.Output()
}

func (w *Worker) cellCommandInput(ctx context.Context, tenant string, input []byte, args ...string) ([]byte, error) {
	container := "openclaw-cell-" + tenant
	command := append([]string{"exec", "-i", container}, args...)
	cmd := exec.CommandContext(ctx, "podman", command...) // #nosec G702 -- Validated tenant and separate argv; no shell.
	cmd.Stdin = bytes.NewReader(input)
	cmd.WaitDelay = 3 * time.Second
	return cmd.Output()
}

func (w *Worker) configure(ctx context.Context, tenant string, mapping modelMapping) error {
	parsed, err := url.Parse(strings.TrimSpace(mapping.GatewayURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("invalid model gateway URL")
	}
	if len(strings.TrimSpace(mapping.APIKey)) < 20 || len(mapping.Models) == 0 {
		return errors.New("invalid model mapping")
	}
	models := make([]map[string]string, 0, len(mapping.Models))
	for _, model := range mapping.Models {
		alias := strings.TrimSpace(model.Alias)
		modelID := strings.TrimSpace(model.Model)
		if alias == "" || modelID == "" || len([]rune(alias)) > 100 || len([]rune(modelID)) > 200 {
			return errors.New("invalid model alias")
		}
		models = append(models, map[string]string{"id": modelID, "name": alias})
	}
	defaultModel := strings.TrimSpace(mapping.DefaultModel)
	if defaultModel == "" {
		defaultModel = strings.TrimSpace(mapping.Models[0].Alias)
	}
	patch := map[string]any{
		"models": map[string]any{
			"providers": map[string]any{
				"sub2api": map[string]any{
					"baseUrl": parsed.String(),
					"api":     "openai-responses",
					"apiKey":  mapping.APIKey,
					"models":  models,
				},
			},
		},
		"agents": map[string]any{"defaults": map[string]any{"model": "sub2api/" + defaultModel}},
	}
	if os.Getenv("OPENCLAW_GATEWAY_ENABLED") == "1" {
		gateway, err := w.gatewayConfigPatch(ctx, tenant)
		if err != nil {
			return err
		}
		patch["gateway"] = gateway
	}
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	if _, err := w.cellCommandInput(ctx, tenant, patchJSON, "openclaw", "config", "patch", "--stdin"); err != nil {
		return errors.New("OpenClaw model mapping failed")
	}
	return nil
}

func (w *Worker) hostCommand(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("host command is empty")
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...) // #nosec G702 -- Call sites supply fixed executable names and separate argv; no shell.
	cmd.WaitDelay = 3 * time.Second
	return cmd.Output()
}

func (w *Worker) connect(ctx context.Context, tenant, kind, credential, account, name string) (Result, error) {
	if kind == "wechat" {
		return w.connectWeChat(ctx, tenant, credential, account)
	}
	if kind == "gws" {
		return w.connectGoogleWorkspace(ctx, tenant, credential)
	}
	if kind != "telegram" {
		return Result{}, errors.New("unsupported connection")
	}
	credential = strings.TrimSpace(credential)
	if len(credential) < 20 || strings.ContainsAny(credential, "\r\n") {
		return Result{}, errors.New("invalid telegram credential")
	}
	if account == "" {
		account = "main"
	}
	if name == "" {
		name = "Telegram"
	}
	if _, err := w.cellCommand(ctx, tenant, "openclaw", "plugins", "install", "telegram", "--accept-capabilities", "--acknowledge-install-policy-warning"); err != nil {
		return Result{}, err
	}
	if _, err := w.cellCommand(ctx, tenant, "openclaw", "plugins", "enable", "telegram"); err != nil {
		return Result{}, err
	}
	if _, err := w.cellCommand(ctx, tenant, "openclaw", "channels", "add", "--channel", "telegram", "--token", credential, "--account", account, "--name", name); err != nil {
		return Result{}, err
	}
	statusOut, err := w.cellCommand(ctx, tenant, "openclaw", "channels", "status", "--channel", "telegram", "--probe", "--json")
	if err != nil {
		return Result{}, err
	}
	var status struct {
		ChannelAccounts map[string][]struct {
			AccountID string `json:"accountId"`
			Probe     *struct {
				OK    bool   `json:"ok"`
				Error string `json:"error"`
			} `json:"probe"`
			LastError string `json:"lastError"`
		} `json:"channelAccounts"`
	}
	if err := json.Unmarshal(statusOut, &status); err != nil {
		return Result{}, err
	}
	for _, accountState := range status.ChannelAccounts["telegram"] {
		if accountState.AccountID == account {
			if accountState.Probe != nil && accountState.Probe.OK {
				return Result{State: "connected"}, nil
			}
			if accountState.Probe != nil && accountState.Probe.Error != "" {
				return Result{}, errors.New(accountState.Probe.Error)
			}
			if accountState.LastError != "" {
				return Result{}, errors.New(accountState.LastError)
			}
		}
	}
	return Result{}, errors.New("telegram credential could not be verified")
}

func (w *Worker) connectGoogleWorkspace(ctx context.Context, tenant, credential string) (Result, error) {
	if err := w.ensureGoogleWorkspacePlugin(ctx, tenant); err != nil {
		return Result{}, err
	}
	// Use the tenant's installed Google Workspace plugin directly. This keeps
	// OAuth setup deterministic and avoids spending a model turn just to build
	// a URL; the resulting token file remains inside this tenant cell.
	script := `import fs from "node:fs";
import { createAuthService } from "/home/node/.openclaw/extensions/openclaw-google-workspace/dist/auth/google-auth.js";
import { resolvePluginConfig } from "/home/node/.openclaw/extensions/openclaw-google-workspace/dist/config/schema.js";
const configPath = "/home/node/.openclaw/openclaw.json";
const root = JSON.parse(fs.readFileSync(configPath, "utf8"));
const raw = root.plugins?.entries?.["openclaw-google-workspace"]?.config ?? {};
const auth = createAuthService(resolvePluginConfig({ pluginConfig: raw }));
const input = process.argv[1] ?? "";
if (!input.trim()) {
  const request = await auth.createAuthorizationUrl();
  console.log(JSON.stringify({ state: "pending", setup_url: request.url, setup_key: "gws", message: "Open the Google link, approve access, then paste the returned code or full redirect URL here." }));
} else {
  let code = input.trim();
  try {
    if (code.startsWith("http://") || code.startsWith("https://")) code = new URL(code).searchParams.get("code") ?? code;
  } catch {}
  await auth.exchangeCodeForToken(code);
  console.log(JSON.stringify({ state: "connected", message: "Google Workspace authorization completed for this tenant." }));
}`
	out, err := w.cellCommand(ctx, tenant, "node", "--input-type=module", "-e", script, credential)
	if err != nil {
		return Result{}, errors.New("google workspace OAuth is not available in this tenant")
	}
	var result Result
	if err := json.Unmarshal(out, &result); err != nil {
		return Result{}, errors.New("google workspace OAuth returned an invalid response")
	}
	return result, nil
}

func (w *Worker) ensureGoogleWorkspacePlugin(ctx context.Context, tenant string) error {
	container := "openclaw-cell-" + tenant
	pluginEntry := "/home/node/.openclaw/extensions/openclaw-google-workspace/dist/auth/google-auth.js"
	if _, err := w.cellCommand(ctx, tenant, "sh", "-lc", "test -s "+shellQuote(pluginEntry)); err != nil {
		archive := os.Getenv("OPENCLAW_GOOGLE_WORKSPACE_PLUGIN_ARCHIVE")
		if archive == "" {
			archive = "/opt/openclaw/google-workspace-plugin-fixed2.tgz"
		}
		if _, err := w.hostCommand(ctx, "podman", "cp", archive, container+":/tmp/google-workspace-plugin.tgz"); err != nil {
			return errors.New("google workspace plugin is not installed on this tenant")
		}
		if _, err := w.cellCommand(ctx, tenant, "openclaw", "plugins", "install", "/tmp/google-workspace-plugin.tgz", "--accept-capabilities", "--acknowledge-install-policy-warning", "--force", "--pin"); err != nil {
			return errors.New("google workspace plugin could not be installed")
		}
	}
	clientFile := os.Getenv("OPENCLAW_GOOGLE_OAUTH_CLIENT_FILE")
	if clientFile == "" {
		clientFile = "/opt/openclaw/google-oauth-client.json"
	}
	credentialsPath := "/home/node/.openclaw/secrets/google-oauth.json"
	if _, err := w.cellCommand(ctx, tenant, "sh", "-lc", "test -s "+shellQuote(credentialsPath)); err != nil {
		if _, err := w.hostCommand(ctx, "podman", "cp", clientFile, container+":"+credentialsPath); err != nil {
			return errors.New("google workspace OAuth client is not configured on this tenant")
		}
	}
	config := `{"credentialsPath":"/home/node/.openclaw/secrets/google-oauth.json","tokenPath":"/home/node/.openclaw/secrets/google-tokens.json","oauthRedirectUri":"http://127.0.0.1:50696/oauth2callback","services":{"gmail":{"enabled":true,"readOnly":true,"maxSearchResults":20},"calendar":{"enabled":true,"readOnly":true,"defaultCalendarId":"primary","upcomingWindowDays":7},"drive":{"enabled":true,"readOnly":true,"maxSearchResults":20}}}`
	if _, err := w.cellCommand(ctx, tenant, "openclaw", "config", "get", "plugins.entries.openclaw-google-workspace.config", "--json"); err != nil {
		if _, err := w.cellCommand(ctx, tenant, "openclaw", "config", "set", "plugins.entries.openclaw-google-workspace.config", config, "--json"); err != nil {
			return errors.New("google workspace plugin configuration failed")
		}
	}
	return nil
}

func (w *Worker) connectWeChat(ctx context.Context, tenant, sessionKey, account string) (Result, error) {
	if account == "" {
		account = "main"
	}
	if err := w.ensureWeChatPlugin(ctx, tenant); err != nil {
		return Result{}, err
	}
	method := "web.login.start"
	params := map[string]any{"channel": "openclaw-weixin", "accountId": account, "force": true}
	if strings.TrimSpace(sessionKey) != "" {
		method = "web.login.wait"
		params = map[string]any{"channel": "openclaw-weixin", "accountId": account, "sessionKey": strings.TrimSpace(sessionKey)}
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return Result{}, err
	}
	out, err := w.cellCommand(ctx, tenant, "openclaw", "gateway", "call", method, "--params", string(paramsJSON), "--json", "--timeout", "15000")
	if err != nil {
		return Result{}, err
	}
	var response struct {
		QRDataURL  string `json:"qrDataUrl"`
		SessionKey string `json:"sessionKey"`
		Connected  bool   `json:"connected"`
		Message    string `json:"message"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return Result{}, err
	}
	if method == "web.login.start" {
		if response.QRDataURL == "" || response.SessionKey == "" {
			return Result{}, errors.New("wechat login did not return a QR session")
		}
		return Result{State: "pending", SetupURL: response.QRDataURL, SetupKey: response.SessionKey, Message: response.Message}, nil
	}
	if response.Connected {
		return Result{State: "connected", Message: response.Message}, nil
	}
	return Result{State: "pending", SetupKey: sessionKey, Message: response.Message}, nil
}

func (w *Worker) ensureWeChatPlugin(ctx context.Context, tenant string) error {
	inspect, inspectErr := w.cellCommand(ctx, tenant, "openclaw", "plugins", "inspect", "openclaw-weixin", "--json")
	if inspectErr != nil {
		if _, err := w.cellCommand(ctx, tenant, "openclaw", "plugins", "install", "@tencent-weixin/openclaw-weixin@2.4.8", "--accept-capabilities", "--acknowledge-install-policy-warning", "--force", "--pin"); err != nil {
			return err
		}
		inspect, inspectErr = w.cellCommand(ctx, tenant, "openclaw", "plugins", "inspect", "openclaw-weixin", "--json")
		if inspectErr != nil {
			return inspectErr
		}
	}
	var info struct {
		Plugin struct {
			RootDir string `json:"rootDir"`
			Status  string `json:"status"`
			Enabled bool   `json:"enabled"`
		} `json:"plugin"`
	}
	if err := json.Unmarshal(inspect, &info); err != nil || !strings.HasPrefix(info.Plugin.RootDir, "/home/node/.openclaw/") {
		return errors.New("invalid WeChat plugin location")
	}
	root := filepath.ToSlash(info.Plugin.RootDir)
	authCompat := `export async function resolveSenderCommandAuthorizationWithRuntime(opts) {
  const allowFrom = (await opts.readAllowFromStore?.()) ?? [];
  const senderId = String(opts.senderId ?? "");
  const senderAllowedForCommands = allowFrom.length === 0
    ? true
    : (opts.isSenderAllowed ? opts.isSenderAllowed(senderId, allowFrom) : allowFrom.includes(senderId));
  return { senderAllowedForCommands, commandAuthorized: senderAllowedForCommands };
}
export function resolveDirectDmAuthorizationOutcome({ dmPolicy, senderAllowedForCommands }) {
  if (dmPolicy === "disabled") return "disabled";
  return senderAllowedForCommands ? "authorized" : "unauthorized";
}
`
	processMessage := root + "/dist/src/messaging/process-message.js"
	commandAuthCompat := root + "/dist/src/command-auth-compat.js"
	patch := "printf '%s' " + shellQuote(authCompat) + " > " + shellQuote(commandAuthCompat) + "; " +
		"for f in " + shellQuote(processMessage) + " " + shellQuote(root+"/dist/src/channel.js") + " " + shellQuote(root+"/dist/src/util/logger.js") + "; do [ -f \"$f\" ] && sed -i 's#openclaw/plugin-sdk/infra-runtime#openclaw/plugin-sdk/temp-path#g' \"$f\"; done; " +
		"[ -f " + shellQuote(processMessage) + " ] && sed -i 's#openclaw/plugin-sdk/channel-message#openclaw/plugin-sdk/channel-outbound#g; s#openclaw/plugin-sdk/command-auth#../command-auth-compat.js#g' " + shellQuote(processMessage) + "; " +
		"f=" + shellQuote(root+"/dist/src/auth/pairing.js") + "; [ -f \"$f\" ] && sed -i 's#openclaw/plugin-sdk/infra-runtime#openclaw/plugin-sdk/file-lock#g' \"$f\""
	if _, err := w.cellCommand(ctx, tenant, "sh", "-lc", patch); err != nil {
		return err
	}
	if info.Plugin.Enabled && info.Plugin.Status == "loaded" {
		_, err := w.cellCommand(ctx, tenant, "openclaw", "plugins", "reload", "openclaw-weixin")
		return err
	}
	if info.Plugin.Enabled {
		_, err := w.cellCommand(ctx, tenant, "openclaw", "plugins", "reload", "openclaw-weixin")
		return err
	}
	_, err := w.cellCommand(ctx, tenant, "openclaw", "plugins", "enable", "openclaw-weixin")
	return err
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (w *Worker) execute(ctx context.Context, tenant, action string) (Result, error) {
	var registry struct {
		Cells []struct {
			Tenant string `json:"tenant"`
			State  string `json:"state"`
			Port   int    `json:"port"`
		} `json:"cells"`
	}
	out, err := w.command(ctx, "fleet", "list", "--json")
	if err != nil || json.Unmarshal(out, &registry) != nil {
		return Result{}, errors.New("fleet unavailable")
	}
	var existing *Result
	for _, cell := range registry.Cells {
		if cell.Tenant == tenant {
			existing = &Result{State: cell.State, Port: cell.Port}
		}
	}
	if action == "status" {
		if existing == nil {
			return Result{State: "missing"}, nil
		}
		if existing.Port > 0 {
			if latency := probeCell(ctx, existing.Port); latency != nil {
				existing.LatencyMS = latency
			}
		}
		return *existing, nil
	}
	if action == "create" {
		if existing != nil {
			// A previous create can have reached readiness without completing
			// inherited-feature setup. Retry the idempotent managed bootstrap.
			if existing.Port < 1 {
				return Result{}, errors.New("existing cell has no gateway port")
			}
			if _, err := w.command(ctx, "fleet", "start", tenant); err != nil {
				return Result{}, err
			}
			existing.State = "running"
			return *existing, nil
		}
		if w.MaxCells <= 0 || len(registry.Cells) >= w.MaxCells {
			return Result{}, errors.New("worker capacity reached")
		}
		args := []string{"fleet", "create", tenant, "--runtime", "podman", "--memory", "2g", "--cpus", "2", "--pids-limit", "512", "--json"}
		if w.Image != "" {
			args = append(args, "--image", w.Image)
		}
		out, err := w.command(ctx, args...)
		if err != nil {
			return Result{}, err
		}
		var created struct {
			Port    int  `json:"port"`
			Started bool `json:"started"`
		}
		if json.Unmarshal(out, &created) != nil || created.Port < 1 || !created.Started {
			return Result{}, errors.New("invalid create response")
		}
		return Result{State: "running", Port: created.Port}, nil
	}
	if existing == nil && action == "delete" {
		return Result{State: "missing"}, nil
	}
	if existing == nil {
		return Result{}, errors.New("cell missing")
	}
	args := []string{"fleet", action, tenant}
	state := "running"
	switch action {
	case "delete":
		args = []string{"fleet", "rm", "--force", "--purge-data", tenant}
		state = "missing"
	case "stop":
		state = "stopped"
	}
	if _, err := w.command(ctx, args...); err != nil {
		return Result{}, err
	}
	return Result{State: state, Port: existing.Port}, nil
}

func probeCell(parent context.Context, port int) *int {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+"/healthz", nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	value := int(time.Since(started).Milliseconds())
	return &value
}
