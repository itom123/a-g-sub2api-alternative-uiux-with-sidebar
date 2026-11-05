package service

import (
	"math"
	"time"
)

// TTFT P90 band for the business score. LLM time to first token is set mostly
// by the upstream model (prompt size, reasoning effort, prompt-cache hits), so
// the old 1–3s P99 band scored normal long-prompt traffic as broken.
const (
	opsHealthTTFTGoodMs = 5_000
	opsHealthTTFTBadMs  = 20_000
)

// computeDashboardHealthScore computes a 0-100 health score from the metrics returned by the dashboard overview.
//
// Design goals:
// - Backend-owned scoring (UI only displays).
// - Layered scoring: Business Health (70%) + Infrastructure Health (30%)
// - Avoids double-counting (e.g., DB failure affects both infra and business metrics)
// - Conservative + stable: penalize clear degradations; avoid overreacting to missing/idle data.
func computeDashboardHealthScore(now time.Time, overview *OpsDashboardOverview, jobs opsJobHeartbeatPolicies) int {
	if overview == nil {
		return 0
	}

	// Idle/no-data: avoid showing a "bad" score when there is no traffic.
	// UI can still render a gray/idle state based on QPS + error rate.
	if overview.RequestCountSLA <= 0 && overview.RequestCountTotal <= 0 && overview.ErrorCountTotal <= 0 {
		return 100
	}

	businessHealth := computeBusinessHealth(overview)
	infraHealth := computeInfraHealth(now, overview, jobs)

	// Weighted combination: 70% business + 30% infrastructure
	score := businessHealth*0.7 + infraHealth*0.3
	return int(math.Round(clampFloat64(score, 0, 100)))
}

// computeBusinessHealth calculates business health score (0-100)
// Components: Error Rate (50%) + TTFT (50%)
func computeBusinessHealth(overview *OpsDashboardOverview) float64 {
	// Error rate score: 1% → 100, 10% → 0 (linear)
	// Uses the client-visible (SLA) error rate only. The upstream error rate also
	// counts attempts that failed over and then succeeded on another account,
	// which clients never saw; it stays a diagnosis signal, not a score input.
	errorScore := 100.0
	errorPct := clampFloat64(overview.ErrorRate*100, 0, 100)
	if errorPct > 1.0 {
		if errorPct <= 10.0 {
			errorScore = (10.0 - errorPct) / 9.0 * 100
		} else {
			errorScore = 0
		}
	}

	// TTFT score: P90 5s → 100, 20s → 0 (linear). P90 tracks what most users
	// see without one huge prompt deciding the score.
	ttftScore := 100.0
	if overview.TTFT.P90 != nil {
		p90 := float64(*overview.TTFT.P90)
		if p90 > opsHealthTTFTGoodMs {
			if p90 <= opsHealthTTFTBadMs {
				ttftScore = (opsHealthTTFTBadMs - p90) / (opsHealthTTFTBadMs - opsHealthTTFTGoodMs) * 100
			} else {
				ttftScore = 0
			}
		}
	}

	// Weighted combination: 50% error rate + 50% TTFT
	return errorScore*0.5 + ttftScore*0.5
}

// computeInfraHealth calculates infrastructure health score (0-100)
// Components: Storage (40%) + Compute Resources (30%) + Background Jobs (30%)
func computeInfraHealth(now time.Time, overview *OpsDashboardOverview, jobs opsJobHeartbeatPolicies) float64 {
	// Storage score: DB critical, Redis less critical
	storageScore := 100.0
	if overview.SystemMetrics != nil {
		if overview.SystemMetrics.DBOK != nil && !*overview.SystemMetrics.DBOK {
			storageScore = 0 // DB failure is critical
		} else if overview.SystemMetrics.RedisOK != nil && !*overview.SystemMetrics.RedisOK {
			storageScore = 50 // Redis failure is degraded but not critical
		}
	}

	// Compute resources score: CPU + Memory
	computeScore := 100.0
	if overview.SystemMetrics != nil {
		cpuScore := 100.0
		if overview.SystemMetrics.CPUUsagePercent != nil {
			cpuPct := clampFloat64(*overview.SystemMetrics.CPUUsagePercent, 0, 100)
			if cpuPct > 80 {
				if cpuPct <= 100 {
					cpuScore = (100 - cpuPct) / 20 * 100
				} else {
					cpuScore = 0
				}
			}
		}

		memScore := 100.0
		if overview.SystemMetrics.MemoryUsagePercent != nil {
			memPct := clampFloat64(*overview.SystemMetrics.MemoryUsagePercent, 0, 100)
			if memPct > 85 {
				if memPct <= 100 {
					memScore = (100 - memPct) / 15 * 100
				} else {
					memScore = 0
				}
			}
		}

		computeScore = (cpuScore + memScore) / 2
	}

	// Background jobs score: a job counts as failed when its latest run failed
	// or it missed its own schedule (see opsJobHeartbeatPolicy).
	jobScore := 100.0
	failedJobs := 0
	totalJobs := 0
	for _, hb := range overview.JobHeartbeats {
		if hb == nil {
			continue
		}
		policy := jobs.policyFor(hb.JobName)
		if policy.skip {
			continue
		}
		totalJobs++
		if policy.failed(now, hb) {
			failedJobs++
		}
	}
	if totalJobs > 0 && failedJobs > 0 {
		jobScore = (1 - float64(failedJobs)/float64(totalJobs)) * 100
	}

	// Weighted combination
	return storageScore*0.4 + computeScore*0.3 + jobScore*0.3
}

func clampFloat64(v float64, min float64, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
