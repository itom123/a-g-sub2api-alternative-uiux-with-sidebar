package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// telegramBindingRepository stores user ↔ Telegram chat links (raw SQL).
type telegramBindingRepository struct {
	db *sql.DB
}

func NewTelegramBindingRepository(db *sql.DB) service.TelegramBindingRepository {
	return &telegramBindingRepository{db: db}
}

const telegramBindingColumns = `user_id, bind_code, COALESCE(chat_id, 0), telegram_username, language, linked_at`

func scanTelegramBinding(row *sql.Row) (*service.TelegramBinding, error) {
	var b service.TelegramBinding
	var linkedAt sql.NullTime
	if err := row.Scan(&b.UserID, &b.BindCode, &b.ChatID, &b.TelegramUsername, &b.Language, &linkedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if linkedAt.Valid {
		t := linkedAt.Time
		b.LinkedAt = &t
	}
	return &b, nil
}

func (r *telegramBindingRepository) GetByUserID(ctx context.Context, userID int64) (*service.TelegramBinding, error) {
	return scanTelegramBinding(r.db.QueryRowContext(ctx,
		`SELECT `+telegramBindingColumns+` FROM telegram_bindings WHERE user_id = $1`, userID))
}

func (r *telegramBindingRepository) GetByChatID(ctx context.Context, chatID int64) (*service.TelegramBinding, error) {
	return scanTelegramBinding(r.db.QueryRowContext(ctx,
		`SELECT `+telegramBindingColumns+` FROM telegram_bindings WHERE chat_id = $1`, chatID))
}

func (r *telegramBindingRepository) EnsureBindCode(ctx context.Context, userID int64, code string) (string, error) {
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO telegram_bindings (user_id, bind_code) VALUES ($1, $2)
		ON CONFLICT (user_id) DO NOTHING`, userID, code); err != nil {
		return "", err
	}
	var stored string
	err := r.db.QueryRowContext(ctx, `SELECT bind_code FROM telegram_bindings WHERE user_id = $1`, userID).Scan(&stored)
	return stored, err
}

func (r *telegramBindingRepository) ResetBindCode(ctx context.Context, userID int64, code string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO telegram_bindings (user_id, bind_code) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET bind_code = EXCLUDED.bind_code, updated_at = NOW()`,
		userID, code)
	return err
}

// unlinkSet clears a chat link but keeps the user's bind code.
const unlinkSet = `chat_id = NULL, telegram_username = '', language = '', linked_at = NULL, updated_at = NOW()`

func (r *telegramBindingRepository) BindChat(ctx context.Context, code string, chatID int64, username, language string, now time.Time) (userID int64, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var linkedChat int64
	err = tx.QueryRowContext(ctx, `
		SELECT user_id, COALESCE(chat_id, 0) FROM telegram_bindings
		WHERE bind_code = $1
		FOR UPDATE`, code).Scan(&userID, &linkedChat)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, service.ErrTelegramBindCodeInvalid
	}
	if err != nil {
		return 0, err
	}
	if linkedChat != 0 && linkedChat != chatID {
		return 0, service.ErrTelegramLinkedElsewhere
	}
	// One Telegram chat belongs to one account: move it off any previous owner.
	if _, err = tx.ExecContext(ctx,
		`UPDATE telegram_bindings SET `+unlinkSet+` WHERE chat_id = $1 AND user_id <> $2`, chatID, userID); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE telegram_bindings
		SET chat_id = $2, telegram_username = $3, language = $4, linked_at = $5, updated_at = NOW()
		WHERE user_id = $1`, userID, chatID, username, language, now.UTC()); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return userID, nil
}

func (r *telegramBindingRepository) UnlinkUser(ctx context.Context, userID int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE telegram_bindings SET `+unlinkSet+` WHERE user_id = $1`, userID)
	return err
}

func (r *telegramBindingRepository) UnlinkChat(ctx context.Context, chatID int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE telegram_bindings SET `+unlinkSet+` WHERE chat_id = $1`, chatID)
	return err
}

func (r *telegramBindingRepository) ListTargets(ctx context.Context, adminsOnly bool) ([]service.TelegramChatTarget, error) {
	query := `
		SELECT b.user_id, b.chat_id, b.language
		FROM telegram_bindings b
		JOIN users u ON u.id = b.user_id
		WHERE b.chat_id IS NOT NULL AND u.deleted_at IS NULL AND u.status = $1`
	args := []any{service.StatusActive}
	if adminsOnly {
		query += ` AND u.role = $2`
		args = append(args, service.RoleAdmin)
	}
	query += ` ORDER BY b.user_id`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []service.TelegramChatTarget
	for rows.Next() {
		var t service.TelegramChatTarget
		if err := rows.Scan(&t.UserID, &t.ChatID, &t.Language); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *telegramBindingRepository) CountLinked(ctx context.Context) (users int64, admins int64, err error) {
	err = r.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE u.role = $2)
		FROM telegram_bindings b
		JOIN users u ON u.id = b.user_id
		WHERE b.chat_id IS NOT NULL AND u.deleted_at IS NULL AND u.status = $1`,
		service.StatusActive, service.RoleAdmin).Scan(&users, &admins)
	return users, admins, err
}
