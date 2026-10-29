//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/telegram"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

// ─── fakes ───

type tgSettingRepoFake struct {
	mu     sync.Mutex
	values map[string]string
}

func newTGSettingRepoFake() *tgSettingRepoFake {
	return &tgSettingRepoFake{values: map[string]string{SettingKeySiteName: "TestSite"}}
}

func (r *tgSettingRepoFake) Get(ctx context.Context, key string) (*Setting, error) {
	v, err := r.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return &Setting{Key: key, Value: v}, nil
}
func (r *tgSettingRepoFake) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return v, nil
}
func (r *tgSettingRepoFake) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[key] = value
	return nil
}
func (r *tgSettingRepoFake) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, nil
}
func (r *tgSettingRepoFake) SetMultiple(context.Context, map[string]string) error { return nil }
func (r *tgSettingRepoFake) GetAll(context.Context) (map[string]string, error)    { return nil, nil }
func (r *tgSettingRepoFake) Delete(context.Context, string) error                 { return nil }

type tgEncryptorFake struct{}

func (tgEncryptorFake) Encrypt(p string) (string, error) { return "enc:" + p, nil }
func (tgEncryptorFake) Decrypt(c string) (string, error) {
	if !strings.HasPrefix(c, "enc:") {
		return "", errors.New("bad ciphertext")
	}
	return strings.TrimPrefix(c, "enc:"), nil
}

type tgBindingRepoFake struct {
	mu            sync.Mutex
	byUser        map[int64]*TelegramBinding
	admins        map[int64]bool
	chatLookupErr error
}

func newTGBindingRepoFake() *tgBindingRepoFake {
	return &tgBindingRepoFake{byUser: map[int64]*TelegramBinding{}, admins: map[int64]bool{}}
}

func (r *tgBindingRepoFake) link(userID, chatID int64, lang string, admin bool) {
	r.byUser[userID] = &TelegramBinding{UserID: userID, BindCode: fmt.Sprintf("%032x", userID), ChatID: chatID, Language: lang}
	r.admins[userID] = admin
}

func tgUnlinkFake(b *TelegramBinding) {
	b.ChatID, b.TelegramUsername, b.Language, b.LinkedAt = 0, "", "", nil
}
func (r *tgBindingRepoFake) GetByUserID(_ context.Context, userID int64) (*TelegramBinding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.byUser[userID]; ok {
		cp := *b
		return &cp, nil
	}
	return nil, nil
}
func (r *tgBindingRepoFake) GetByChatID(_ context.Context, chatID int64) (*TelegramBinding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.chatLookupErr != nil {
		return nil, r.chatLookupErr
	}
	for _, b := range r.byUser {
		if b.ChatID == chatID && chatID != 0 {
			cp := *b
			return &cp, nil
		}
	}
	return nil, nil
}
func (r *tgBindingRepoFake) EnsureBindCode(_ context.Context, userID int64, code string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.byUser[userID]; ok {
		return b.BindCode, nil
	}
	r.byUser[userID] = &TelegramBinding{UserID: userID, BindCode: code}
	return code, nil
}
func (r *tgBindingRepoFake) ResetBindCode(_ context.Context, userID int64, code string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.byUser[userID]; ok {
		b.BindCode = code
		return nil
	}
	r.byUser[userID] = &TelegramBinding{UserID: userID, BindCode: code}
	return nil
}
func (r *tgBindingRepoFake) BindChat(_ context.Context, code string, chatID int64, username, lang string, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var owner *TelegramBinding
	for _, b := range r.byUser {
		if b.BindCode == code {
			owner = b
		}
	}
	if owner == nil {
		return 0, ErrTelegramBindCodeInvalid
	}
	if owner.ChatID != 0 && owner.ChatID != chatID {
		return 0, ErrTelegramLinkedElsewhere
	}
	for _, b := range r.byUser {
		if b.ChatID == chatID && b.UserID != owner.UserID {
			tgUnlinkFake(b)
		}
	}
	owner.ChatID, owner.TelegramUsername, owner.Language, owner.LinkedAt = chatID, username, lang, &now
	return owner.UserID, nil
}
func (r *tgBindingRepoFake) UnlinkUser(_ context.Context, userID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.byUser[userID]; ok {
		tgUnlinkFake(b)
	}
	return nil
}
func (r *tgBindingRepoFake) UnlinkChat(_ context.Context, chatID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.byUser {
		if b.ChatID == chatID {
			tgUnlinkFake(b)
		}
	}
	return nil
}
func (r *tgBindingRepoFake) ListTargets(_ context.Context, adminsOnly bool) ([]TelegramChatTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []TelegramChatTarget
	for id, b := range r.byUser {
		if b.ChatID == 0 || (adminsOnly && !r.admins[id]) {
			continue
		}
		out = append(out, TelegramChatTarget{UserID: id, ChatID: b.ChatID, Language: b.Language})
	}
	return out, nil
}
func (r *tgBindingRepoFake) CountLinked(ctx context.Context) (int64, int64, error) {
	all, _ := r.ListTargets(ctx, false)
	admins, _ := r.ListTargets(ctx, true)
	return int64(len(all)), int64(len(admins)), nil
}

