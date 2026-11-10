package openclawworker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestConfigureSpecifiesResponsesProtocol(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "patch.json")
	// Capture the exact patch sent to the cell, without contacting a gateway.
	if err := os.WriteFile(filepath.Join(dir, "podman"), []byte("#!/bin/sh\ncat > \"$CLAW_TEST_PATCH\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLAW_TEST_PATCH", capture)
	w := &Worker{}
	mapping := modelMapping{
		GatewayURL: "https://gateway.example/v1", APIKey: strings.Repeat("x", 32),
		DefaultModel: "test", Models: []modelAlias{{Alias: "test", Model: "test"}, {Alias: "second", Model: "second-model"}},
	}
	for range 2 {
		if err := w.configure(context.Background(), "u1-123456789abc", mapping); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(capture)
		if err != nil {
			t.Fatal(err)
		}
		var patch struct {
			Models struct {
				Providers map[string]struct {
					API    string `json:"api"`
					Models []struct {
						ID     string `json:"id"`
						Name   string `json:"name"`
						Compat struct {
							SupportsPromptCacheKey *bool `json:"supportsPromptCacheKey"`
						} `json:"compat"`
					} `json:"models"`
				} `json:"providers"`
			} `json:"models"`
		}
		if err := json.Unmarshal(data, &patch); err != nil {
			t.Fatal(err)
		}
		provider := patch.Models.Providers["sub2api"]
		if got := provider.API; got != "openai-responses" {
			t.Fatalf("provider protocol = %q, want openai-responses", got)
		}
		if len(provider.Models) != len(mapping.Models) {
			t.Fatalf("models = %d, want %d", len(provider.Models), len(mapping.Models))
		}
		for i, model := range provider.Models {
			if model.ID != mapping.Models[i].Model || model.Name != mapping.Models[i].Alias {
				t.Fatalf("model %d = %q/%q, want %q/%q", i, model.ID, model.Name, mapping.Models[i].Model, mapping.Models[i].Alias)
			}
			// Without this flag OpenClaw omits prompt_cache_key for non-OpenAI base URLs.
			if flag := model.Compat.SupportsPromptCacheKey; flag == nil || !*flag {
				t.Fatalf("model %q missing compat.supportsPromptCacheKey=true", model.ID)
			}
		}
	}
}

