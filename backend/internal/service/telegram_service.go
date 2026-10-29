package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/Wei-Shaw/sub2api/internal/pkg/telegram"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	settingKeyTelegramBotConfig     = "telegram_bot_config"
	settingKeyTelegramUpdateOffset  = "telegram_bot_update_offset"
	settingKeyTelegramReportLastDay = "telegram_bot_daily_report_last_date"

	// TelegramBroadcastMaxRunes keeps broadcasts to a short notice.
	TelegramBroadcastMaxRunes = 200

	defaultTelegramDailyReportTime = "23:55"

	telegramConfigCacheTTL    = 30 * time.Second
	telegramSendTimeout       = 15 * time.Second
	telegramMaxRetryAfter     = 30 * time.Second
	telegramBroadcastInterval = 40 * time.Millisecond // stays under Telegram's ~30 msg/s limit
)

var (
	ErrTelegramNotConfigured = infraerrors.BadRequest("TELEGRAM_NOT_CONFIGURED", "telegram bot is not enabled or has no token")
	ErrTelegramInvalidToken  = infraerrors.BadRequest("TELEGRAM_INVALID_TOKEN", "telegram rejected this bot token")
	ErrTelegramUnreachable   = infraerrors.ServiceUnavailable("TELEGRAM_UNREACHABLE", "could not reach Telegram; check the network or proxy setting")
	ErrTelegramInvalidTime   = infraerrors.BadRequest("TELEGRAM_INVALID_REPORT_TIME", "daily report time must use HH:MM")
	ErrTelegramInvalidProxy  = infraerrors.BadRequest("TELEGRAM_INVALID_PROXY", "proxy URL must be http, https, socks5 or socks5h")
	ErrTelegramInvalidLink   = infraerrors.BadRequest("TELEGRAM_INVALID_LINK", "channel and group links must start with https://")
	ErrTelegramBroadcastText = infraerrors.BadRequest("TELEGRAM_BROADCAST_TEXT", fmt.Sprintf("notice must be 1-%d characters", TelegramBroadcastMaxRunes))
	ErrTelegramBroadcastBusy = infraerrors.Conflict("TELEGRAM_BROADCAST_IN_PROGRESS", "a notice is still being sent")
	ErrTelegramEncryptionKey = infraerrors.BadRequest(
		"SECRET_ENCRYPTION_KEY_NOT_CONFIGURED",
		"cannot store the bot token: no fixed secret encryption key is configured. Set TOTP_ENCRYPTION_KEY (for example `openssl rand -hex 32`) and try again",
	)

	ErrTelegramBindCodeInvalid = errors.New("telegram bind code is invalid")
	// ErrTelegramLinkedElsewhere means the account is already linked to a
	// different Telegram chat; it must be unlinked first.
	ErrTelegramLinkedElsewhere = errors.New("account is already linked to another telegram chat")

	telegramBindCodePattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

// TelegramBotConfig is persisted as JSON under settingKeyTelegramBotConfig.
type TelegramBotConfig struct {
	Enabled            bool   `json:"enabled"`
	BotTokenEncrypted  string `json:"bot_token_encrypted,omitempty"`
	BotUsername        string `json:"bot_username"`
	ProxyURL           string `json:"proxy_url"`
	NotifyBalanceLow   bool   `json:"notify_balance_low"`
	NotifyAdminAlerts  bool   `json:"notify_admin_alerts"`
	DailyReportEnabled bool   `json:"daily_report_enabled"`
	DailyReportTime    string `json:"daily_report_time"`
	NewsChannelURL     string `json:"news_channel_url"`
	GroupInviteURL     string `json:"group_invite_url"`
}

func defaultTelegramBotConfig() TelegramBotConfig {
	return TelegramBotConfig{NotifyBalanceLow: true, NotifyAdminAlerts: true, DailyReportTime: defaultTelegramDailyReportTime}
}

// TelegramBotSettings is the admin view. It never contains the token.
type TelegramBotSettings struct {
	Enabled            bool       `json:"enabled"`
	TokenConfigured    bool       `json:"token_configured"`
	BotUsername        string     `json:"bot_username"`
	ProxyURL           string     `json:"proxy_url"`
	NotifyBalanceLow   bool       `json:"notify_balance_low"`
	NotifyAdminAlerts  bool       `json:"notify_admin_alerts"`
	DailyReportEnabled bool       `json:"daily_report_enabled"`
	DailyReportTime    string     `json:"daily_report_time"`
	NewsChannelURL     string     `json:"news_channel_url"`
	GroupInviteURL     string     `json:"group_invite_url"`
	Timezone           string     `json:"timezone"`
	LinkedUsers        int64      `json:"linked_users"`
	LinkedAdmins       int64      `json:"linked_admins"`
	LastError          string     `json:"last_error,omitempty"`
	LastPollAt         *time.Time `json:"last_poll_at,omitempty"`
	BroadcastMaxLength int        `json:"broadcast_max_length"`
}

// TelegramBotSettingsInput updates the settings. An empty BotToken keeps the stored one.
type TelegramBotSettingsInput struct {
	Enabled            bool   `json:"enabled"`
	BotToken           string `json:"bot_token"`
	ProxyURL           string `json:"proxy_url"`
	NotifyBalanceLow   bool   `json:"notify_balance_low"`
	NotifyAdminAlerts  bool   `json:"notify_admin_alerts"`
	DailyReportEnabled bool   `json:"daily_report_enabled"`
	DailyReportTime    string `json:"daily_report_time"`
	NewsChannelURL     string `json:"news_channel_url"`
	GroupInviteURL     string `json:"group_invite_url"`
}

// TelegramLinkStatus is the user's view. BindCode is only set while the
// account is not linked.
type TelegramLinkStatus struct {
	Available        bool       `json:"available"`
	BotUsername      string     `json:"bot_username,omitempty"`
	Linked           bool       `json:"linked"`
	TelegramUsername string     `json:"telegram_username,omitempty"`
	LinkedAt         *time.Time `json:"linked_at,omitempty"`
	BindCode         string     `json:"bind_code,omitempty"`
}

// TelegramBinding links a user to a private Telegram chat. Every user with a
// row has a permanent BindCode; ChatID is 0 until they link a chat.
type TelegramBinding struct {
	UserID           int64
	BindCode         string
	ChatID           int64
	TelegramUsername string
	Language         string
	LinkedAt         *time.Time
}

// TelegramChatTarget is a linked chat of an active user.
type TelegramChatTarget struct {
	UserID   int64
	ChatID   int64
	Language string
}

type TelegramBindingRepository interface {
	// GetByUserID returns nil, nil when the user has no row.
	GetByUserID(ctx context.Context, userID int64) (*TelegramBinding, error)
	// GetByChatID returns nil, nil when the chat is not linked.
	GetByChatID(ctx context.Context, chatID int64) (*TelegramBinding, error)
	// EnsureBindCode stores code for the user unless they already have one,
	// and returns the stored code.
	EnsureBindCode(ctx context.Context, userID int64, code string) (string, error)
	// ResetBindCode replaces the user's code so the old one stops working.
	ResetBindCode(ctx context.Context, userID int64, code string) error
	// BindChat links chatID to the code's owner and moves the chat off any
	// other user. It returns the owner's user ID, ErrTelegramBindCodeInvalid,
	// or ErrTelegramLinkedElsewhere when the owner uses a different chat.
	BindChat(ctx context.Context, code string, chatID int64, username, language string, now time.Time) (int64, error)
	// UnlinkUser and UnlinkChat clear the chat but keep the bind code.
	UnlinkUser(ctx context.Context, userID int64) error
	UnlinkChat(ctx context.Context, chatID int64) error
	// ListTargets returns linked chats of active, non-deleted users.
	ListTargets(ctx context.Context, adminsOnly bool) ([]TelegramChatTarget, error)
	CountLinked(ctx context.Context) (users int64, admins int64, err error)
}

type telegramRuntime struct {
	cfg   TelegramBotConfig
	bot   telegram.Bot
	ready bool // enabled and has a usable token
}

// TelegramService owns the bot settings, account links and outgoing messages.
// The polling loop and commands live in TelegramBotService.
type TelegramService struct {
	settingRepo             SettingRepository
	encryptor               SecretEncryptor
	bindings                TelegramBindingRepository
	api                     telegram.API
	encryptionKeyConfigured bool

	cacheMu  sync.Mutex
	cached   *telegramRuntime
	cachedAt time.Time

	statusMu   sync.Mutex
	lastError  string
	lastPollAt time.Time

	broadcasting atomic.Bool
	// generation changes on every settings save so the bot can resume
	// polling after it paused for a token conflict.
	generation atomic.Int64
	now        func() time.Time
}

func NewTelegramService(settingRepo SettingRepository, encryptor SecretEncryptor, bindings TelegramBindingRepository, api telegram.API, encryptionKeyConfigured bool) *TelegramService {
	return &TelegramService{
		settingRepo:             settingRepo,
		encryptor:               encryptor,
		bindings:                bindings,
		api:                     api,
		encryptionKeyConfigured: encryptionKeyConfigured,
		now:                     time.Now,
	}
}

// ─── Settings ───

func (s *TelegramService) loadConfig(ctx context.Context) (TelegramBotConfig, error) {
	raw, err := s.settingRepo.GetValue(ctx, settingKeyTelegramBotConfig)
	if errors.Is(err, ErrSettingNotFound) || (err == nil && strings.TrimSpace(raw) == "") {
		return defaultTelegramBotConfig(), nil
	}
	if err != nil {
		return TelegramBotConfig{}, err
	}
	cfg := defaultTelegramBotConfig()
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return TelegramBotConfig{}, fmt.Errorf("parse telegram bot config: %w", err)
	}
	if cfg.DailyReportTime == "" {
		cfg.DailyReportTime = defaultTelegramDailyReportTime
	}
	return cfg, nil
}