type tgSent struct {
	chatID int64
	text   string
}

type tgAPIFake struct {
	mu          sync.Mutex
	meErr       error
	username    string
	sent        []tgSent
	updates     []telegram.Update
	updatesErr  error
	pollOffsets []int64
	commandSets int
	menuEN      []telegram.BotCommand
	approved    []int64
	declined    []int64
}

func (a *tgAPIFake) GetMe(context.Context, telegram.Bot) (*telegram.User, error) {
	if a.meErr != nil {
		return nil, a.meErr
	}
	return &telegram.User{ID: 1, IsBot: true, Username: a.username}, nil
}
func (a *tgAPIFake) GetUpdates(_ context.Context, _ telegram.Bot, offset int64, _ int) ([]telegram.Update, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pollOffsets = append(a.pollOffsets, offset)
	if a.updatesErr != nil {
		return nil, a.updatesErr
	}
	u := a.updates
	a.updates = nil
	return u, nil
}
func (a *tgAPIFake) SendMessage(_ context.Context, _ telegram.Bot, chatID int64, text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = append(a.sent, tgSent{chatID, text})
	return nil
}
func (a *tgAPIFake) SetMyCommands(_ context.Context, _ telegram.Bot, cmds []telegram.BotCommand, lang string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.commandSets++
	if lang == "" {
		a.menuEN = cmds
	}
	return nil
}
func (a *tgAPIFake) ApproveChatJoinRequest(_ context.Context, _ telegram.Bot, _, userID int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.approved = append(a.approved, userID)
	return nil
}
func (a *tgAPIFake) DeclineChatJoinRequest(_ context.Context, _ telegram.Bot, _, userID int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.declined = append(a.declined, userID)
	return nil
}
func (a *tgAPIFake) sentTo(chatID int64) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, s := range a.sent {
		if s.chatID == chatID {
			out = append(out, s.text)
		}
	}
	return out
}

type tgUsersFake map[int64]*User

func (u tgUsersFake) GetByID(_ context.Context, id int64) (*User, error) {
	if user, ok := u[id]; ok {
		return user, nil
	}
	return nil, ErrUserNotFound
}

type tgSubsFake []UserSubscription

func (s tgSubsFake) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return s, nil
}

type tgKeysFake []APIKey

func (k tgKeysFake) ListByUserID(context.Context, int64, pagination.PaginationParams, APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	return k, &pagination.PaginationResult{Total: int64(len(k))}, nil
}

type tgStatsFake struct{}

func (tgStatsFake) GetUserDashboardStats(context.Context, int64) (*usagestats.UserDashboardStats, error) {
	return &usagestats.UserDashboardStats{TodayRequests: 1234, TodayTokens: 2_500_000, TodayActualCost: 1.5}, nil
}
func (tgStatsFake) GetDashboardStats(context.Context) (*usagestats.DashboardStats, error) {
	return &usagestats.DashboardStats{TodayRequests: 99, TotalUsers: 10, ErrorAccounts: 2}, nil
}

