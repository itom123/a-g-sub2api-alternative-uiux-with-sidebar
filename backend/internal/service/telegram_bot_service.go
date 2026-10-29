package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/telegram"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/google/uuid"
)

const (
	// Telegram allows one getUpdates consumer per bot, so only the lock holder polls.
	telegramPollLeaderLockKey   = "telegram:bot:poll:leader"
	telegramPollLeaderLockTTL   = 90 * time.Second
	telegramReportLeaderLockKey = "telegram:bot:report:leader"
	telegramReportLeaderLockTTL = 5 * time.Minute

	telegramPollTimeoutSeconds = 25
	telegramIdleWait           = 30 * time.Second
	telegramPeerWait           = 10 * time.Second
	telegramErrorWait          = 10 * time.Second
	telegramConflictWait       = 60 * time.Second
	telegramMaxConflicts       = 3
	telegramCommandTimeout     = 30 * time.Second
	telegramKeysListLimit      = 10
)

type telegramUserReader interface {
	GetByID(ctx context.Context, id int64) (*User, error)
}

type telegramSubscriptionLister interface {
	ListActiveByUserID(ctx context.Context, userID int64) ([]UserSubscription, error)
}

type telegramAPIKeyLister interface {
	ListByUserID(ctx context.Context, userID int64, params pagination.PaginationParams, filters APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error)
}

type telegramUserStatsReader interface {
	GetUserDashboardStats(ctx context.Context, userID int64) (*usagestats.UserDashboardStats, error)
}

type telegramSiteStatsReader interface {
	GetDashboardStats(ctx context.Context) (*usagestats.DashboardStats, error)
}

// TelegramBotService polls Telegram for commands and sends the daily report.
type TelegramBotService struct {
	tg        *TelegramService
	users     telegramUserReader
	subs      telegramSubscriptionLister
	keys      telegramAPIKeyLister
	userStats telegramUserStatsReader
	siteStats telegramSiteStatsReader

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string

	ctx       context.Context
	cancel    context.CancelFunc
	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup

	// Used only by the polling goroutine.
	commandsKey   string // token and menu shape last sent to setMyCommands
	conflicts     int
	paused        bool
	pausedGen     int64
	lastReportDay string
}

