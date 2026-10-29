//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func insertTelegramTestUser(t *testing.T, ctx context.Context, role, status string) int64 {
	t.Helper()
	var id int64
	email := fmt.Sprintf("tg-%s-%d@example.com", role, time.Now().UnixNano())
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
INSERT INTO users (email, password_hash, role, status, balance, concurrency)
VALUES ($1, 'hash', $2, $3, 0, 1)
RETURNING id`, email, role, status).Scan(&id))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

func TestTelegramBindingRepositoryLinkFlow(t *testing.T) {
	ctx := context.Background()
	repo := NewTelegramBindingRepository(integrationDB)
	user := insertTelegramTestUser(t, ctx, service.RoleUser, service.StatusActive)
	admin := insertTelegramTestUser(t, ctx, service.RoleAdmin, service.StatusActive)
	disabled := insertTelegramTestUser(t, ctx, service.RoleUser, "disabled")
	now := time.Now().UTC()
	chat := now.UnixNano() // unique per run
	code := func(id int64, n int) string { return fmt.Sprintf("%016x%016x", id, now.UnixNano()+int64(n)) }

	b, err := repo.GetByUserID(ctx, user)
	require.NoError(t, err)
	require.Nil(t, b)

	// The first code is kept; later calls return it unchanged.
	userCode := code(user, 1)
	stored, err := repo.EnsureBindCode(ctx, user, userCode)
	require.NoError(t, err)
	require.Equal(t, userCode, stored)
	stored, err = repo.EnsureBindCode(ctx, user, code(user, 2))
	require.NoError(t, err)
	require.Equal(t, userCode, stored)
	b, err = repo.GetByUserID(ctx, user)
	require.NoError(t, err)
	require.Equal(t, userCode, b.BindCode)
	require.Zero(t, b.ChatID)

	_, err = repo.BindChat(ctx, "nope", chat, "u", "en", now)
	require.ErrorIs(t, err, service.ErrTelegramBindCodeInvalid)

	got, err := repo.BindChat(ctx, userCode, chat, "alice", "zh", now)
	require.NoError(t, err)
	require.Equal(t, user, got)
	b, err = repo.GetByChatID(ctx, chat)
	require.NoError(t, err)
	require.Equal(t, user, b.UserID)
	require.Equal(t, "alice", b.TelegramUsername)
	require.Equal(t, "zh", b.Language)
	require.NotNil(t, b.LinkedAt)

	// The code is reusable by the same chat, but cannot take the account to another chat.
	_, err = repo.BindChat(ctx, userCode, chat, "alice", "zh", now)
	require.NoError(t, err)
	_, err = repo.BindChat(ctx, userCode, chat+9, "mallory", "en", now)
	require.ErrorIs(t, err, service.ErrTelegramLinkedElsewhere)

	// The same Telegram chat moves to the admin account when it links there.
	adminCode, err := repo.EnsureBindCode(ctx, admin, code(admin, 1))
	require.NoError(t, err)
	_, err = repo.BindChat(ctx, adminCode, chat, "alice", "en", now)
	require.NoError(t, err)
	b, err = repo.GetByUserID(ctx, user)
	require.NoError(t, err)
	require.Zero(t, b.ChatID, "previous owner is unlinked")
	require.Equal(t, userCode, b.BindCode, "but keeps its code")

	// Reset replaces the code.
	newUserCode := code(user, 3)
	require.NoError(t, repo.ResetBindCode(ctx, user, newUserCode))
	_, err = repo.BindChat(ctx, userCode, chat+1, "", "en", now)
	require.ErrorIs(t, err, service.ErrTelegramBindCodeInvalid)
	_, err = repo.BindChat(ctx, newUserCode, chat+1, "", "en", now)
	require.NoError(t, err)

	// Targets skip disabled users and filter admins.
	disabledCode, err := repo.EnsureBindCode(ctx, disabled, code(disabled, 1))
	require.NoError(t, err)
	_, err = repo.BindChat(ctx, disabledCode, chat+2, "", "en", now)
	require.NoError(t, err)

	all, err := repo.ListTargets(ctx, false)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{admin, user}, targetUserIDs(all, admin, user, disabled))
	admins, err := repo.ListTargets(ctx, true)
	require.NoError(t, err)
	require.Equal(t, []int64{admin}, targetUserIDs(admins, admin, user, disabled))

	users, adminCount, err := repo.CountLinked(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, users, int64(2))
	require.GreaterOrEqual(t, adminCount, int64(1))

	// Unlinking clears the chat and keeps the code.
	require.NoError(t, repo.UnlinkChat(ctx, chat))
	b, err = repo.GetByUserID(ctx, admin)
	require.NoError(t, err)
	require.Zero(t, b.ChatID)
	require.Equal(t, adminCode, b.BindCode)
	require.NoError(t, repo.UnlinkUser(ctx, user))
	b, err = repo.GetByUserID(ctx, user)
	require.NoError(t, err)
	require.Zero(t, b.ChatID)
	require.Nil(t, b.LinkedAt)
	require.Equal(t, newUserCode, b.BindCode)
}

// targetUserIDs keeps only this test's users, since other tests share the database.
func targetUserIDs(targets []service.TelegramChatTarget, ids ...int64) []int64 {
	keep := map[int64]bool{}
	for _, id := range ids {
		keep[id] = true
	}
	var out []int64
	for _, t := range targets {
		if keep[t.UserID] {
			out = append(out, t.UserID)
		}
	}
	return out
}