type tgFixture struct {
	settings *tgSettingRepoFake
	bindings *tgBindingRepoFake
	api      *tgAPIFake
	tg       *TelegramService
	bot      *TelegramBotService
	users    tgUsersFake
}

func newTGFixture(t *testing.T) *tgFixture {
	t.Helper()
	f := &tgFixture{
		settings: newTGSettingRepoFake(),
		bindings: newTGBindingRepoFake(),
		api:      &tgAPIFake{username: "sub2api_test_bot"},
		users: tgUsersFake{
			1: {ID: 1, Email: "alice@example.com", Role: RoleUser, Status: StatusActive, Balance: 12.5},
			2: {ID: 2, Email: "admin@example.com", Role: RoleAdmin, Status: StatusActive},
			3: {ID: 3, Email: "banned@example.com", Role: RoleUser, Status: "disabled"},
		},
	}
	f.tg = NewTelegramService(f.settings, tgEncryptorFake{}, f.bindings, f.api, true)
	f.bot = NewTelegramBotService(f.tg, f.users, tgSubsFake{}, tgKeysFake{}, tgStatsFake{}, tgStatsFake{}, nil, nil)
	return f
}

func (f *tgFixture) enable(t *testing.T) telegramRuntime {
	t.Helper()
	return f.enableWith(t, TelegramBotSettingsInput{})
}

// enableWith turns the bot on with in's link settings.
func (f *tgFixture) enableWith(t *testing.T, in TelegramBotSettingsInput) telegramRuntime {
	t.Helper()
	in.Enabled, in.BotToken, in.NotifyBalanceLow, in.NotifyAdminAlerts = true, "123:secret", true, true
	_, err := f.tg.UpdateSettings(context.Background(), in)
	require.NoError(t, err)
	rt := f.tg.runtime(context.Background())
	require.True(t, rt.ready)
	return rt
}

func privateMessage(chatID int64, text string) telegram.Update {
	return telegram.Update{UpdateID: 1, Message: &telegram.Message{
		From: &telegram.User{ID: chatID, Username: "tguser", LanguageCode: "en"},
		Chat: telegram.Chat{ID: chatID, Type: "private"},
		Text: text,
	}}
}

// ─── settings ───

func TestTelegramUpdateSettingsStoresEncryptedTokenAndHidesIt(t *testing.T) {
	f := newTGFixture(t)
	f.settings.values[settingKeyTelegramUpdateOffset] = "500"

	out, err := f.tg.UpdateSettings(context.Background(), TelegramBotSettingsInput{Enabled: true, BotToken: " 123:secret ", DailyReportTime: "09:30"})
	require.NoError(t, err)
	require.True(t, out.TokenConfigured)
	require.Equal(t, "sub2api_test_bot", out.BotUsername)
	require.Equal(t, "09:30", out.DailyReportTime)
	require.Contains(t, f.settings.values[settingKeyTelegramBotConfig], `"bot_token_encrypted":"enc:123:secret"`)
	require.Equal(t, "0", f.settings.values[settingKeyTelegramUpdateOffset], "a new token resets the update offset")

	// Saving again without a token keeps it.
	out, err = f.tg.UpdateSettings(context.Background(), TelegramBotSettingsInput{Enabled: true})
	require.NoError(t, err)
	require.True(t, out.TokenConfigured)
	require.Equal(t, "123:secret", f.tg.runtime(context.Background()).bot.Token)
}

