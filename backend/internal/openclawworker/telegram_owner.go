package openclawworker

// Telegram owner claim. Without an owner, OpenClaw runs a new Telegram account
// in dmPolicy "pairing" and answers the customer with a hard-coded "access not
// configured … Pairing code" message. With the claim enabled, a customer's bot
// is attached to the cell only together with its owner:
//
//  1. The token arrives (console → control plane → connect). The worker checks it
//     with getMe, prepares the plugin, keeps the token in memory for at most
//     claimTTL and returns "pending" with a t.me/<bot>?start=<code> link.
//  2. The console polls with the returned setup key. The worker reads the bot's
//     updates until a private chat sends /start <code> (or just the code).
//  3. One config patch writes the token and the owner allowlist together, the same
//     shape as the hand-configured cells, so the channel's first start is already
//     in allowlist mode.
//
// The behaviour is gated per tenant by a JSON file read on every request; a
// missing or invalid file keeps the old connect path.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	telegramClaimKeyPrefix = "tgclaim_"
	telegramClaimTTL       = 15 * time.Minute
	telegramClaimDoneTTL   = 10 * time.Minute
	telegramClaimMax       = 64
	telegramCodeAlphabet   = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	telegramCodeLength     = 10
	telegramDefaultAPIBase = "https://api.telegram.org"
	telegramLockWait       = 45 * time.Second // the console times a call out at 90 s
)

var (
	errTelegramUnreachable = errors.New("telegram api unreachable")
	errTelegramClaimLocked = errors.New("worker busy")
)

const (
	telegramMsgExpired   = "这次连接已过期，请重新粘贴机器人令牌再连接一次。This connection attempt expired. Paste the bot token and connect again."
	telegramMsgConflict  = "这个机器人正被另一个程序读取消息。请先停用那个程序，或用 BotFather 新建一个机器人，再连接一次。This bot is being read by another program. Stop that program, or create a new bot with BotFather, then connect again."
	telegramMsgRevoked   = "机器人令牌已失效，请在 BotFather 获取新令牌后重新连接。The bot token is no longer valid. Get a new token from BotFather and connect again."
	telegramMsgFinishing = "已收到，正在完成设置，请稍候…Got it, finishing setup…"
	telegramConfirmZH    = "✅ 已连接。现在只有你能和我对话，直接发消息就可以开始。"
	telegramConfirmEN    = "✅ Connected. Only you can talk to me now. Just send a message to start."
)

// telegramClaimPolicy is the operator switch, e.g. {"all":false,"tenants":["u35-0123456789ab"],"confirm_message":true}.
type telegramClaimPolicy struct {
	All            bool     `json:"all"`
	Tenants        []string `json:"tenants"`
	ConfirmMessage bool     `json:"confirm_message"`
}

type telegramUser struct {
	ID           int64  `json:"id"`
	IsBot        bool   `json:"is_bot"`
	FirstName    string `json:"first_name"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code"`
}

type telegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string        `json:"text"`
		From *telegramUser `json:"from"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
	} `json:"message"`
}

type telegramClaim struct {
	mu      sync.Mutex // serializes polls of one claim
	key     string
	tenant  string
	account string
	name    string
	token   string // cleared once the cell holds it
	code    string
	bot     string
	expires time.Time
	offset  int64         // next getUpdates offset
	owner   *telegramUser // set once the code arrived
	written bool          // token + allowlist are in the cell
	done    bool
	result  Result
}

func (w *Worker) clock() time.Time {
	if w.now != nil {
		return w.now()
	}
	return time.Now()
}

func (w *Worker) runCell(ctx context.Context, tenant string, input []byte, args ...string) ([]byte, error) {
	if w.cellRun != nil {
		return w.cellRun(ctx, tenant, input, args...)
	}
	if input == nil {
		return w.cellCommand(ctx, tenant, args...)
	}
	return w.cellCommandInput(ctx, tenant, input, args...)
}

func (w *Worker) telegramPolicyPath() string {
	if w.claimPolicyPath != "" {
		return w.claimPolicyPath
	}
	if value := strings.TrimSpace(os.Getenv("OPENCLAW_TELEGRAM_OWNER_CLAIM_FILE")); value != "" {
		return value
	}
	return filepath.Join(os.Getenv("HOME"), ".openclaw", "fleet", "telegram-owner-claim.json")
}

// telegramPolicy reads the switch on every call. Any read or parse problem means "off".
func (w *Worker) telegramPolicy() (telegramClaimPolicy, bool) {
	file, err := os.Open(w.telegramPolicyPath())
	if err != nil {
		return telegramClaimPolicy{}, false
	}
	defer func() { _ = file.Close() }()
	var policy telegramClaimPolicy
	if json.NewDecoder(io.LimitReader(file, 64<<10)).Decode(&policy) != nil {
		return telegramClaimPolicy{}, false
	}
	return policy, true
}

func (w *Worker) telegramClaimEnabled(tenant string) bool {
	policy, ok := w.telegramPolicy()
	return ok && (policy.All || slices.Contains(policy.Tenants, tenant))
}

func isTelegramClaimKey(credential string) bool {
	key := strings.TrimSpace(credential)
	if !strings.HasPrefix(key, telegramClaimKeyPrefix) || len(key) != len(telegramClaimKeyPrefix)+32 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(key, telegramClaimKeyPrefix))
	return err == nil
}

func newTelegramClaimCode() (string, error) {
	buf := make([]byte, telegramCodeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, telegramCodeLength)
	for i, b := range buf {
		out[i] = telegramCodeAlphabet[int(b)%len(telegramCodeAlphabet)] // 256 is a multiple of 32: unbiased
	}
	return string(out), nil
}

func newTelegramClaimKey() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return telegramClaimKeyPrefix + hex.EncodeToString(buf), nil
}

func displayTelegramCode(code string) string {
	if len(code) != telegramCodeLength {
		return code
	}
	return code[:5] + "-" + code[5:]
}

// telegramClaimCodeFrom extracts a claim code from "/start CODE", "/start@bot CODE" or a typed "abcde-fghjk".
func telegramClaimCodeFrom(text string) string {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "/start") {
		rest := strings.TrimPrefix(t, "/start")
		if strings.HasPrefix(rest, "@") {
			if i := strings.IndexAny(rest, " \t\r\n"); i >= 0 {
				rest = rest[i:]
			} else {
				rest = ""
			}
		}
		t = strings.TrimSpace(rest)
	}
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "", "_", "").Replace(t))
}

func telegramOwnerLabel(owner *telegramUser) string {
	if owner == nil {
		return ""
	}
	label := strings.TrimSpace(owner.FirstName)
	if owner.Username != "" {
		if label != "" {
			label += " "
		}
		label += "@" + owner.Username
	}
	if label == "" {
		label = strconv.FormatInt(owner.ID, 10)
	}
	return label
}

// telegramCall calls one Bot API method. The returned error never contains the
// request URL, because that URL carries the bot token.
func (w *Worker) telegramCall(ctx context.Context, token, method string, params map[string]any, out any) (int, error) {
	base := w.telegramAPIBase
	if base == "" {
		base = telegramDefaultAPIBase
	}
	if params == nil {
		params = map[string]any{}
	}
	body, err := json.Marshal(params)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/bot"+token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return 0, errTelegramUnreachable
	}
	req.Header.Set("Content-Type", "application/json")
	client := w.telegramHTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, errTelegramUnreachable
	}
	defer func() { _ = resp.Body.Close() }()
	var envelope struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&envelope); err != nil {
		return resp.StatusCode, fmt.Errorf("telegram %s: unreadable response (http %d)", method, resp.StatusCode)
	}
	if !envelope.OK {
		code := envelope.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return code, fmt.Errorf("telegram %s failed with code %d", method, code)
	}
	if out != nil && json.Unmarshal(envelope.Result, out) != nil {
		return resp.StatusCode, fmt.Errorf("telegram %s: unexpected result", method)
	}
	return resp.StatusCode, nil
}

// startTelegramClaim is phase 1: validate the token, prepare the plugin and hand back the Start link.
// The channel itself is not added yet. The caller holds w.mu.
func (w *Worker) startTelegramClaim(ctx context.Context, tenant, token, account, name string) (Result, error) {
	token = strings.TrimSpace(token)
	if len(token) < 20 || strings.ContainsAny(token, " \t\r\n/?#") {
		return Result{}, errors.New("invalid telegram credential")
	}
	if account == "" {
		account = "main"
	}
	if name == "" {
		name = "Telegram"
	}
	var bot telegramUser
	if _, err := w.telegramCall(ctx, token, "getMe", nil, &bot); err != nil || !bot.IsBot || bot.Username == "" {
		return Result{}, errors.New("invalid telegram credential")
	}
	// OpenClaw's polling session makes the same call when it starts; getUpdates fails while a webhook is set.
	if _, err := w.telegramCall(ctx, token, "deleteWebhook", map[string]any{"drop_pending_updates": false}, nil); err != nil {
		return Result{}, err
	}
	if _, err := w.runCell(ctx, tenant, nil, "openclaw", "plugins", "install", "telegram", "--accept-capabilities", "--acknowledge-install-policy-warning"); err != nil {
		return Result{}, err
	}
	if _, err := w.runCell(ctx, tenant, nil, "openclaw", "plugins", "enable", "telegram"); err != nil {
		return Result{}, err
	}
	code, err := newTelegramClaimCode()
	if err != nil {
		return Result{}, err
	}
	key, err := newTelegramClaimKey()
	if err != nil {
		return Result{}, err
	}
	claim := &telegramClaim{key: key, tenant: tenant, account: account, name: name, token: token, code: code, bot: bot.Username, expires: w.clock().Add(telegramClaimTTL)}
	w.telegramMu.Lock()
	if w.telegramClaims == nil {
		w.telegramClaims = map[string]*telegramClaim{}
	}
	now := w.clock()
	for k, c := range w.telegramClaims {
		if c.tenant == tenant || now.After(c.expires) {
			delete(w.telegramClaims, k)
		}
	}
	if len(w.telegramClaims) >= telegramClaimMax {
		w.telegramMu.Unlock()
		return Result{}, errors.New("too many pending telegram connections")
	}
	w.telegramClaims[key] = claim
	w.telegramMu.Unlock()
	return w.telegramPendingResult(claim, ""), nil
}

func (w *Worker) telegramPendingResult(claim *telegramClaim, message string) Result {
	if message == "" {
		shown := displayTelegramCode(claim.code)
		message = fmt.Sprintf("在 Telegram 打开 @%s 并点「开始」(Start)，或把代码 %s 发给它。Open @%s in Telegram and tap Start, or send it the code %s.", claim.bot, shown, claim.bot, shown)
	}
	return Result{State: "pending", SetupURL: "https://t.me/" + claim.bot + "?start=" + claim.code, SetupKey: claim.key, Message: message}
}

func (w *Worker) lookupTelegramClaim(tenant, key string) *telegramClaim {
	w.telegramMu.Lock()
	defer w.telegramMu.Unlock()
	claim := w.telegramClaims[key]
	if claim == nil || claim.tenant != tenant {
		return nil
	}
	if w.clock().After(claim.expires) {
		delete(w.telegramClaims, key)
		return nil
	}
	return claim
}

func (w *Worker) dropTelegramClaim(key string) {
	w.telegramMu.Lock()
	delete(w.telegramClaims, key)
	w.telegramMu.Unlock()
}

// pollTelegramClaim is phase 2. It is called without w.mu; it takes w.mu only to write the cell.
func (w *Worker) pollTelegramClaim(ctx context.Context, tenant, key string) (Result, error) {
	claim := w.lookupTelegramClaim(tenant, strings.TrimSpace(key))
	if claim == nil {
		return Result{State: "pending", Message: telegramMsgExpired}, nil
	}
	claim.mu.Lock()
	defer claim.mu.Unlock()
	if claim.done {
		return claim.result, nil
	}
	if claim.owner == nil {
		var updates []telegramUpdate
		code, err := w.telegramCall(ctx, claim.token, "getUpdates", map[string]any{"offset": claim.offset, "timeout": 0, "limit": 100}, &updates)
		switch {
		case code == http.StatusConflict:
			return w.telegramPendingResult(claim, telegramMsgConflict), nil
		case code == http.StatusUnauthorized || code == http.StatusNotFound:
			w.dropTelegramClaim(claim.key)
			return Result{State: "pending", Message: telegramMsgRevoked}, nil
		case err != nil:
			return w.telegramPendingResult(claim, ""), nil // transient; the next poll retries
		}
		var match *telegramUpdate
		for i := range updates {
			u := &updates[i]
			if u.UpdateID >= claim.offset {
				claim.offset = u.UpdateID + 1
			}
			m := u.Message
			if match != nil || m == nil || m.From == nil || m.From.IsBot || m.Chat.Type != "private" || m.Chat.ID != m.From.ID {
				continue
			}
			if got := telegramClaimCodeFrom(m.Text); len(got) == len(claim.code) && subtle.ConstantTimeCompare([]byte(got), []byte(claim.code)) == 1 {
				match = u
			}
		}
		if match == nil {
			return w.telegramPendingResult(claim, ""), nil
		}
		// Confirm everything read so far before OpenClaw starts polling, so /start is not replayed to the agent.
		_, _ = w.telegramCall(ctx, claim.token, "getUpdates", map[string]any{"offset": claim.offset, "timeout": 0, "limit": 1}, nil)
		owner := *match.Message.From
		claim.owner = &owner
	}
	if !claim.written {
		if err := w.lockForTelegramWrite(ctx); err != nil {
			return w.telegramPendingResult(claim, telegramMsgFinishing), nil
		}
		err := w.writeTelegramOwner(ctx, claim)
		w.mu.Unlock()
		if err != nil {
			return Result{}, err
		}
		claim.written = true
	}
	if err := w.probeTelegram(ctx, claim.tenant, claim.account); err != nil {
		return w.telegramPendingResult(claim, telegramMsgFinishing), nil
	}
	label := telegramOwnerLabel(claim.owner)
	claim.result = Result{State: "connected", Message: fmt.Sprintf("已连接：@%s 现在只回复 %s。Connected: @%s now answers only %s.", claim.bot, label, claim.bot, label)}
	claim.done = true
	if policy, ok := w.telegramPolicy(); ok && policy.ConfirmMessage {
		text := telegramConfirmEN
		if strings.HasPrefix(strings.ToLower(claim.owner.LanguageCode), "zh") {
			text = telegramConfirmZH
		}
		_, _ = w.telegramCall(ctx, claim.token, "sendMessage", map[string]any{"chat_id": claim.owner.ID, "text": text}, nil)
	}
	w.recordTelegramOwner(claim)
	claim.token = ""
	w.telegramMu.Lock() // expires is read under telegramMu by lookups and sweeps
	claim.expires = w.clock().Add(telegramClaimDoneTTL)
	w.telegramMu.Unlock()
	return claim.result, nil
}

// lockForTelegramWrite takes w.mu, waiting for other worker operations to finish.
func (w *Worker) lockForTelegramWrite(ctx context.Context) error {
	deadline := time.Now().Add(telegramLockWait)
	for {
		if w.mu.TryLock() {
			return nil
		}
		if time.Now().After(deadline) {
			return errTelegramClaimLocked
		}
		select {
		case <-ctx.Done():
			return errTelegramClaimLocked
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// writeTelegramOwner writes the token and the owner allowlist in one validated config patch.
// It never overwrites an existing commands.ownerAllowFrom it could not read.
func (w *Worker) writeTelegramOwner(ctx context.Context, claim *telegramClaim) error {
	ownerID := strconv.FormatInt(claim.owner.ID, 10)
	allow := []string{ownerID}
	policy := map[string]any{"dmPolicy": "allowlist", "allowFrom": allow, "groupPolicy": "allowlist", "groupAllowFrom": allow}
	account := map[string]any{"name": claim.name, "enabled": true, "botToken": claim.token}
	telegram := map[string]any{"enabled": true, "accounts": map[string]any{claim.account: account}}
	for k, v := range policy {
		account[k] = v
		telegram[k] = v
	}
	patch := map[string]any{"channels": map[string]any{"telegram": telegram}}
	if owners, ok := w.readOwnerAllowFrom(ctx, claim.tenant); ok {
		entry := "telegram:" + ownerID
		if !slices.Contains(owners, entry) {
			patch["commands"] = map[string]any{"ownerAllowFrom": append(owners, entry)}
		}
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	if _, err := w.runCell(ctx, claim.tenant, body, "openclaw", "config", "patch", "--stdin", "--dry-run"); err != nil {
		return errors.New("telegram owner config rejected")
	}
	if _, err := w.runCell(ctx, claim.tenant, body, "openclaw", "config", "patch", "--stdin"); err != nil {
		return errors.New("telegram owner config write failed")
	}
	return nil
}

func (w *Worker) readOwnerAllowFrom(ctx context.Context, tenant string) ([]string, bool) {
	out, err := w.runCell(ctx, tenant, nil, "openclaw", "config", "get", "commands", "--json")
	if err != nil {
		return nil, false
	}
	var commands struct {
		OwnerAllowFrom []string `json:"ownerAllowFrom"`
	}
	if json.Unmarshal(out, &commands) != nil {
		return nil, false
	}
	return commands.OwnerAllowFrom, true
}

// probeTelegram waits for the hot reload and checks that OpenClaw accepted the token.
func (w *Worker) probeTelegram(ctx context.Context, tenant, account string) error {
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(w.probeGap()):
			}
		}
		out, err := w.runCell(ctx, tenant, nil, "openclaw", "channels", "status", "--channel", "telegram", "--probe", "--json")
		if err != nil {
			continue
		}
		var status struct {
			ChannelAccounts map[string][]struct {
				AccountID string `json:"accountId"`
				Probe     *struct {
					OK bool `json:"ok"`
				} `json:"probe"`
			} `json:"channelAccounts"`
		}
		if json.Unmarshal(out, &status) != nil {
			continue
		}
		for _, state := range status.ChannelAccounts["telegram"] {
			if state.AccountID == account && state.Probe != nil && state.Probe.OK {
				return nil
			}
		}
	}
	return errors.New("telegram channel not verified yet")
}

func (w *Worker) probeGap() time.Duration {
	if w.telegramProbeGap > 0 {
		return w.telegramProbeGap
	}
	return 2 * time.Second
}

// recordTelegramOwner appends a receipt (no token) next to the policy file. Best effort.
func (w *Worker) recordTelegramOwner(claim *telegramClaim) {
	line, err := json.Marshal(map[string]any{
		"at": w.clock().UTC().Format(time.RFC3339), "event": "bound", "tenant": claim.tenant,
		"account": claim.account, "bot": claim.bot, "owner_id": claim.owner.ID,
	})
	if err != nil {
		return
	}
	path := filepath.Join(filepath.Dir(w.telegramPolicyPath()), "telegram-owner-claims.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- Operator-configured directory.
	if err != nil {
		return
	}
	_, _ = file.Write(append(line, '\n'))
	_ = file.Close()
}