func (s *TelegramService) decryptToken(cfg TelegramBotConfig) (string, error) {
	if cfg.BotTokenEncrypted == "" {
		return "", nil
	}
	return s.encryptor.Decrypt(cfg.BotTokenEncrypted)
}

// runtime returns the cached bot runtime; it is cheap enough for hot paths.
func (s *TelegramService) runtime(ctx context.Context) telegramRuntime {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.cached != nil && s.now().Sub(s.cachedAt) < telegramConfigCacheTTL {
		return *s.cached
	}
	rt := telegramRuntime{}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		slog.Warn("telegram: load config failed", "error", err)
	} else {
		rt.cfg = cfg
		token, err := s.decryptToken(cfg)
		if err != nil {
			slog.Warn("telegram: decrypt bot token failed", "error", err)
		}
		rt.bot = telegram.Bot{Token: token, ProxyURL: cfg.ProxyURL}
		rt.ready = cfg.Enabled && token != ""
	}
	s.cached = &rt
	s.cachedAt = s.now()
	return rt
}

func (s *TelegramService) invalidateCache() {
	s.cacheMu.Lock()
	s.cached = nil
	s.cacheMu.Unlock()
}

func (s *TelegramService) GetSettings(ctx context.Context) (*TelegramBotSettings, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	users, admins, err := s.bindings.CountLinked(ctx)
	if err != nil {
		return nil, err
	}
	out := &TelegramBotSettings{
		Enabled:            cfg.Enabled,
		TokenConfigured:    cfg.BotTokenEncrypted != "",
		BotUsername:        cfg.BotUsername,
		ProxyURL:           cfg.ProxyURL,
		NotifyBalanceLow:   cfg.NotifyBalanceLow,
		NotifyAdminAlerts:  cfg.NotifyAdminAlerts,
		DailyReportEnabled: cfg.DailyReportEnabled,
		DailyReportTime:    cfg.DailyReportTime,
		NewsChannelURL:     cfg.NewsChannelURL,
		GroupInviteURL:     cfg.GroupInviteURL,
		Timezone:           timezone.Name(),
		LinkedUsers:        users,
		LinkedAdmins:       admins,
		BroadcastMaxLength: TelegramBroadcastMaxRunes,
	}
	s.statusMu.Lock()
	out.LastError = s.lastError
	if !s.lastPollAt.IsZero() {
		at := s.lastPollAt
		out.LastPollAt = &at
	}
	s.statusMu.Unlock()
	return out, nil
}