func TestTelegramUpdateSettingsValidation(t *testing.T) {
	ctx := context.Background()

	f := newTGFixture(t)
	_, err := f.tg.UpdateSettings(ctx, TelegramBotSettingsInput{Enabled: true})
	require.ErrorIs(t, err, ErrTelegramNotConfigured)

	_, err = f.tg.UpdateSettings(ctx, TelegramBotSettingsInput{BotToken: "x", DailyReportTime: "25:99"})
	require.ErrorIs(t, err, ErrTelegramInvalidTime)

	_, err = f.tg.UpdateSettings(ctx, TelegramBotSettingsInput{BotToken: "x", ProxyURL: "ftp://bad"})
	require.ErrorIs(t, err, ErrTelegramInvalidProxy)

	f.api.meErr = &telegram.APIError{Code: 401, Description: "Unauthorized"}
	_, err = f.tg.UpdateSettings(ctx, TelegramBotSettingsInput{BotToken: "bad"})
	require.ErrorIs(t, err, ErrTelegramInvalidToken)

	noKey := NewTelegramService(newTGSettingRepoFake(), tgEncryptorFake{}, newTGBindingRepoFake(), &tgAPIFake{username: "b"}, false)
	_, err = noKey.UpdateSettings(ctx, TelegramBotSettingsInput{BotToken: "123:secret"})
	require.ErrorIs(t, err, ErrTelegramEncryptionKey)

	for _, bad := range []string{"http://t.me/news", "t.me/news", "https://"} {
		_, err = newTGFixture(t).tg.UpdateSettings(ctx, TelegramBotSettingsInput{BotToken: "x", NewsChannelURL: bad})
		require.ErrorIs(t, err, ErrTelegramInvalidLink, bad)
		_, err = newTGFixture(t).tg.UpdateSettings(ctx, TelegramBotSettingsInput{BotToken: "x", GroupInviteURL: bad})
		require.ErrorIs(t, err, ErrTelegramInvalidLink, bad)
	}
	out, err := newTGFixture(t).tg.UpdateSettings(ctx, TelegramBotSettingsInput{
		BotToken: "x", NewsChannelURL: " https://t.me/sitenews ", GroupInviteURL: "https://t.me/+AbC123",
	})
	require.NoError(t, err)
	require.Equal(t, "https://t.me/sitenews", out.NewsChannelURL)
	require.Equal(t, "https://t.me/+AbC123", out.GroupInviteURL)
}

// ─── linking ───

func TestTelegramBindCodeIsPermanent(t *testing.T) {
	f := newTGFixture(t)
	rt := f.enable(t)
	ctx := context.Background()

	status, err := f.tg.GetLinkStatus(ctx, 1)
	require.NoError(t, err)
	code := status.BindCode
	require.Len(t, code, 32)
	again, err := f.tg.GetLinkStatus(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, code, again.BindCode, "the code does not change between visits")

	f.bot.handleUpdate(rt, privateMessage(777, "/bind "+code))
	replies := f.api.sentTo(777)
	require.Len(t, replies, 1)
	require.Contains(t, replies[0], MaskEmail("alice@example.com"))

	status, err = f.tg.GetLinkStatus(ctx, 1)
	require.NoError(t, err)
	require.True(t, status.Linked)
	require.Equal(t, "tguser", status.TelegramUsername)
	require.Empty(t, status.BindCode, "the code is hidden while linked")

	// After unlinking, the same code links again (the one-tap /start link uses it too).
	require.NoError(t, f.tg.Unlink(ctx, 1))
	status, err = f.tg.GetLinkStatus(ctx, 1)
	require.NoError(t, err)
	require.False(t, status.Linked)
	require.Equal(t, code, status.BindCode)
	f.bot.handleUpdate(rt, privateMessage(778, "/start "+code))
	require.Contains(t, f.api.sentTo(778)[0], "Linked to")
}

func TestTelegramBindRefusesAccountLinkedElsewhere(t *testing.T) {
	f := newTGFixture(t)
	rt := f.enable(t)
	status, err := f.tg.GetLinkStatus(context.Background(), 1)
	require.NoError(t, err)
	f.bot.handleUpdate(rt, privateMessage(777, "/bind "+status.BindCode))

	// Someone else with the code cannot take over the account.
	f.bot.handleUpdate(rt, privateMessage(888, "/bind "+status.BindCode))
	require.Contains(t, f.api.sentTo(888)[0], "already linked to another Telegram account")
	b, _ := f.bindings.GetByChatID(context.Background(), 888)
	require.Nil(t, b)

	// The linked chat may bind again.
	f.bot.handleUpdate(rt, privateMessage(777, "/bind "+status.BindCode))
	require.Contains(t, f.api.sentTo(777)[1], "Linked to")
}

