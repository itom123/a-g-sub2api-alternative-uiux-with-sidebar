package openclawworker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	tgTenant = "u35-0123456789ab"
	tgToken  = "123456:TEST-TOKEN-not-a-real-token"
	tgOwner  = int64(4242424242)
)

// fakeTelegram is a minimal Bot API: it serves queued updates and records every call.
type fakeTelegram struct {
	mu          sync.Mutex
	calls       []string // method names
	offsets     []int64  // getUpdates offsets, in order
	sent        []map[string]any
	updates     []map[string]any
	getMeCode   int
	updatesCode int
	server      *httptest.Server
}

func newFakeTelegram(t *testing.T) *fakeTelegram {
	t.Helper()
	f := &fakeTelegram{}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeTelegram) serve(rw http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "bot"+tgToken {
		rw.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(rw, `{"ok":false,"error_code":404,"description":"Not Found"}`)
		return
	}
	var params map[string]any
	_ = json.NewDecoder(r.Body).Decode(&params)
	f.mu.Lock()
	defer f.mu.Unlock()
	method := parts[1]
	f.calls = append(f.calls, method)
	fail := func(code int) {
		rw.WriteHeader(code)
		_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "error_code": code, "description": "fail"})
	}
	ok := func(result any) { _ = json.NewEncoder(rw).Encode(map[string]any{"ok": true, "result": result}) }
	switch method {
	case "getMe":
		if f.getMeCode != 0 {
			fail(f.getMeCode)
			return
		}
		ok(map[string]any{"id": 777, "is_bot": true, "first_name": "Bug", "username": "testbugbot"})
	case "deleteWebhook":
		ok(true)
	case "getUpdates":
		if f.updatesCode != 0 {
			fail(f.updatesCode)
			return
		}
		offset := int64(params["offset"].(float64))
		f.offsets = append(f.offsets, offset)
		var out []map[string]any
		var keep []map[string]any
		for _, u := range f.updates {
			if int64(u["update_id"].(int)) >= offset {
				out = append(out, u)
				keep = append(keep, u)
			}
		}
		f.updates = keep // confirmed updates are gone, like the real API
		if out == nil {
			out = []map[string]any{}
		}
		ok(out)
	case "sendMessage":
		f.sent = append(f.sent, params)
		ok(map[string]any{"message_id": 1})
	default:
		fail(http.StatusBadRequest)
	}
}

func (f *fakeTelegram) push(id int, chatType string, chatID, fromID int64, text, lang string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, map[string]any{"update_id": id, "message": map[string]any{
		"text": text, "chat": map[string]any{"id": chatID, "type": chatType},
		"from": map[string]any{"id": fromID, "is_bot": false, "first_name": "Ada", "username": "ada_example", "language_code": lang},
	}})
}

func (f *fakeTelegram) count(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == method {
			n++
		}
	}
	return n
}

type cellCall struct {
	args  string
	input []byte
}

// fakeCell records cell commands and answers the few the claim reads.
type fakeCell struct {
	mu       sync.Mutex
	calls    []cellCall
	commands string // `config get commands --json` output; "" = command fails
	probeOK  bool
	patchErr bool
}

func (c *fakeCell) run(_ context.Context, tenant string, input []byte, args ...string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	joined := strings.Join(args, " ")
	c.calls = append(c.calls, cellCall{args: tenant + " " + joined, input: input})
	switch {
	case joined == "openclaw config get commands --json":
		if c.commands == "" {
			return nil, io.ErrUnexpectedEOF
		}
		return []byte(c.commands), nil
	case strings.HasPrefix(joined, "openclaw config patch"):
		if c.patchErr {
			return nil, io.ErrUnexpectedEOF
		}
		return []byte("{}"), nil
	case strings.HasPrefix(joined, "openclaw channels status"):
		if !c.probeOK {
			return []byte(`{"channelAccounts":{"telegram":[{"accountId":"main","probe":{"ok":false}}]}}`), nil
		}
		return []byte(`{"channelAccounts":{"telegram":[{"accountId":"main","probe":{"ok":true}}]}}`), nil
	}
	return []byte("{}"), nil
}

func (c *fakeCell) argsList() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.calls))
	for i, call := range c.calls {
		out[i] = call.args
	}
	return out
}

func (c *fakeCell) patches() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out [][]byte
	for _, call := range c.calls {
		if strings.Contains(call.args, "config patch --stdin") && !strings.Contains(call.args, "--dry-run") {
			out = append(out, call.input)
		}
	}
	return out
}

