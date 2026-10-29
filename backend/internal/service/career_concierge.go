package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

var (
	ErrCareerProfileNotFound   = errors.New("career concierge profile not found")
	ErrCareerTelegramNotPaired = errors.New("telegram is not paired")
	ErrCareerPairingInvalid    = errors.New("career concierge pairing code is invalid or expired")
	ErrCareerScheduleNotFound  = errors.New("career concierge schedule not found")
)

const careerPairingTTL = 15 * time.Minute

var careerCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

type CareerProfile struct {
	UserID               int64      `json:"user_id"`
	DisplayName          string     `json:"display_name"`
	Roles                string     `json:"roles"`
	Locations            string     `json:"locations"`
	RemoteOK             bool       `json:"remote_ok"`
	SalaryMin            float64    `json:"salary_min"`
	Notes                string     `json:"notes"`
	Timezone             string     `json:"timezone"`
	TelegramPaired       bool       `json:"telegram_paired"`
	TelegramPairedAt     *time.Time `json:"telegram_paired_at,omitempty"`
	UpdatedAt            time.Time  `json:"updated_at"`
	telegramChatIDUnsafe string
}

type CareerProfileInput struct {
	DisplayName string  `json:"display_name"`
	Roles       string  `json:"roles"`
	Locations   string  `json:"locations"`
	RemoteOK    bool    `json:"remote_ok"`
	SalaryMin   float64 `json:"salary_min"`
	Notes       string  `json:"notes"`
	Timezone    string  `json:"timezone"`
}

type CareerInternalProfile struct {
	Profile        CareerProfile `json:"profile"`
	TelegramChatID string        `json:"telegram_chat_id"`
}

func (s *OpenClawService) GetCareerInternalProfile(ctx context.Context, userID, scheduleID int64) (CareerInternalProfile, error) {
	var enabled bool
	err := s.db.QueryRowContext(ctx, `SELECT enabled FROM career_concierge_schedules WHERE id=$1 AND user_id=$2`, scheduleID, userID).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !enabled) {
		return CareerInternalProfile{}, ErrCareerScheduleNotFound
	}
	if err != nil {
		return CareerInternalProfile{}, err
	}
	profile, err := s.GetCareerProfile(ctx, userID)
	if err != nil {
		return CareerInternalProfile{}, err
	}
	if !profile.TelegramPaired || profile.telegramChatIDUnsafe == "" {
		return CareerInternalProfile{}, ErrCareerTelegramNotPaired
	}
	return CareerInternalProfile{Profile: profile, TelegramChatID: profile.telegramChatIDUnsafe}, nil
}

type CareerPairing struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

type CareerSchedule struct {
	ID             int64     `json:"id"`
	UserID         int64     `json:"user_id"`
	CronExpression string    `json:"cron_expression"`
	Timezone       string    `json:"timezone"`
	Enabled        bool      `json:"enabled"`
	OpenClawJobID  string    `json:"openclaw_job_id,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CareerScheduleInput struct {
	CronExpression string `json:"cron_expression"`
	Timezone       string `json:"timezone"`
	Enabled        *bool  `json:"enabled"`
}

func (s *OpenClawService) GetCareerProfile(ctx context.Context, userID int64) (CareerProfile, error) {
	var p CareerProfile
	var pairedAt *time.Time
	var chatID sql.NullString
	err := s.db.QueryRowContext(ctx, `
        SELECT user_id, display_name, roles, locations, remote_ok, salary_min, notes,
               timezone, telegram_chat_id, telegram_chat_id IS NOT NULL, telegram_paired_at, updated_at
        FROM career_concierge_profiles WHERE user_id = $1`, userID).
		Scan(&p.UserID, &p.DisplayName, &p.Roles, &p.Locations, &p.RemoteOK, &p.SalaryMin,
			&p.Notes, &p.Timezone, &chatID, &p.TelegramPaired, &pairedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return CareerProfile{UserID: userID, RemoteOK: true, Timezone: "Europe/London"}, nil
	}
	if err != nil {
		return CareerProfile{}, err
	}
	p.TelegramPairedAt = pairedAt
	if chatID.Valid {
		p.telegramChatIDUnsafe = chatID.String
	}
	return p, nil
}

func (s *OpenClawService) UpdateCareerProfile(ctx context.Context, userID int64, in CareerProfileInput) (CareerProfile, error) {
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.Roles = strings.TrimSpace(in.Roles)
	in.Locations = strings.TrimSpace(in.Locations)
	in.Notes = strings.TrimSpace(in.Notes)
	if len([]rune(in.DisplayName)) > 120 || len([]rune(in.Roles)) > 2000 || len([]rune(in.Locations)) > 2000 || len([]rune(in.Notes)) > 8000 {
		return CareerProfile{}, errors.New("career profile field is too long")
	}
	if in.SalaryMin < 0 {
		return CareerProfile{}, errors.New("salary_min must be non-negative")
	}
	if in.Timezone == "" {
		in.Timezone = "Europe/London"
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return CareerProfile{}, errors.New("invalid timezone")
	}
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO career_concierge_profiles
            (user_id, display_name, roles, locations, remote_ok, salary_min, notes, timezone)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
        ON CONFLICT (user_id) DO UPDATE SET
            display_name=EXCLUDED.display_name, roles=EXCLUDED.roles,
            locations=EXCLUDED.locations, remote_ok=EXCLUDED.remote_ok,
            salary_min=EXCLUDED.salary_min, notes=EXCLUDED.notes,
            timezone=EXCLUDED.timezone, updated_at=NOW()`,
		userID, in.DisplayName, in.Roles, in.Locations, in.RemoteOK, in.SalaryMin, in.Notes, in.Timezone)
	if err != nil {
		return CareerProfile{}, err
	}
	return s.GetCareerProfile(ctx, userID)
}