func TestTelegramResetBindCode(t *testing.T) {
	f := newTGFixture(t)
	ctx := context.Background()
	_, err := f.tg.ResetBindCode(ctx, 1)
	require.ErrorIs(t, err, ErrTelegramNotConfigured)
	status, err := f.tg.GetLinkStatus(ctx, 1)
	require.NoError(t, err)
	require.False(t, status.Available)

	rt := f.enable(t)
	old, err := f.tg.GetLinkStatus(ctx, 1)
	require.NoError(t, err)
	fresh, err := f.tg.ResetBindCode(ctx, 1)
	require.NoError(t, err)
	require.Len(t, fresh.BindCode, 32)
	require.NotEqual(t, old.BindCode, fresh.BindCode)

	f.bot.handleUpdate(rt, privateMessage(777, "/bind "+old.BindCode))
	require.Contains(t, f.api.sentTo(777)[0], "not valid")
	f.bot.handleUpdate(rt, privateMessage(777, "/bind "+fresh.BindCode))
	require.Contains(t, f.api.sentTo(777)[1], "Linked to")
}

func TestTelegramLinkedReplyPointsToNewsChannel(t *testing.T) {
	f := newTGFixture(t)
	rt := f.enableWith(t, TelegramBotSettingsInput{NewsChannelURL: "https://t.me/sitenews"})
	status, err := f.tg.GetLinkStatus(context.Background(), 1)
	require.NoError(t, err)

	f.bot.handleUpdate(rt, privateMessage(777, "/bind "+status.BindCode))
	require.Contains(t, f.api.sentTo(777)[0], "https://t.me/sitenews")
}

// ─── commands ───

func TestTelegramCommands(t *testing.T) {
	f := newTGFixture(t)
	rt := f.enable(t)
	f.bindings.link(1, 100, "en", false)
	f.bindings.link(2, 200, "zh", true)
	f.bindings.link(3, 300, "en", false)

	cases := []struct {
		chatID int64
		text   string
		want   string
	}{
		{100, "/balance", "Balance: $12.50"},
		{100, "/usage", "Today: 1,234 requests · 2.50M tokens · $1.50"},
		{100, "/keys", "no API keys"},
		{100, "/report", "admins only"},
		{100, "/help", "/balance"},
		{100, "hello", "/balance"},
		{200, "/report@sub2api_test_bot", "TestSite"},
		{300, "/balance", "disabled"},
		{999, "/balance", "not linked"},
	}
	for _, tc := range cases {
		f.bot.handleUpdate(rt, privateMessage(tc.chatID, tc.text))
		replies := f.api.sentTo(tc.chatID)
		require.NotEmpty(t, replies, tc.text)
		require.Contains(t, replies[len(replies)-1], tc.want, tc.text)
	}

	f.bot.handleUpdate(rt, privateMessage(100, "/getchatlink"))
	require.Contains(t, f.api.sentTo(100)[len(f.api.sentTo(100))-1], "no discussion group")
	require.NotContains(t, telegramHelpText("en", false, false), "/getchatlink")

	f.bot.handleUpdate(rt, privateMessage(100, "/unbind"))
	b, _ := f.bindings.GetByChatID(context.Background(), 100)
	require.Nil(t, b)
	require.Equal(t, fmt.Sprintf("%032x", 1), f.bindings.byUser[1].BindCode, "unbinding keeps the bind code")
}

// ─── discussion group ───

func joinRequest(userID int64) telegram.Update {
	return telegram.Update{UpdateID: 1, ChatJoinRequest: &telegram.ChatJoinRequest{
		Chat:       telegram.Chat{ID: -1001, Type: "supergroup"},
		From:       telegram.User{ID: userID, LanguageCode: "en"},
		UserChatID: userID,
	}}
}