func (s *TelegramService) UpdateSettings(ctx context.Context, in TelegramBotSettingsInput) (*TelegramBotSettings, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	proxy, _, err := proxyurl.Parse(in.ProxyURL)
	if err != nil {
		return nil, ErrTelegramInvalidProxy
	}
	reportTime := strings.TrimSpace(in.DailyReportTime)
	if reportTime == "" {
		reportTime = defaultTelegramDailyReportTime
	}
	if _, err := time.Parse("15:04", reportTime); err != nil {
		return nil, ErrTelegramInvalidTime
	}
	newsURL, err := normalizeTelegramLinkURL(in.NewsChannelURL)
	if err != nil {
		return nil, err
	}
	groupURL, err := normalizeTelegramLinkURL(in.GroupInviteURL)
	if err != nil {
		return nil, err
	}

	token, err := s.decryptToken(cfg)
	if err != nil {
		token = "" // unreadable old token: the admin must enter a new one
	}
	newToken := strings.TrimSpace(in.BotToken)
	tokenChanged := newToken != "" && newToken != token
	if newToken != "" {
		token = newToken
	}
	if in.Enabled && token == "" {
		return nil, ErrTelegramNotConfigured
	}

	if token != "" && (tokenChanged || proxy != cfg.ProxyURL || cfg.BotUsername == "") {
		me, err := s.api.GetMe(ctx, telegram.Bot{Token: token, ProxyURL: proxy})
		if err != nil {
			if telegram.IsUnauthorized(err) || telegram.IsNotFound(err) {
				return nil, ErrTelegramInvalidToken
			}
			return nil, ErrTelegramUnreachable.WithCause(err)
		}
		cfg.BotUsername = me.Username
	}
	if tokenChanged {
		if !s.encryptionKeyConfigured {
			return nil, ErrTelegramEncryptionKey
		}
		encrypted, err := s.encryptor.Encrypt(token)
		if err != nil {
			return nil, fmt.Errorf("encrypt telegram token: %w", err)
		}
		cfg.BotTokenEncrypted = encrypted
		// Update IDs are per bot, so a stale offset would skip the new bot's messages.
		if err := s.settingRepo.Set(ctx, settingKeyTelegramUpdateOffset, "0"); err != nil {
			return nil, err
		}
	}

	cfg.Enabled = in.Enabled
	cfg.ProxyURL = proxy
	cfg.NotifyBalanceLow = in.NotifyBalanceLow
	cfg.NotifyAdminAlerts = in.NotifyAdminAlerts
	cfg.DailyReportEnabled = in.DailyReportEnabled
	cfg.DailyReportTime = reportTime
	cfg.NewsChannelURL = newsURL
	cfg.GroupInviteURL = groupURL

	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := s.settingRepo.Set(ctx, settingKeyTelegramBotConfig, string(data)); err != nil {
		return nil, err
	}
	s.invalidateCache()
	s.generation.Add(1)
	s.recordPoll(nil, false)
	return s.GetSettings(ctx)
}