func (s *OpenClawService) CreateCareerPairing(ctx context.Context, userID int64) (CareerPairing, error) {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return CareerPairing{}, err
	}
	code := strings.ToUpper(hex.EncodeToString(raw))
	hash := sha256.Sum256([]byte(code))
	expires := time.Now().UTC().Add(careerPairingTTL)
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO career_concierge_profiles (user_id, remote_ok, timezone, pairing_hash, pairing_expires_at)
        VALUES ($1, TRUE, 'Europe/London', $2, $3)
        ON CONFLICT (user_id) DO UPDATE SET pairing_hash=$2, pairing_expires_at=$3, updated_at=NOW()`,
		userID, hex.EncodeToString(hash[:]), expires)
	if err != nil {
		return CareerPairing{}, err
	}
	return CareerPairing{Code: code, ExpiresAt: expires}, nil
}

func (s *OpenClawService) UnpairCareerTelegram(ctx context.Context, userID int64) error {
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(openclaw_job_id,'') FROM career_concierge_schedules WHERE user_id=$1 AND enabled=TRUE`, userID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var jobIDs []string
	for rows.Next() {
		var jobID string
		if scanErr := rows.Scan(&jobID); scanErr != nil {
			return scanErr
		}
		if jobID != "" {
			jobIDs = append(jobIDs, jobID)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if os.Getenv("OPENCLAW_CONCIERGE_ENABLED") == "1" {
		for _, jobID := range jobIDs {
			_ = removeCareerCron(ctx, jobID)
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE career_concierge_schedules SET enabled=FALSE,updated_at=NOW() WHERE user_id=$1`, userID); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE career_concierge_profiles SET telegram_chat_id=NULL, telegram_paired_at=NULL, pairing_hash=NULL, pairing_expires_at=NULL, updated_at=NOW() WHERE user_id=$1`, userID)
	return err
}

func (s *OpenClawService) PairCareerTelegram(ctx context.Context, code, chatID string) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	chatID = strings.TrimSpace(chatID)
	chatNumber, parseErr := strconv.ParseInt(chatID, 10, 64)
	if len(code) != 12 || parseErr != nil || chatNumber <= 0 {
		return ErrCareerPairingInvalid
	}
	hash := sha256.Sum256([]byte(code))
	result, err := s.db.ExecContext(ctx, `
        UPDATE career_concierge_profiles
        SET telegram_chat_id=$1, telegram_paired_at=NOW(), pairing_hash=NULL,
            pairing_expires_at=NULL, updated_at=NOW()
        WHERE pairing_hash=$2 AND pairing_expires_at > NOW()`, chatID, hex.EncodeToString(hash[:]))
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrCareerPairingInvalid
	}
	return nil
}