func TestTelegramGroupLinkCommand(t *testing.T) {
	f := newTGFixture(t)
	rt := f.enableWith(t, TelegramBotSettingsInput{GroupInviteURL: "https://t.me/+AbC123"})
	f.bindings.link(1, 100, "en", false)

	f.bot.handleUpdate(rt, privateMessage(100, "/getchatlink"))
	require.Contains(t, f.api.sentTo(100)[0], "https://t.me/+AbC123")
	f.bot.handleUpdate(rt, privateMessage(100, "/help"))
	require.Contains(t, f.api.sentTo(100)[1], "/getchatlink")

	// Unlinked chats are told to bind first.
	f.bot.handleUpdate(rt, privateMessage(999, "/getchatlink"))
	require.Contains(t, f.api.sentTo(999)[0], "/bind")
}

func TestTelegramGroupJoinRequests(t *testing.T) {
	f := newTGFixture(t)
	rt := f.enableWith(t, TelegramBotSettingsInput{GroupInviteURL: "https://t.me/+AbC123"})
	f.bindings.link(1, 100, "en", false)
	f.bindings.link(3, 300, "en", false) // disabled user

	f.bot.handleUpdate(rt, joinRequest(100))
	f.bot.handleUpdate(rt, joinRequest(555))
	f.bot.handleUpdate(rt, joinRequest(300))
	require.Equal(t, []int64{100}, f.api.approved)
	require.Equal(t, []int64{555, 300}, f.api.declined)
	require.Contains(t, f.api.sentTo(100)[0], "automatic check")
	require.Contains(t, f.api.sentTo(555)[0], "Only linked")

	// A lookup failure leaves the request pending instead of declining it.
	f.bindings.chatLookupErr = errors.New("db down")
	f.bot.handleUpdate(rt, joinRequest(100))
	require.Len(t, f.api.approved, 1)
	require.Len(t, f.api.declined, 2)
}

func TestTelegramJoinRequestsIgnoredWithoutGroup(t *testing.T) {
	f := newTGFixture(t)
	rt := f.enable(t)
	f.bot.handleUpdate(rt, joinRequest(555))
	require.Empty(t, f.api.approved)
	require.Empty(t, f.api.declined)
	require.Empty(t, f.api.sentTo(555))
}

func TestTelegramCommandMenuFollowsGroupSetting(t *testing.T) {
	f := newTGFixture(t)
	rt := f.enable(t)
	f.bot.ensureCommands(rt)
	menu := func() []string {
		var names []string
		for _, c := range f.api.menuEN {
			names = append(names, c.Command)
		}
		return names
	}
	require.Equal(t, []string{"bind", "balance", "usage", "keys", "help", "unbind"}, menu())
	f.bot.ensureCommands(rt)
	require.Equal(t, 2, f.api.commandSets, "unchanged menu is not resent")

	rt = f.enableWith(t, TelegramBotSettingsInput{GroupInviteURL: "https://t.me/+AbC123"})
	f.bot.ensureCommands(rt)
	require.Contains(t, menu(), "getchatlink")
	require.Equal(t, 4, f.api.commandSets)
}

func TestTelegramIgnoresGroupChats(t *testing.T) {
	f := newTGFixture(t)
	rt := f.enable(t)
	f.bindings.link(1, 100, "en", false)

	u := privateMessage(100, "/balance")
	u.Message.Chat.Type = "group"
	f.bot.handleUpdate(rt, u)
	require.Empty(t, f.api.sentTo(100))
}

func TestParseTelegramCommand(t *testing.T) {
	cmd, arg := parseTelegramCommand("/start abc123")
	require.Equal(t, "start", cmd)
	require.Equal(t, "abc123", arg)
	cmd, arg = parseTelegramCommand("/Balance@MyBot")
	require.Equal(t, "balance", cmd)
	require.Empty(t, arg)
	cmd, _ = parseTelegramCommand("just text")
	require.Empty(t, cmd)
}