// normalizeTelegramLinkURL accepts an empty value or an https:// URL.
func normalizeTelegramLinkURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || len(raw) > 512 {
		return "", ErrTelegramInvalidLink
	}
	return raw, nil
}

// recordPoll keeps the latest polling outcome for the admin page.
func (s *TelegramService) recordPoll(err error, polled bool) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if polled {
		s.lastPollAt = s.now()
	}
	if err == nil {
		s.lastError = ""
		return
	}
	s.lastError = truncateString(err.Error(), 300)
}

// ─── Account links ───

func (s *TelegramService) GetLinkStatus(ctx context.Context, userID int64) (*TelegramLinkStatus, error) {
	rt := s.runtime(ctx)
	out := &TelegramLinkStatus{Available: rt.ready && rt.cfg.BotUsername != ""}
	if !out.Available {
		return out, nil
	}
	out.BotUsername = rt.cfg.BotUsername
	b, err := s.bindings.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if b != nil && b.ChatID != 0 {
		out.Linked = true
		out.TelegramUsername = b.TelegramUsername
		out.LinkedAt = b.LinkedAt
		return out, nil
	}
	if b != nil && b.BindCode != "" {
		out.BindCode = b.BindCode
		return out, nil
	}
	code, err := newTelegramBindCode()
	if err != nil {
		return nil, err
	}
	if out.BindCode, err = s.bindings.EnsureBindCode(ctx, userID, code); err != nil {
		return nil, err
	}
	return out, nil
}