func (s *OpenClawService) ListCareerSchedules(ctx context.Context, userID int64) ([]CareerSchedule, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id,user_id,cron_expression,timezone,enabled,COALESCE(openclaw_job_id,''),COALESCE(last_error,''),created_at,updated_at
        FROM career_concierge_schedules WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []CareerSchedule
	for rows.Next() {
		var v CareerSchedule
		if err := rows.Scan(&v.ID, &v.UserID, &v.CronExpression, &v.Timezone, &v.Enabled, &v.OpenClawJobID, &v.LastError, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *OpenClawService) CreateCareerSchedule(ctx context.Context, userID int64, in CareerScheduleInput) (CareerSchedule, error) {
	in.CronExpression = strings.TrimSpace(in.CronExpression)
	in.Timezone = strings.TrimSpace(in.Timezone)
	if in.Timezone == "" {
		in.Timezone = "Europe/London"
	}
	if _, err := careerCronParser.Parse(in.CronExpression); err != nil {
		return CareerSchedule{}, errors.New("invalid cron expression")
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return CareerSchedule{}, errors.New("invalid timezone")
	}
	p, err := s.GetCareerProfile(ctx, userID)
	if err != nil {
		return CareerSchedule{}, err
	}
	if !p.TelegramPaired {
		return CareerSchedule{}, ErrCareerTelegramNotPaired
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	var v CareerSchedule
	err = s.db.QueryRowContext(ctx, `
        INSERT INTO career_concierge_schedules (user_id,cron_expression,timezone,enabled)
        VALUES ($1,$2,$3,$4)
        RETURNING id,user_id,cron_expression,timezone,enabled,COALESCE(openclaw_job_id,''),COALESCE(last_error,''),created_at,updated_at`, userID, in.CronExpression, in.Timezone, enabled).
		Scan(&v.ID, &v.UserID, &v.CronExpression, &v.Timezone, &v.Enabled, &v.OpenClawJobID, &v.LastError, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return CareerSchedule{}, err
	}
	if enabled {
		var jobID string
		runErr := errors.New("career concierge scheduling is disabled")
		if os.Getenv("OPENCLAW_CONCIERGE_ENABLED") == "1" {
			jobID, runErr = registerCareerCron(ctx, v, p)
		}
		if runErr != nil {
			v.Enabled = false
			v.LastError = runErr.Error()
			_, err = s.db.ExecContext(ctx, `UPDATE career_concierge_schedules SET enabled=FALSE,last_error=$1,updated_at=NOW() WHERE id=$2`, v.LastError, v.ID)
		} else {
			v.OpenClawJobID = jobID
			_, err = s.db.ExecContext(ctx, `UPDATE career_concierge_schedules SET openclaw_job_id=$1,updated_at=NOW() WHERE id=$2`, jobID, v.ID)
		}
		if err != nil {
			if jobID != "" {
				_ = removeCareerCron(ctx, jobID)
			}
			return CareerSchedule{}, err
		}
	}
	return v, nil
}

func (s *OpenClawService) DeleteCareerSchedule(ctx context.Context, userID, id int64) error {
	var jobID string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(openclaw_job_id,'') FROM career_concierge_schedules WHERE id=$1 AND user_id=$2`, id, userID).Scan(&jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCareerScheduleNotFound
	}
	if err != nil {
		return err
	}
	// Disable first: a leftover gateway job must not send after deletion.
	if _, err := s.db.ExecContext(ctx, `UPDATE career_concierge_schedules SET enabled=FALSE,updated_at=NOW() WHERE id=$1 AND user_id=$2`, id, userID); err != nil {
		return err
	}
	if jobID != "" && os.Getenv("OPENCLAW_CONCIERGE_ENABLED") == "1" {
		if err := removeCareerCron(ctx, jobID); err != nil {
			return errors.New("schedule disabled; gateway cleanup failed, retry removal")
		}
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM career_concierge_schedules WHERE id=$1 AND user_id=$2`, id, userID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrCareerScheduleNotFound
	}
	return nil
}

func registerCareerCron(ctx context.Context, schedule CareerSchedule, profile CareerProfile) (string, error) {
	runner := strings.TrimSpace(os.Getenv("OPENCLAW_CONCIERGE_RUNNER"))
	if runner == "" {
		runner = "/usr/local/lib/openclaw/career-concierge/run.mjs"
	}
	argv, _ := json.Marshal([]string{"node", runner, "--user-id", strconv.FormatInt(profile.UserID, 10), "--schedule-id", strconv.FormatInt(schedule.ID, 10)})
	args := []string{"cron", "add", "--no-deliver", "--cron", schedule.CronExpression, "--tz", schedule.Timezone, "--command-argv", string(argv), "--name", "career-" + strconv.FormatInt(schedule.ID, 10), "--declaration-key", "career:" + strconv.FormatInt(schedule.ID, 10), "--json"}
	out, err := exec.CommandContext(ctx, openClawBinary(), args...).CombinedOutput() // #nosec G702 -- Operator-configured binary with separate argv, no shell.
	if err != nil {
		return "", fmt.Errorf("openclaw cron registration failed: %w", err)
	}
	var payload map[string]any
	if json.Unmarshal(out, &payload) != nil {
		return "", errors.New("openclaw returned an invalid cron response")
	}
	for _, key := range []string{"id", "job_id"} {
		if value, ok := payload[key].(string); ok && value != "" {
			return value, nil
		}
	}
	if job, ok := payload["job"].(map[string]any); ok {
		if value, ok := job["id"].(string); ok && value != "" {
			return value, nil
		}
	}
	return "", errors.New("openclaw returned no cron job id")
}

func removeCareerCron(ctx context.Context, id string) error {
	return exec.CommandContext(ctx, openClawBinary(), "cron", "rm", id, "--json").Run()
}
func openClawBinary() string {
	if value := strings.TrimSpace(os.Getenv("OPENCLAW_BIN")); value != "" {
		return value
	}
	return "openclaw"
}