// ─── polling ───

func TestTelegramPollSavesOffsetAndHandlesUpdates(t *testing.T) {
	f := newTGFixture(t)
	rt := f.enable(t)
	f.bindings.link(1, 100, "en", false)
	f.api.updates = []telegram.Update{privateMessage(100, "/balance")}
	f.api.updates[0].UpdateID = 41

	wait := f.bot.pollOnce(rt)
	require.Zero(t, wait)
	require.Equal(t, "42", f.settings.values[settingKeyTelegramUpdateOffset])
	require.Len(t, f.api.sentTo(100), 1)
	require.Equal(t, 1, f.api.commandSets/2, "commands are registered once per token")

	f.bot.pollOnce(rt)
	require.Equal(t, int64(42), f.api.pollOffsets[1])
}

func TestTelegramPollPausesAfterRepeatedConflicts(t *testing.T) {
	f := newTGFixture(t)
	f.enable(t)
	f.api.updatesErr = &telegram.APIError{Code: 409, Description: "Conflict: terminated by other getUpdates request"}

	for i := 0; i < telegramMaxConflicts; i++ {
		require.Equal(t, telegramConflictWait, f.bot.tick())
	}
	require.True(t, f.bot.paused)
	polls := len(f.api.pollOffsets)
	f.bot.tick()
	require.Len(t, f.api.pollOffsets, polls, "paused bot does not poll")

	settings, err := f.tg.GetSettings(context.Background())
	require.NoError(t, err)
	require.Contains(t, settings.LastError, "Conflict")

	// Saving settings resumes polling.
	f.api.updatesErr = nil
	_, err = f.tg.UpdateSettings(context.Background(), TelegramBotSettingsInput{Enabled: true})
	require.NoError(t, err)
	f.bot.tick()
	require.False(t, f.bot.paused)
	require.Len(t, f.api.pollOffsets, polls+1)
}

// ─── notifications ───

func TestTelegramDailyReportSentOncePerDay(t *testing.T) {
	f := newTGFixture(t)
	_, err := f.tg.UpdateSettings(context.Background(), TelegramBotSettingsInput{
		Enabled: true, BotToken: "123:secret", DailyReportEnabled: true, DailyReportTime: "00:00",
	})
	require.NoError(t, err)
	f.bindings.link(2, 200, "en", true)
	f.bindings.link(1, 100, "en", false)
	rt := f.tg.runtime(context.Background())

	f.bot.maybeSendDailyReport(rt)
	f.bot.lastReportDay = "" // simulate another instance: the stored date must still dedupe
	f.bot.maybeSendDailyReport(rt)

	require.Len(t, f.api.sentTo(200), 1)
	require.Contains(t, f.api.sentTo(200)[0], "Requests: 99")
	require.Empty(t, f.api.sentTo(100), "reports go to admins only")
}

func TestTelegramNotifications(t *testing.T) {
	f := newTGFixture(t)
	f.enable(t)
	f.bindings.link(1, 100, "zh", false)
	f.bindings.link(2, 200, "en", true)
	ctx := context.Background()

	f.tg.NotifyBalanceLow(ctx, 1, 0.5, 5, "https://example.com/pay")
	require.Contains(t, f.api.sentTo(100)[0], "余额不足")
	require.Contains(t, f.api.sentTo(100)[0], "https://example.com/pay")

	f.tg.NotifyAccountQuota(ctx, "acct-1", "anthropic", quotaDimDaily, 80, 100)
	require.Contains(t, f.api.sentTo(200)[0], "Daily quota: $80.00 used of $100.00, $20.00 left")
	require.Len(t, f.api.sentTo(100), 1, "users do not get admin alerts")

	value := 12.0
	f.tg.NotifyOpsAlert(ctx, &OpsAlertRule{Name: "Error rate", Severity: "P1", MetricType: "error_rate", Operator: ">", Threshold: 5}, &OpsAlertEvent{MetricValue: &value})
	require.Contains(t, f.api.sentTo(200)[1], "Error rate")

	var nilService *TelegramService
	require.False(t, nilService.AdminAlertsActive(ctx))
	nilService.NotifyBalanceLow(ctx, 1, 0, 0, "")
}