// ResetBindCode gives the user a new bind code; the old one stops working.
func (s *TelegramService) ResetBindCode(ctx context.Context, userID int64) (*TelegramLinkStatus, error) {
	rt := s.runtime(ctx)
	if !rt.ready || rt.cfg.BotUsername == "" {
		return nil, ErrTelegramNotConfigured
	}
	code, err := newTelegramBindCode()
	if err != nil {
		return nil, err
	}
	if err := s.bindings.ResetBindCode(ctx, userID, code); err != nil {
		return nil, err
	}
	return s.GetLinkStatus(ctx, userID)
}

func (s *TelegramService) Unlink(ctx context.Context, userID int64) error {
	return s.bindings.UnlinkUser(ctx, userID)
}

// bindChat is called by the bot when a user sends /bind CODE or /start CODE.
func (s *TelegramService) bindChat(ctx context.Context, code string, chatID int64, username, language string) (int64, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if !telegramBindCodePattern.MatchString(code) {
		return 0, ErrTelegramBindCodeInvalid
	}
	return s.bindings.BindChat(ctx, code, chatID, truncateString(username, 64), language, s.now())
}

func newTelegramBindCode() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// ─── Outgoing messages ───

func (s *TelegramService) siteName(ctx context.Context) string {
	name, err := s.settingRepo.GetValue(ctx, SettingKeySiteName)
	if err != nil || strings.TrimSpace(name) == "" {
		return defaultSiteName
	}
	return name
}

// send delivers one message, waiting once if Telegram asks us to slow down.
func (s *TelegramService) send(ctx context.Context, bot telegram.Bot, chatID int64, text string) error {
	sendCtx, cancel := context.WithTimeout(ctx, telegramSendTimeout)
	err := s.api.SendMessage(sendCtx, bot, chatID, text)
	cancel()
	if wait := telegram.RetryAfter(err); wait > 0 && wait <= telegramMaxRetryAfter {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
		sendCtx, cancel = context.WithTimeout(ctx, telegramSendTimeout)
		err = s.api.SendMessage(sendCtx, bot, chatID, text)
		cancel()
	}
	if err != nil {
		slog.Warn("telegram: send message failed", "chat_id", chatID, "error", err)
	}
	return err
}

func (s *TelegramService) sendToAdmins(ctx context.Context, rt telegramRuntime, render func(lang string) string) int {
	targets, err := s.bindings.ListTargets(ctx, true)
	if err != nil {
		slog.Warn("telegram: list admin chats failed", "error", err)
		return 0
	}
	sent := 0
	for _, t := range targets {
		if s.send(ctx, rt.bot, t.ChatID, render(t.Language)) == nil {
			sent++
		}
	}
	return sent
}

// AdminAlertsActive is a cheap check used before deciding to skip alert work.
func (s *TelegramService) AdminAlertsActive(ctx context.Context) bool {
	if s == nil {
		return false
	}
	rt := s.runtime(ctx)
	return rt.ready && rt.cfg.NotifyAdminAlerts
}