// fakePodman installs a podman stub that logs each cell command to CLAW_TEST_LOG
// (one line per call) and answers plugin inspect / gateway call with canned JSON.
// An empty wait or status response makes that command exit 1.
func fakePodman(t *testing.T, waitResponse, statusResponse string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell podman stub needs a POSIX host")
	}
	dir := t.TempDir()
	logFile := filepath.Join(dir, "calls.log")
	script := `#!/bin/sh
# argv: exec <container> <cmd...>
case "$3 $4" in
  "openclaw gateway"|"openclaw config") printf '%s\n' "$*" >> "$CLAW_TEST_LOG" ;;
  *) printf '%s %s %s\n' "$3" "$4" "$5" >> "$CLAW_TEST_LOG" ;;
esac
case "$3 $4 $5" in
  "openclaw config set") [ "$CLAW_TEST_CONFIG" != fail ] || { echo boom >&2; exit 1; } ;;
  "openclaw plugins inspect") printf '%s' '{"plugin":{"rootDir":"/home/node/.openclaw/extensions/openclaw-weixin","status":"loaded","enabled":true}}' ;;
  "openclaw gateway call")
    case "$6" in
      web.login.start)
        [ "$CLAW_TEST_START" != fail ] || { echo boom >&2; exit 1; }
        if [ -n "$CLAW_TEST_START" ]; then printf '%s' "$CLAW_TEST_START"; else printf '%s' '{"qrDataUrl":"data:image/png;base64,QR","sessionKey":"sess-1","message":"scan"}'; fi ;;
      web.login.wait) [ -n "$CLAW_TEST_WAIT" ] || { echo timeout >&2; exit 1; }; printf '%s' "$CLAW_TEST_WAIT" ;;
      channels.status) [ -n "$CLAW_TEST_STATUS" ] || { echo down >&2; exit 1; }; printf '%s' "$CLAW_TEST_STATUS" ;;
    esac ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "podman"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLAW_TEST_LOG", logFile)
	t.Setenv("CLAW_TEST_WAIT", waitResponse)
	t.Setenv("CLAW_TEST_STATUS", statusResponse)
	return logFile
}

func readCalls(t *testing.T, logFile string) []string {
	t.Helper()
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

const (
	weixinConfigured = `{"channelAccounts":{"openclaw-weixin":[{"accountId":"x-im-bot","enabled":true,"configured":true}],"telegram":[]}}`
	weixinNone       = `{"channelAccounts":{"openclaw-weixin":[],"telegram":[{"configured":true}]}}`
)

// requireWaitOnly checks the first call is web.login.wait and that no plugin prepare ran.
func requireWaitOnly(t *testing.T, calls []string) {
	t.Helper()
	first := calls[0]
	if !strings.HasPrefix(first, "exec openclaw-cell-u1-123456789abc openclaw gateway call web.login.wait --params ") ||
		!strings.Contains(first, `"sessionKey":"sess-1"`) {
		t.Fatalf("unexpected first cell command: %q", first)
	}
	for _, c := range calls {
		if strings.Contains(c, "plugins") || strings.Contains(c, " sh ") || strings.HasPrefix(c, "sh ") {
			t.Fatalf("wait path issued a plugin/sh command: %q", c)
		}
	}
}

const (
	weixinStopped = `{"channelAccounts":{"openclaw-weixin":[{"accountId":"x-im-bot","enabled":true,"configured":true,"running":false}]}}`
	weixinRunning = `{"channelAccounts":{"openclaw-weixin":[{"accountId":"x-im-bot","enabled":true,"configured":true,"running":true}]}}`
	configSetPfx  = "exec openclaw-cell-u1-123456789abc openclaw config set channels.openclaw-weixin.channelConfigUpdatedAt "
)

var configSetTimestamp = regexp.MustCompile(`^"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z" --json$`)

// configSetCalls returns the channelConfigUpdatedAt writes and fails if one has a malformed value.
func configSetCalls(t *testing.T, calls []string) int {
	t.Helper()
	n := 0
	for _, c := range calls {
		if !strings.Contains(c, "openclaw config set") {
			continue
		}
		n++
		if !strings.HasPrefix(c, configSetPfx) || !configSetTimestamp.MatchString(strings.TrimPrefix(c, configSetPfx)) {
			t.Fatalf("unexpected config set call: %q", c)
		}
	}
	return n
}

func TestConnectWeChatWaitConnectedAccountRunningDoesNotSetConfig(t *testing.T) {
	logFile := fakePodman(t, `{"connected":true,"message":"ok"}`, weixinRunning)
	res, err := (&Worker{}).connectWeChat(context.Background(), "u1-123456789abc", " sess-1 ", "main")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "connected" {
		t.Fatalf("state = %q, want connected", res.State)
	}
	calls := readCalls(t, logFile)
	if len(calls) != 2 || !strings.Contains(calls[1], "gateway call channels.status") {
		t.Fatalf("want wait then channels.status, got %q", calls)
	}
	requireWaitOnly(t, calls)
	if n := configSetCalls(t, calls); n != 0 {
		t.Fatalf("config set calls = %d, want 0", n)
	}
}

func TestConnectWeChatWaitConnectedStoppedAccountStartsChannel(t *testing.T) {
	logFile := fakePodman(t, `{"connected":true,"message":"ok"}`, weixinStopped)
	res, err := (&Worker{}).connectWeChat(context.Background(), "u1-123456789abc", " sess-1 ", "main")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "connected" || res.Message != "ok" {
		t.Fatalf("unexpected result: %+v", res)
	}
	calls := readCalls(t, logFile)
	if len(calls) != 3 || !strings.Contains(calls[1], "gateway call channels.status") {
		t.Fatalf("want wait, channels.status, config set; got %q", calls)
	}
	requireWaitOnly(t, calls)
	if n := configSetCalls(t, calls); n != 1 {
		t.Fatalf("config set calls = %d, want 1: %q", n, calls)
	}
}

func TestConnectWeChatWaitConnectedStatusFailsStillConnectedNoConfigSet(t *testing.T) {
	logFile := fakePodman(t, `{"connected":true,"message":"ok"}`, "")
	res, err := (&Worker{}).connectWeChat(context.Background(), "u1-123456789abc", "sess-1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "connected" {
		t.Fatalf("state = %q, want connected", res.State)
	}
	if n := configSetCalls(t, readCalls(t, logFile)); n != 0 {
		t.Fatalf("config set calls = %d, want 0", n)
	}
}

func TestConnectWeChatWaitNotConnectedStoppedAccountIsConnectedAndStartsChannel(t *testing.T) {
	logFile := fakePodman(t, `{"connected":false,"message":"no active login"}`, weixinStopped)
	res, err := (&Worker{}).connectWeChat(context.Background(), "u1-123456789abc", "sess-1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "connected" {
		t.Fatalf("state = %q, want connected", res.State)
	}
	calls := readCalls(t, logFile)
	if len(calls) != 3 {
		t.Fatalf("want wait, channels.status, config set; got %q", calls)
	}
	if n := configSetCalls(t, calls); n != 1 {
		t.Fatalf("config set calls = %d, want 1: %q", n, calls)
	}
}

func TestConnectWeChatConfigSetFailureStillConnected(t *testing.T) {
	logFile := fakePodman(t, `{"connected":true,"message":"ok"}`, weixinStopped)
	t.Setenv("CLAW_TEST_CONFIG", "fail")
	res, err := (&Worker{}).connectWeChat(context.Background(), "u1-123456789abc", "sess-1", "main")
	if err != nil {
		t.Fatalf("config set failure must not surface: %v", err)
	}
	if res.State != "connected" || res.Message != "ok" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if n := configSetCalls(t, readCalls(t, logFile)); n != 1 {
		t.Fatalf("config set calls = %d, want 1", n)
	}
}

const newQRStart = `{"qrDataUrl":"data:image/png;base64,NEW","sessionKey":"sess-2","message":"scan"}`

// requireForceFalseStart checks the last call is web.login.start with force:false and nothing touched plugins or sh.
func requireForceFalseStart(t *testing.T, calls []string) {
	t.Helper()
	last := calls[len(calls)-1]
	if !strings.Contains(last, "gateway call web.login.start --params ") || !strings.Contains(last, `"force":false`) || strings.Contains(last, `"force":true`) {
		t.Fatalf("want a force:false web.login.start last, got %q", calls)
	}
	requireWaitOnly(t, calls)
}

func TestConnectWeChatWaitNotConnectedNoAccountHandsBackNewQR(t *testing.T) {
	logFile := fakePodman(t, `{"connected":false,"message":"no active login"}`, weixinNone)
	t.Setenv("CLAW_TEST_START", newQRStart)
	res, err := (&Worker{}).connectWeChat(context.Background(), "u1-123456789abc", " sess-1 ", "main")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "pending" || res.SetupURL != "data:image/png;base64,NEW" || res.SetupKey != "sess-2" || res.Message != weChatQRExpired {
		t.Fatalf("unexpected result: %+v", res)
	}
	calls := readCalls(t, logFile)
	if len(calls) != 3 || !strings.Contains(calls[1], "gateway call channels.status") {
		t.Fatalf("want wait, channels.status, start; got %q", calls)
	}
	requireForceFalseStart(t, calls)
}

func TestConnectWeChatWaitNotConnectedStartFailsFallsBackToSameKey(t *testing.T) {
	logFile := fakePodman(t, `{"connected":false,"message":"waiting"}`, weixinNone)
	t.Setenv("CLAW_TEST_START", "fail")
	res, err := (&Worker{}).connectWeChat(context.Background(), "u1-123456789abc", " sess-1 ", "main")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "pending" || res.SetupKey != " sess-1 " || res.SetupURL != "" || res.Message != "waiting" {
		t.Fatalf("unexpected result: %+v", res)
	}
	requireForceFalseStart(t, readCalls(t, logFile))
}

func TestConnectWeChatWaitFalseButAccountConfiguredIsConnected(t *testing.T) {
	logFile := fakePodman(t, `{"connected":false,"message":"no active login"}`, weixinConfigured)
	res, err := (&Worker{}).connectWeChat(context.Background(), "u1-123456789abc", "sess-1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "connected" {
		t.Fatalf("state = %q, want connected", res.State)
	}
	calls := readCalls(t, logFile)
	requireWaitOnly(t, calls)
	for _, c := range calls {
		if strings.Contains(c, "web.login.start") {
			t.Fatalf("configured account must not start a login: %q", c)
		}
	}
}

func TestConnectWeChatWaitErrorWithoutAccountHandsBackQRWithStillWaiting(t *testing.T) {
	logFile := fakePodman(t, "", weixinNone)
	t.Setenv("CLAW_TEST_START", newQRStart)
	res, err := (&Worker{}).connectWeChat(context.Background(), "u1-123456789abc", "sess-1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "pending" || res.SetupURL != "data:image/png;base64,NEW" || res.SetupKey != "sess-2" || res.Message != weChatStillWaiting {
		t.Fatalf("unexpected result: %+v", res)
	}
	requireForceFalseStart(t, readCalls(t, logFile))
}

func TestConnectWeChatWaitErrorStartFailsIsPendingWithoutRawError(t *testing.T) {
	fakePodman(t, "", weixinNone)
	t.Setenv("CLAW_TEST_START", "fail")
	res, err := (&Worker{}).connectWeChat(context.Background(), "u1-123456789abc", "sess-1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "pending" || res.SetupKey != "sess-1" || res.SetupURL != "" || res.Message != weChatStillWaiting || strings.Contains(res.Message, "exit status") {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestConnectWeChatWaitErrorAndStatusErrorReturnsError(t *testing.T) {
	logFile := fakePodman(t, "", "")
	if _, err := (&Worker{}).connectWeChat(context.Background(), "u1-123456789abc", "sess-1", "main"); err == nil {
		t.Fatal("want the wait error when both wait and status fail")
	}
	for _, c := range readCalls(t, logFile) {
		if strings.Contains(c, "web.login.start") {
			t.Fatalf("start must not be called when wait and status both fail: %q", c)
		}
	}
}

func TestConnectWeChatStartPreparesPluginFirst(t *testing.T) {
	logFile := fakePodman(t, "", "")
	w := &Worker{}
	res, err := w.connectWeChat(context.Background(), "u1-123456789abc", "  ", "main")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "pending" || res.SetupURL != "data:image/png;base64,QR" || res.SetupKey != "sess-1" {
		t.Fatalf("unexpected result: %+v", res)
	}
	calls := readCalls(t, logFile)
	inspect, start := -1, -1
	for i, c := range calls {
		if strings.Contains(c, "openclaw plugins inspect") && inspect < 0 {
			inspect = i
		}
		if strings.Contains(c, "gateway call web.login.start") {
			start = i
		}
	}
	if inspect < 0 || start < 0 || inspect > start {
		t.Fatalf("want plugins inspect before web.login.start, got %q", calls)
	}
	if !strings.Contains(calls[start], `"force":true`) {
		t.Fatalf("start path must use force:true: %q", calls[start])
	}
	if start != len(calls)-1 {
		t.Fatalf("web.login.start must be the last command: %q", calls)
	}
}