func NewTelegramBotService(tg *TelegramService, users telegramUserReader, subs telegramSubscriptionLister, keys telegramAPIKeyLister, userStats telegramUserStatsReader, siteStats telegramSiteStatsReader, lockCache LeaderLockCache, db *sql.DB) *TelegramBotService {
	ctx, cancel := context.WithCancel(context.Background())
	return &TelegramBotService{
		tg:         tg,
		users:      users,
		subs:       subs,
		keys:       keys,
		userStats:  userStats,
		siteStats:  siteStats,
		lockCache:  lockCache,
		db:         db,
		instanceID: uuid.NewString(),
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (s *TelegramBotService) Start() {
	if s == nil || s.tg == nil {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go s.run()
	})
}

func (s *TelegramBotService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(s.cancel)
	s.wg.Wait()
}

func (s *TelegramBotService) run() {
	defer s.wg.Done()
	for {
		wait := s.tick()
		if s.ctx.Err() != nil {
			return
		}
		if wait <= 0 {
			continue
		}
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (s *TelegramBotService) tick() (wait time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("telegram: panic in bot loop", "recover", r)
			wait = telegramErrorWait
		}
	}()
	rt := s.tg.runtime(s.ctx)
	if !rt.ready {
		s.conflicts = 0
		return telegramIdleWait
	}
	s.maybeSendDailyReport(rt)

	if s.paused {
		if s.tg.generation.Load() == s.pausedGen {
			return telegramIdleWait
		}
		s.paused = false
		s.conflicts = 0
	}
	return s.pollOnce(rt)
}

func (s *TelegramBotService) pollOnce(rt telegramRuntime) time.Duration {
	release, ok := tryAcquireSingletonLeaderLock(s.ctx, s.lockCache, s.db, telegramPollLeaderLockKey, s.instanceID, telegramPollLeaderLockTTL)
	if !ok {
		return telegramPeerWait
	}
	defer release()

	s.ensureCommands(rt)
	offset := s.tg.loadUpdateOffset(s.ctx)
	updates, err := s.tg.api.GetUpdates(s.ctx, rt.bot, offset, telegramPollTimeoutSeconds)
	if s.ctx.Err() != nil {
		return 0
	}
	if err != nil {
		s.tg.recordPoll(err, true)
		switch {
		case telegram.IsConflict(err):
			// Another program (or a webhook) is reading this bot. Back off,
			// and stop entirely after a few tries so we never steal its messages.
			s.conflicts++
			if s.conflicts >= telegramMaxConflicts {
				s.paused = true
				s.pausedGen = s.tg.generation.Load()
				slog.Warn("telegram: polling paused; another program is using this bot token")
			}
			return telegramConflictWait
		case telegram.IsUnauthorized(err):
			return telegramIdleWait
		}
		if d := telegram.RetryAfter(err); d > 0 {
			return d
		}
		slog.Warn("telegram: getUpdates failed", "error", err)
		return telegramErrorWait
	}
	s.conflicts = 0
	s.tg.recordPoll(nil, true)

	next := offset
	for _, u := range updates {
		if u.UpdateID >= next {
			next = u.UpdateID + 1
		}
	}
	// Save the offset before handling so one bad message can't loop forever.
	if next != offset {
		if err := s.tg.saveUpdateOffset(s.ctx, next); err != nil {
			slog.Warn("telegram: save update offset failed", "error", err)
		}
	}
	for _, u := range updates {
		s.handleUpdate(rt, u)
	}
	return 0
}

func (s *TelegramBotService) ensureCommands(rt telegramRuntime) {
	hasGroup := rt.cfg.GroupInviteURL != ""
	key := rt.bot.Token + "|" + strconv.FormatBool(hasGroup)
	if s.commandsKey == key {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, telegramSendTimeout)
	defer cancel()
	if err := s.tg.api.SetMyCommands(ctx, rt.bot, telegramCommandMenu("en", hasGroup), ""); err != nil {
		slog.Warn("telegram: set commands failed", "error", err)
		return
	}
	_ = s.tg.api.SetMyCommands(ctx, rt.bot, telegramCommandMenu("zh", hasGroup), "zh")
	s.commandsKey = key
}

func telegramCommandMenu(lang string, hasGroup bool) []telegram.BotCommand {
	cmds := []telegram.BotCommand{
		{Command: "bind", Description: tgText(lang, "Link your account: /bind CODE", "绑定账号：/bind 绑定码")},
		{Command: "balance", Description: tgText(lang, "Balance and subscriptions", "余额与订阅")},
		{Command: "usage", Description: tgText(lang, "Today and total usage", "今日与累计用量")},
		{Command: "keys", Description: tgText(lang, "API keys", "API 密钥")},
	}
	if hasGroup {
		cmds = append(cmds, telegram.BotCommand{Command: "getchatlink", Description: tgText(lang, "Discussion group link", "讨论组链接")})
	}
	return append(cmds,
		telegram.BotCommand{Command: "help", Description: tgText(lang, "All commands", "全部命令")},
		telegram.BotCommand{Command: "unbind", Description: tgText(lang, "Unlink this chat", "解除绑定")},
	)
}

// parseTelegramCommand splits "/cmd@BotName arg" into ("cmd", "arg").
func parseTelegramCommand(text string) (string, string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", ""
	}
	first := strings.Fields(text)[0]
	arg := strings.TrimSpace(strings.TrimPrefix(text, first))
	cmd := strings.TrimPrefix(first, "/")
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	return strings.ToLower(cmd), arg
}