func (s *TelegramService) NotifyBalanceLow(ctx context.Context, userID int64, balance, threshold float64, rechargeURL string) {
	if s == nil {
		return
	}
	rt := s.runtime(ctx)
	if !rt.ready || !rt.cfg.NotifyBalanceLow {
		return
	}
	b, err := s.bindings.GetByUserID(ctx, userID)
	if err != nil || b == nil || b.ChatID == 0 {
		return
	}
	_ = s.send(ctx, rt.bot, b.ChatID, telegramBalanceLowText(b.Language, s.siteName(ctx), balance, threshold, rechargeURL))
}

func (s *TelegramService) NotifyAccountQuota(ctx context.Context, accountName, platform, dim string, used, limit float64) {
	if !s.AdminAlertsActive(ctx) {
		return
	}
	rt := s.runtime(ctx)
	site := s.siteName(ctx)
	s.sendToAdmins(ctx, rt, func(lang string) string {
		return telegramAccountQuotaText(lang, site, accountName, platform, dim, used, limit)
	})
}

func (s *TelegramService) NotifyOpsAlert(ctx context.Context, rule *OpsAlertRule, event *OpsAlertEvent) {
	if rule == nil || event == nil || !s.AdminAlertsActive(ctx) {
		return
	}
	rt := s.runtime(ctx)
	site := s.siteName(ctx)
	s.sendToAdmins(ctx, rt, func(lang string) string {
		return telegramOpsAlertText(lang, site, rule, event)
	})
}

// SendTestMessage sends a test message to every linked admin.
func (s *TelegramService) SendTestMessage(ctx context.Context) (int, error) {
	rt := s.runtime(ctx)
	if !rt.ready {
		return 0, ErrTelegramNotConfigured
	}
	site := s.siteName(ctx)
	return s.sendToAdmins(ctx, rt, func(lang string) string { return telegramTestText(lang, site) }), nil
}

// Broadcast queues a short notice to every linked user and returns how many
// chats it will be sent to. Delivery continues in the background.
func (s *TelegramService) Broadcast(ctx context.Context, text string) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > TelegramBroadcastMaxRunes {
		return 0, ErrTelegramBroadcastText
	}
	rt := s.runtime(ctx)
	if !rt.ready {
		return 0, ErrTelegramNotConfigured
	}
	targets, err := s.bindings.ListTargets(ctx, false)
	if err != nil {
		return 0, err
	}
	if !s.broadcasting.CompareAndSwap(false, true) {
		return 0, ErrTelegramBroadcastBusy
	}
	message := telegramNoticeText(s.siteName(ctx), text)
	go func() {
		defer s.broadcasting.Store(false)
		defer func() {
			if r := recover(); r != nil {
				slog.Error("telegram: panic during broadcast", "recover", r)
			}
		}()
		sent := 0
		for i, t := range targets {
			if i > 0 {
				time.Sleep(telegramBroadcastInterval)
			}
			if s.send(context.Background(), rt.bot, t.ChatID, message) == nil {
				sent++
			}
		}
		slog.Info("telegram: notice sent", "recipients", len(targets), "delivered", sent)
	}()
	return len(targets), nil
}

// ─── Small persisted state for the bot worker ───

func (s *TelegramService) loadUpdateOffset(ctx context.Context) int64 {
	raw, err := s.settingRepo.GetValue(ctx, settingKeyTelegramUpdateOffset)
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	return v
}

func (s *TelegramService) saveUpdateOffset(ctx context.Context, offset int64) error {
	return s.settingRepo.Set(ctx, settingKeyTelegramUpdateOffset, strconv.FormatInt(offset, 10))
}

func (s *TelegramService) loadReportLastDate(ctx context.Context) string {
	raw, err := s.settingRepo.GetValue(ctx, settingKeyTelegramReportLastDay)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(raw)
}

func (s *TelegramService) saveReportLastDate(ctx context.Context, day string) error {
	return s.settingRepo.Set(ctx, settingKeyTelegramReportLastDay, day)
}

func normalizeTelegramLanguage(code string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(code)), "zh") {
		return "zh"
	}
	return "en"
}