func TestTelegramNotificationsRespectToggles(t *testing.T) {
	f := newTGFixture(t)
	_, err := f.tg.UpdateSettings(context.Background(), TelegramBotSettingsInput{Enabled: true, BotToken: "123:secret"})
	require.NoError(t, err)
	f.bindings.link(1, 100, "en", false)
	f.bindings.link(2, 200, "en", true)

	f.tg.NotifyBalanceLow(context.Background(), 1, 0.5, 5, "")
	f.tg.NotifyAccountQuota(context.Background(), "a", "p", quotaDimDaily, 1, 2)
	require.Empty(t, f.api.sent)
	require.False(t, f.tg.AdminAlertsActive(context.Background()))
}

func TestBalanceNotifyCrossingReachesTelegram(t *testing.T) {
	f := newTGFixture(t)
	f.enable(t)
	f.bindings.link(1, 100, "en", false)

	s, repo := newBalanceNotifyServiceForTest()
	s.SetTelegramService(f.tg)
	repo.data[SettingKeyBalanceLowNotifyEnabled] = "true"
	repo.data[SettingKeyBalanceLowNotifyThreshold] = "10"

	// 12 -> 8 crosses below 10.
	s.CheckBalanceAfterDeduction(context.Background(), &User{ID: 1, BalanceNotifyEnabled: true}, 12, 4)
	require.Eventually(t, func() bool { return len(f.api.sentTo(100)) == 1 }, 2*time.Second, 10*time.Millisecond)
	require.Contains(t, f.api.sentTo(100)[0], "Balance: $8.00")
}

func TestOpsAlertTelegramSkipsRulesWithNotificationsOff(t *testing.T) {
	f := newTGFixture(t)
	f.enable(t)
	f.bindings.link(2, 200, "en", true)
	s := &OpsAlertEvaluatorService{}
	s.SetTelegramService(f.tg)

	s.maybeSendAlertTelegram(nil, &OpsAlertRule{Name: "quiet", NotifyEmail: false}, &OpsAlertEvent{})
	s.maybeSendAlertTelegram(nil, &OpsAlertRule{Name: "loud", NotifyEmail: true}, &OpsAlertEvent{})
	require.Eventually(t, func() bool { return len(f.api.sentTo(200)) == 1 }, 2*time.Second, 10*time.Millisecond)
	require.Contains(t, f.api.sentTo(200)[0], "loud")
}

func TestTelegramBroadcast(t *testing.T) {
	f := newTGFixture(t)
	f.enable(t)
	f.bindings.link(1, 100, "en", false)
	f.bindings.link(2, 200, "en", true)
	ctx := context.Background()

	_, err := f.tg.Broadcast(ctx, "   ")
	require.ErrorIs(t, err, ErrTelegramBroadcastText)
	_, err = f.tg.Broadcast(ctx, strings.Repeat("倍", TelegramBroadcastMaxRunes+1))
	require.ErrorIs(t, err, ErrTelegramBroadcastText)

	n, err := f.tg.Broadcast(ctx, strings.Repeat("倍", TelegramBroadcastMaxRunes))
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Eventually(t, func() bool { return !f.tg.broadcasting.Load() }, 2*time.Second, 10*time.Millisecond)
	require.Contains(t, f.api.sentTo(100)[0], "📢 TestSite")
	require.Len(t, f.api.sentTo(200), 1)
}

func TestFormatTelegramCount(t *testing.T) {
	require.Equal(t, "0", formatTelegramCount(0))
	require.Equal(t, "999", formatTelegramCount(999))
	require.Equal(t, "1,000", formatTelegramCount(1000))
	require.Equal(t, "-1,234,567", formatTelegramCount(-1234567))
}