func (s *TelegramBotService) handleUpdate(rt telegramRuntime, u telegram.Update) {
	if u.ChatJoinRequest != nil {
		s.handleJoinRequest(rt, u.ChatJoinRequest)
		return
	}
	m := u.Message
	// Private chats only: account data must never be posted into a group.
	if m == nil || m.From == nil || m.From.IsBot || m.Chat.Type != "private" {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, telegramCommandTimeout)
	defer cancel()
	lang := normalizeTelegramLanguage(m.From.LanguageCode)
	cmd, arg := parseTelegramCommand(m.Text)
	if reply := s.reply(ctx, rt, m, cmd, arg, lang); reply != "" {
		_ = s.tg.send(ctx, rt.bot, m.Chat.ID, reply)
	}
}

func (s *TelegramBotService) reply(ctx context.Context, rt telegramRuntime, m *telegram.Message, cmd, arg, lang string) string {
	if (cmd == "start" || cmd == "bind") && arg != "" {
		return s.link(ctx, rt, m, arg, lang)
	}
	site := s.tg.siteName(ctx)
	binding, err := s.tg.bindings.GetByChatID(ctx, m.Chat.ID)
	if err != nil {
		slog.Warn("telegram: lookup chat failed", "error", err)
		return telegramTryLaterText(lang)
	}
	if binding == nil {
		return telegramNotLinkedText(lang, site)
	}
	user, err := s.users.GetByID(ctx, binding.UserID)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return telegramTryLaterText(lang)
	}
	if user == nil || user.Status != StatusActive {
		return telegramAccountUnavailableText(lang)
	}
	isAdmin := user.Role == RoleAdmin

	switch cmd {
	case "balance":
		subs, err := s.subs.ListActiveByUserID(ctx, user.ID)
		if err != nil {
			slog.Warn("telegram: list subscriptions failed", "user_id", user.ID, "error", err)
			subs = nil
		}
		return telegramBalanceText(lang, user, subs)
	case "usage":
		st, err := s.userStats.GetUserDashboardStats(ctx, user.ID)
		if err != nil || st == nil {
			return telegramTryLaterText(lang)
		}
		return telegramUsageText(lang, st)
	case "keys":
		keys, page, err := s.keys.ListByUserID(ctx, user.ID, pagination.PaginationParams{Page: 1, PageSize: telegramKeysListLimit}, APIKeyListFilters{})
		if err != nil {
			return telegramTryLaterText(lang)
		}
		total := int64(len(keys))
		if page != nil {
			total = page.Total
		}
		return telegramKeysText(lang, keys, total)
	case "unbind":
		if err := s.tg.bindings.UnlinkChat(ctx, m.Chat.ID); err != nil {
			return telegramTryLaterText(lang)
		}
		return telegramUnlinkedText(lang)
	case "getchatlink":
		return telegramGroupLinkText(lang, site, rt.cfg.GroupInviteURL)
	case "report":
		if !isAdmin {
			return telegramAdminOnlyText(lang)
		}
		st, err := s.siteStats.GetDashboardStats(ctx)
		if err != nil || st == nil {
			return telegramTryLaterText(lang)
		}
		return telegramDailyReportText(lang, site, timezone.Now().Format("2006-01-02"), st)
	default:
		return telegramHelpText(lang, isAdmin, rt.cfg.GroupInviteURL != "")
	}
}

func (s *TelegramBotService) link(ctx context.Context, rt telegramRuntime, m *telegram.Message, code, lang string) string {
	userID, err := s.tg.bindChat(ctx, code, m.Chat.ID, m.From.Username, lang)
	switch {
	case errors.Is(err, ErrTelegramBindCodeInvalid):
		return telegramLinkInvalidText(lang, s.tg.siteName(ctx))
	case errors.Is(err, ErrTelegramLinkedElsewhere):
		return telegramLinkedElsewhereText(lang)
	case err != nil:
		slog.Warn("telegram: link failed", "error", err)
		return telegramTryLaterText(lang)
	}
	account := tgText(lang, "your account", "你的账号")
	if user, err := s.users.GetByID(ctx, userID); err == nil && user != nil {
		account = MaskEmail(user.Email)
	}
	return telegramLinkedText(lang, account, rt.cfg.NewsChannelURL)
}