func writePolicy(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "telegram-owner-claim.json")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func newClaimWorker(t *testing.T, policy string) (*Worker, *fakeTelegram, *fakeCell, string) {
	t.Helper()
	dir := t.TempDir()
	tg := newFakeTelegram(t)
	cell := &fakeCell{commands: `{"native":"auto","restart":true}`, probeOK: true}
	w := &Worker{
		telegramAPIBase:  tg.server.URL,
		telegramHTTP:     tg.server.Client(),
		telegramProbeGap: time.Millisecond,
		claimPolicyPath:  writePolicy(t, dir, policy),
		cellRun:          cell.run,
	}
	return w, tg, cell, dir
}

func startClaim(t *testing.T, w *Worker) Result {
	t.Helper()
	res, err := w.connect(context.Background(), tgTenant, "telegram", " "+tgToken+" ", "", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return res
}

func claimCode(t *testing.T, res Result) string {
	t.Helper()
	const prefix = "https://t.me/testbugbot?start="
	if !strings.HasPrefix(res.SetupURL, prefix) {
		t.Fatalf("setup url = %q", res.SetupURL)
	}
	return strings.TrimPrefix(res.SetupURL, prefix)
}

func TestTelegramPolicyGate(t *testing.T) {
	cases := []struct {
		name, body string
		want       bool
	}{
		{"missing file", "", false},
		{"invalid json", "{", false},
		{"off", `{"all":false,"tenants":[]}`, false},
		{"other tenant", `{"tenants":["u2-0123456789ab"]}`, false},
		{"listed tenant", `{"tenants":["` + tgTenant + `"]}`, true},
		{"all", `{"all":true}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &Worker{claimPolicyPath: writePolicy(t, t.TempDir(), tc.body)}
			if got := w.telegramClaimEnabled(tgTenant); got != tc.want {
				t.Fatalf("enabled = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTelegramClaimStartReturnsLinkWithoutAddingChannel(t *testing.T) {
	w, tg, cell, _ := newClaimWorker(t, `{"tenants":["`+tgTenant+`"]}`)
	res := startClaim(t, w)
	if res.State != "pending" || !isTelegramClaimKey(res.SetupKey) {
		t.Fatalf("result = %+v", res)
	}
	code := claimCode(t, res)
	if len(code) != telegramCodeLength || strings.Trim(code, telegramCodeAlphabet) != "" {
		t.Fatalf("code = %q", code)
	}
	if !strings.Contains(res.Message, "@testbugbot") || !strings.Contains(res.Message, code[:5]+"-"+code[5:]) {
		t.Fatalf("message = %q", res.Message)
	}
	encoded, _ := json.Marshal(res)
	if strings.Contains(string(encoded), tgToken) {
		t.Fatal("result leaks the bot token")
	}
	want := []string{
		tgTenant + " openclaw plugins install telegram --accept-capabilities --acknowledge-install-policy-warning",
		tgTenant + " openclaw plugins enable telegram",
	}
	if got := cell.argsList(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("cell calls = %q, want %q", got, want)
	}
	if tg.count("getMe") != 1 || tg.count("deleteWebhook") != 1 || tg.count("getUpdates") != 0 {
		t.Fatalf("telegram calls = %v", tg.calls)
	}
}

func TestTelegramClaimStartRejectsBadToken(t *testing.T) {
	w, tg, cell, _ := newClaimWorker(t, `{"all":true}`)
	tg.getMeCode = http.StatusUnauthorized
	_, err := w.connect(context.Background(), tgTenant, "telegram", tgToken, "", "")
	if err == nil || strings.Contains(err.Error(), tgToken) {
		t.Fatalf("err = %v", err)
	}
	if len(cell.argsList()) != 0 || len(w.telegramClaims) != 0 {
		t.Fatal("bad token touched the cell or stored a claim")
	}
}

func TestTelegramClaimUnreachableErrorHidesToken(t *testing.T) {
	w := &Worker{telegramAPIBase: "http://127.0.0.1:1", telegramHTTP: &http.Client{Timeout: time.Second}}
	_, err := w.telegramCall(context.Background(), tgToken, "getMe", nil, nil)
	if err == nil || strings.Contains(err.Error(), tgToken) || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("err = %v", err)
	}
}

func TestTelegramClaimPollWaitsIgnoresStrangersAndGroups(t *testing.T) {
	w, tg, cell, _ := newClaimWorker(t, `{"all":true}`)
	res := startClaim(t, w)
	code := claimCode(t, res)
	before := len(cell.argsList())

	got, err := w.pollTelegramClaim(context.Background(), tgTenant, res.SetupKey)
	if err != nil || got.State != "pending" || got.SetupKey != res.SetupKey || got.SetupURL != res.SetupURL {
		t.Fatalf("empty poll = %+v, %v", got, err)
	}
	tg.push(10, "private", 42, 42, "/start WRONGCODE1", "en")     // stranger, wrong code
	tg.push(11, "group", -100, tgOwner, "/start "+code, "en")     // right code, but in a group
	tg.push(12, "private", 43, 44, "/start "+code, "en")          // chat/from mismatch
	tg.push(13, "private", tgOwner, tgOwner, "hello there", "en") // owner, no code
	got, err = w.pollTelegramClaim(context.Background(), tgTenant, res.SetupKey)
	if err != nil || got.State != "pending" {
		t.Fatalf("poll = %+v, %v", got, err)
	}
	if len(cell.argsList()) != before {
		t.Fatalf("no-match poll touched the cell: %q", cell.argsList()[before:])
	}
	got, _ = w.pollTelegramClaim(context.Background(), tgTenant, res.SetupKey)
	if got.State != "pending" || tg.offsets[len(tg.offsets)-1] != 14 {
		t.Fatalf("offset not advanced: %v (%+v)", tg.offsets, got)
	}
}

func TestTelegramClaimBindsOwnerInOneWrite(t *testing.T) {
	w, tg, cell, dir := newClaimWorker(t, `{"all":true}`)
	cell.commands = `{"native":"auto","ownerAllowFrom":["openclaw-weixin:wx1"]}`
	res := startClaim(t, w)
	code := claimCode(t, res)
	tg.push(20, "private", 42, 42, "hi", "en")
	tg.push(21, "private", tgOwner, tgOwner, "/start "+code, "en")

	got, err := w.pollTelegramClaim(context.Background(), tgTenant, res.SetupKey)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "connected" || !strings.Contains(got.Message, "Ada @ada_example") || !strings.Contains(got.Message, "@testbugbot") {
		t.Fatalf("result = %+v", got)
	}
	// The ack (offset 22) must come before any cell write.
	if last := tg.offsets[len(tg.offsets)-1]; last != 22 {
		t.Fatalf("getUpdates offsets = %v, want ack at 22", tg.offsets)
	}
	calls := cell.argsList()[2:] // after plugin install/enable
	want := []string{
		tgTenant + " openclaw config get commands --json",
		tgTenant + " openclaw config patch --stdin --dry-run",
		tgTenant + " openclaw config patch --stdin",
		tgTenant + " openclaw channels status --channel telegram --probe --json",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("cell calls = %q", calls)
	}
	patches := cell.patches()
	if len(patches) != 1 {
		t.Fatalf("patches = %d", len(patches))
	}
	var patch struct {
		Channels struct {
			Telegram map[string]json.RawMessage `json:"telegram"`
		} `json:"channels"`
		Commands struct {
			OwnerAllowFrom []string `json:"ownerAllowFrom"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(patches[0], &patch); err != nil {
		t.Fatal(err)
	}
	var top struct {
		Enabled        bool     `json:"enabled"`
		DMPolicy       string   `json:"dmPolicy"`
		AllowFrom      []string `json:"allowFrom"`
		GroupPolicy    string   `json:"groupPolicy"`
		GroupAllowFrom []string `json:"groupAllowFrom"`
		Accounts       map[string]struct {
			Name           string   `json:"name"`
			Enabled        bool     `json:"enabled"`
			BotToken       string   `json:"botToken"`
			DMPolicy       string   `json:"dmPolicy"`
			AllowFrom      []string `json:"allowFrom"`
			GroupPolicy    string   `json:"groupPolicy"`
			GroupAllowFrom []string `json:"groupAllowFrom"`
		} `json:"accounts"`
	}
	raw, _ := json.Marshal(patch.Channels.Telegram)
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	id := "4242424242"
	main := top.Accounts["main"]
	if !top.Enabled || top.DMPolicy != "allowlist" || top.GroupPolicy != "allowlist" || strings.Join(top.AllowFrom, ",") != id || strings.Join(top.GroupAllowFrom, ",") != id {
		t.Fatalf("channels.telegram = %+v", top)
	}
	if main.Name != "Telegram" || !main.Enabled || main.BotToken != tgToken || main.DMPolicy != "allowlist" || main.GroupPolicy != "allowlist" ||
		strings.Join(main.AllowFrom, ",") != id || strings.Join(main.GroupAllowFrom, ",") != id {
		t.Fatalf("accounts.main = %+v", main)
	}
	if strings.Join(patch.Commands.OwnerAllowFrom, ",") != "openclaw-weixin:wx1,telegram:"+id {
		t.Fatalf("ownerAllowFrom = %v", patch.Commands.OwnerAllowFrom)
	}
	if tg.count("sendMessage") != 0 {
		t.Fatal("confirmation sent although confirm_message is off")
	}
	// Repeated polls answer from memory; the token is gone.
	n := len(cell.argsList())
	again, err := w.pollTelegramClaim(context.Background(), tgTenant, res.SetupKey)
	if err != nil || again.State != "connected" || len(cell.argsList()) != n {
		t.Fatalf("repeat poll = %+v, %v", again, err)
	}
	if claim := w.telegramClaims[res.SetupKey]; claim == nil || claim.token != "" {
		t.Fatal("token kept after binding")
	}
	receipt, err := os.ReadFile(filepath.Join(dir, "telegram-owner-claims.jsonl"))
	if err != nil || !strings.Contains(string(receipt), `"owner_id":4242424242`) || strings.Contains(string(receipt), tgToken) {
		t.Fatalf("receipt = %q, %v", receipt, err)
	}
}

func TestTelegramClaimTypedCodeAndConfirmation(t *testing.T) {
	w, tg, _, _ := newClaimWorker(t, `{"all":true,"confirm_message":true}`)
	res := startClaim(t, w)
	code := claimCode(t, res)
	tg.push(30, "private", tgOwner, tgOwner, " "+strings.ToLower(code[:5]+"-"+code[5:])+" ", "zh-hans")
	got, err := w.pollTelegramClaim(context.Background(), tgTenant, res.SetupKey)
	if err != nil || got.State != "connected" {
		t.Fatalf("result = %+v, %v", got, err)
	}
	if len(tg.sent) != 1 || tg.sent[0]["text"] != telegramConfirmZH || int64(tg.sent[0]["chat_id"].(float64)) != tgOwner {
		t.Fatalf("sent = %v", tg.sent)
	}
}

func TestTelegramClaimOwnerAllowFromHandling(t *testing.T) {
	for _, tc := range []struct {
		name, commands string
		wantCommands   bool
	}{
		{"unreadable commands are left alone", "", false},
		{"already the owner", `{"ownerAllowFrom":["telegram:4242424242"]}`, false},
		{"empty list gets the owner", `{"native":"auto"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, tg, cell, _ := newClaimWorker(t, `{"all":true}`)
			cell.commands = tc.commands
			res := startClaim(t, w)
			tg.push(40, "private", tgOwner, tgOwner, "/start "+claimCode(t, res), "en")
			if got, err := w.pollTelegramClaim(context.Background(), tgTenant, res.SetupKey); err != nil || got.State != "connected" {
				t.Fatalf("result = %+v, %v", got, err)
			}
			var patch map[string]json.RawMessage
			_ = json.Unmarshal(cell.patches()[0], &patch)
			if _, has := patch["commands"]; has != tc.wantCommands {
				t.Fatalf("commands in patch = %v, want %v: %s", has, tc.wantCommands, cell.patches()[0])
			}
		})
	}
}

func TestTelegramClaimExpiredUnknownAndCrossTenant(t *testing.T) {
	w, _, _, _ := newClaimWorker(t, `{"all":true}`)
	now := time.Now()
	w.now = func() time.Time { return now }
	res := startClaim(t, w)
	for _, tc := range []struct{ tenant, key string }{
		{"u36-0123456789ab", res.SetupKey},
		{tgTenant, "tgclaim_" + strings.Repeat("0", 32)},
	} {
		got, err := w.pollTelegramClaim(context.Background(), tc.tenant, tc.key)
		if err != nil || got.State != "pending" || got.SetupKey != "" || got.Message != telegramMsgExpired {
			t.Fatalf("poll %+v = %+v, %v", tc, got, err)
		}
	}
	now = now.Add(telegramClaimTTL + time.Second)
	got, err := w.pollTelegramClaim(context.Background(), tgTenant, res.SetupKey)
	if err != nil || got.SetupKey != "" || got.Message != telegramMsgExpired {
		t.Fatalf("expired poll = %+v, %v", got, err)
	}
}

func TestTelegramClaimConflictAndRevokedToken(t *testing.T) {
	w, tg, _, _ := newClaimWorker(t, `{"all":true}`)
	res := startClaim(t, w)
	tg.updatesCode = http.StatusConflict
	got, err := w.pollTelegramClaim(context.Background(), tgTenant, res.SetupKey)
	if err != nil || got.State != "pending" || got.SetupKey != res.SetupKey || got.Message != telegramMsgConflict {
		t.Fatalf("conflict poll = %+v, %v", got, err)
	}
	tg.updatesCode = http.StatusUnauthorized
	got, err = w.pollTelegramClaim(context.Background(), tgTenant, res.SetupKey)
	if err != nil || got.SetupKey != "" || got.Message != telegramMsgRevoked {
		t.Fatalf("revoked poll = %+v, %v", got, err)
	}
	if w.lookupTelegramClaim(tgTenant, res.SetupKey) != nil {
		t.Fatal("revoked claim kept")
	}
}

func TestTelegramClaimNewStartReplacesOldClaim(t *testing.T) {
	w, _, _, _ := newClaimWorker(t, `{"all":true}`)
	first := startClaim(t, w)
	second := startClaim(t, w)
	if first.SetupKey == second.SetupKey || w.lookupTelegramClaim(tgTenant, first.SetupKey) != nil || w.lookupTelegramClaim(tgTenant, second.SetupKey) == nil {
		t.Fatal("old claim of the same tenant not replaced")
	}
}

func TestTelegramClaimProbeAndWriteRetries(t *testing.T) {
	w, tg, cell, _ := newClaimWorker(t, `{"all":true}`)
	res := startClaim(t, w)
	tg.push(50, "private", tgOwner, tgOwner, "/start "+claimCode(t, res), "en")

	cell.patchErr = true
	if _, err := w.pollTelegramClaim(context.Background(), tgTenant, res.SetupKey); err == nil {
		t.Fatal("rejected patch reported success")
	}
	cell.patchErr = false
	cell.probeOK = false
	got, err := w.pollTelegramClaim(context.Background(), tgTenant, res.SetupKey)
	if err != nil || got.State != "pending" || got.Message != telegramMsgFinishing {
		t.Fatalf("unverified poll = %+v, %v", got, err)
	}
	if len(cell.patches()) != 1 {
		t.Fatalf("patches = %d, want 1 (the failed dry run must not count)", len(cell.patches()))
	}
	cell.probeOK = true
	got, err = w.pollTelegramClaim(context.Background(), tgTenant, res.SetupKey)
	if err != nil || got.State != "connected" || len(cell.patches()) != 1 {
		t.Fatalf("final poll = %+v, %v, patches=%d", got, err, len(cell.patches()))
	}
}

func TestTelegramClaimWaitsForWorkerLock(t *testing.T) {
	w, tg, cell, _ := newClaimWorker(t, `{"all":true}`)
	res := startClaim(t, w)
	tg.push(60, "private", tgOwner, tgOwner, "/start "+claimCode(t, res), "en")
	w.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	got, err := w.pollTelegramClaim(ctx, tgTenant, res.SetupKey)
	cancel()
	if err != nil || got.State != "pending" || got.Message != telegramMsgFinishing || len(cell.patches()) != 0 {
		t.Fatalf("locked poll = %+v, %v", got, err)
	}
	w.mu.Unlock()
	got, err = w.pollTelegramClaim(context.Background(), tgTenant, res.SetupKey)
	if err != nil || got.State != "connected" {
		t.Fatalf("poll after unlock = %+v, %v", got, err)
	}
}

func TestServeHTTPRoutesClaimPollBeforeWorkerLock(t *testing.T) {
	w, _, _, _ := newClaimWorker(t, `{"all":true}`)
	w.Token = strings.Repeat("t", 40)
	res := startClaim(t, w)
	w.mu.Lock() // another operation is running
	defer w.mu.Unlock()
	body := strings.NewReader(`{"credential":"` + res.SetupKey + `","account":"main","name":"Telegram"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/cells/"+tgTenant+"/connections/telegram", body)
	req.Header.Set("Authorization", "Bearer "+w.Token)
	rec := httptest.NewRecorder()
	w.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got Result
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.State != "pending" || got.SetupKey != res.SetupKey {
		t.Fatalf("result = %+v, %v", got, err)
	}
}

func TestTelegramClaimCodeParsing(t *testing.T) {
	for in, want := range map[string]string{
		"/start ABCDEFGHJK":             "ABCDEFGHJK",
		"/start@testbugbot abcde-fghjk": "ABCDEFGHJK",
		"  abcde fghjk ":                "ABCDEFGHJK",
		"/start":                        "",
		"/start@testbugbot":             "",
	} {
		if got := telegramClaimCodeFrom(in); got != want {
			t.Fatalf("telegramClaimCodeFrom(%q) = %q, want %q", in, got, want)
		}
	}
	if isTelegramClaimKey(tgToken) || isTelegramClaimKey("tgclaim_xyz") || !isTelegramClaimKey("tgclaim_"+strings.Repeat("a", 32)) {
		t.Fatal("claim key detection")
	}
}
