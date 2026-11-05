package service

import (
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	// opsJobDefaultMaxAge applies to jobs without their own policy; by default
	// they run every minute.
	opsJobDefaultMaxAge = 15 * time.Minute
	// opsCronJobGrace is how long a cron job may run late before it is overdue.
	opsCronJobGrace = time.Hour
)

// opsJobHeartbeatPolicy says how long a background job may go without a
// successful run before the health score counts it as overdue.
type opsJobHeartbeatPolicy struct {
	// skip leaves the job out, e.g. when config or settings disable it and its
	// last heartbeat is simply old.
	skip bool
	// maxAge is the longest tolerated gap since the last success.
	maxAge time.Duration
	// nextRun, when set, replaces maxAge: the job is overdue once its first
	// scheduled run after the last success is more than opsCronJobGrace ago.
	nextRun func(after time.Time) time.Time
}

// opsJobHeartbeatPolicies maps job names to policies. Jobs missing from the
// map (including a nil map) fall back to defaultOpsJobHeartbeatPolicies.
type opsJobHeartbeatPolicies map[string]opsJobHeartbeatPolicy

// defaultOpsJobHeartbeatPolicies covers jobs that run far less often than the
// 15-minute default. Without them the daily cleanup and the hourly-run daily
// pre-aggregation counted as failed between runs, a constant ~3-point penalty.
var defaultOpsJobHeartbeatPolicies = opsJobHeartbeatPolicies{
	opsAggHourlyJobName: {maxAge: 3 * opsAggHourlyInterval},
	opsAggDailyJobName:  {maxAge: 3 * opsAggDailyInterval},
	opsCleanupJobName:   {maxAge: 26 * time.Hour},
}

func (p opsJobHeartbeatPolicies) policyFor(jobName string) opsJobHeartbeatPolicy {
	if policy, ok := p[jobName]; ok {
		return policy
	}
	if policy, ok := defaultOpsJobHeartbeatPolicies[jobName]; ok {
		return policy
	}
	return opsJobHeartbeatPolicy{maxAge: opsJobDefaultMaxAge}
}

// failed reports whether the job's latest run failed or it missed its schedule.
func (p opsJobHeartbeatPolicy) failed(now time.Time, hb *OpsJobHeartbeat) bool {
	if hb.LastErrorAt != nil && (hb.LastSuccessAt == nil || hb.LastErrorAt.After(*hb.LastSuccessAt)) {
		return true
	}
	if hb.LastSuccessAt == nil {
		return false
	}
	if p.nextRun != nil {
		if next := p.nextRun(*hb.LastSuccessAt); !next.IsZero() {
			return now.After(next.Add(opsCronJobGrace))
		}
	}
	maxAge := p.maxAge
	if maxAge <= 0 {
		maxAge = opsJobDefaultMaxAge
	}
	return now.Sub(*hb.LastSuccessAt) > maxAge
}

// opsJobHeartbeatPolicies reflects which background jobs are enabled and how
// often they run, so the health score only flags jobs that really missed a run.
// It reads the in-memory settings snapshot and does no I/O.
func (s *OpsService) opsJobHeartbeatPolicies() opsJobHeartbeatPolicies {
	policies := opsJobHeartbeatPolicies{}
	if s == nil {
		return policies
	}
	if s.cfg != nil && !s.cfg.Ops.Aggregation.Enabled {
		policies[opsAggHourlyJobName] = opsJobHeartbeatPolicy{skip: true}
		policies[opsAggDailyJobName] = opsJobHeartbeatPolicy{skip: true}
	}
	retention := s.OpsAdvancedSettingsSnapshot().DataRetention
	if !retention.CleanupEnabled {
		policies[opsCleanupJobName] = opsJobHeartbeatPolicy{skip: true}
		return policies
	}
	// The cleanup service prefers the settings schedule and falls back to the
	// config one. The snapshot cannot tell which is live, so judge the job
	// against whichever runs later.
	schedules := []string{retention.CleanupSchedule}
	if s.cfg != nil {
		schedules = append(schedules, s.cfg.Ops.Cleanup.Schedule)
	}
	if next := opsCronNextRunFunc(opsCleanupLocation(s.cfg), schedules...); next != nil {
		policies[opsCleanupJobName] = opsJobHeartbeatPolicy{nextRun: next}
	}
	return policies
}

// opsCronNextRunFunc returns the latest next run across the given cron specs,
// or nil when none of them parse.
func opsCronNextRunFunc(loc *time.Location, specs ...string) func(time.Time) time.Time {
	var schedules []interface{ Next(time.Time) time.Time }
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		schedule, err := opsCleanupCronParser.Parse(spec)
		if err != nil {
			continue
		}
		schedules = append(schedules, schedule)
	}
	if len(schedules) == 0 {
		return nil
	}
	return func(after time.Time) time.Time {
		var latest time.Time
		for _, schedule := range schedules {
			if next := schedule.Next(after.In(loc)); next.After(latest) {
				latest = next
			}
		}
		return latest
	}
}

// opsCleanupLocation is the time zone the cleanup cron runs in.
func opsCleanupLocation(cfg *config.Config) *time.Location {
	if cfg != nil && strings.TrimSpace(cfg.Timezone) != "" {
		if parsed, err := time.LoadLocation(strings.TrimSpace(cfg.Timezone)); err == nil && parsed != nil {
			return parsed
		}
	}
	return time.Local
}