// handleJoinRequest approves group join requests from linked, active users
// and declines the rest. Only active when a group link is configured.
func (s *TelegramBotService) handleJoinRequest(rt telegramRuntime, req *telegram.ChatJoinRequest) {
	if rt.cfg.GroupInviteURL == "" || req.From.IsBot {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, telegramCommandTimeout)
	defer cancel()
	lang := normalizeTelegramLanguage(req.From.LanguageCode)
	site := s.tg.siteName(ctx)
	dm := req.UserChatID
	if dm == 0 {
		dm = req.From.ID
	}

	// A private chat's ID is the user's ID, so the binding is keyed by it.
	binding, err := s.tg.bindings.GetByChatID(ctx, req.From.ID)
	if err != nil {
		slog.Warn("telegram: join request lookup failed", "error", err)
		return // leave it pending rather than decline by mistake
	}
	approved := false
	if binding != nil {
		user, err := s.users.GetByID(ctx, binding.UserID)
		if err != nil && !errors.Is(err, ErrUserNotFound) {
			slog.Warn("telegram: join request user lookup failed", "error", err)
			return
		}
		approved = user != nil && user.Status == StatusActive
	}

	if approved {
		if err := s.tg.api.ApproveChatJoinRequest(ctx, rt.bot, req.Chat.ID, req.From.ID); err != nil {
			slog.Warn("telegram: approve join request failed", "error", err)
			return
		}
		_ = s.tg.send(ctx, rt.bot, dm, telegramGroupApprovedText(lang, site))
		return
	}
	if err := s.tg.api.DeclineChatJoinRequest(ctx, rt.bot, req.Chat.ID, req.From.ID); err != nil {
		slog.Warn("telegram: decline join request failed", "error", err)
		return
	}
	_ = s.tg.send(ctx, rt.bot, dm, telegramGroupDeclinedText(lang, site))
}

// maybeSendDailyReport sends today's report once, after the configured time.
func (s *TelegramBotService) maybeSendDailyReport(rt telegramRuntime) {
	if !rt.cfg.DailyReportEnabled {
		return
	}
	at, err := time.Parse("15:04", rt.cfg.DailyReportTime)
	if err != nil {
		return
	}
	now := timezone.Now()
	due := time.Date(now.Year(), now.Month(), now.Day(), at.Hour(), at.Minute(), 0, 0, now.Location())
	day := now.Format("2006-01-02")
	if now.Before(due) || s.lastReportDay == day {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
	defer cancel()
	if s.tg.loadReportLastDate(ctx) == day {
		s.lastReportDay = day
		return
	}
	release, ok := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, telegramReportLeaderLockKey, s.instanceID, telegramReportLeaderLockTTL)
	if !ok {
		return
	}
	defer release()
	if s.tg.loadReportLastDate(ctx) == day {
		s.lastReportDay = day
		return
	}
	// Mark first: a missed report is better than a duplicate on every tick.
	if err := s.tg.saveReportLastDate(ctx, day); err != nil {
		slog.Warn("telegram: save report date failed", "error", err)
		return
	}
	s.lastReportDay = day
	st, err := s.siteStats.GetDashboardStats(ctx)
	if err != nil || st == nil {
		slog.Warn("telegram: load dashboard stats for report failed", "error", err)
		return
	}
	site := s.tg.siteName(ctx)
	s.tg.sendToAdmins(ctx, rt, func(lang string) string {
		return telegramDailyReportText(lang, site, day, st)
	})
}
