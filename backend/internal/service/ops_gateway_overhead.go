package service

import (
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// opsGatewayOverheadWindowMinutes is how much history the recorder keeps.
	opsGatewayOverheadWindowMinutes = 60
	// opsGatewayOverheadSamplesPerMinute caps memory per minute; busier minutes
	// keep a uniform random sample (reservoir sampling).
	opsGatewayOverheadSamplesPerMinute = 2048
)

// OpsGatewayOverhead is the gateway's own time per request before the upstream
// call: pre-processing (API-key checks, body parsing, policy and billing
// checks) plus routing (queueing and account selection). It separates what the
// gateway controls from the upstream share of TTFT. Time spent waiting for the
// client to upload the request body is not gateway work and is reported
// separately (Upload*): multi-MB prompts over a slow link would otherwise
// dominate the percentiles. It comes from this instance's memory, covers at
// most the last hour and resets on restart.
type OpsGatewayOverhead struct {
	WindowMinutes int   `json:"window_minutes"`
	SampleCount   int64 `json:"sample_count"`

	P50 *int `json:"p50_ms"`
	P90 *int `json:"p90_ms"`
	P99 *int `json:"p99_ms"`
	Max *int `json:"max_ms"`

	ProcessingP90 *int `json:"processing_p90_ms"`
	RoutingP90    *int `json:"routing_p90_ms"`

	UploadP50 *int `json:"upload_p50_ms"`
	UploadP90 *int `json:"upload_p90_ms"`
	UploadMax *int `json:"upload_max_ms"`
}

type opsGatewayOverheadSample struct {
	processingMs int32
	uploadMs     int32
	routingMs    int32
	// hasRouting is false when the request failed before account routing.
	hasRouting bool
}

type opsGatewayOverheadBucket struct {
	minute  int64 // Unix minute; 0 means unused
	seen    int64
	samples []opsGatewayOverheadSample
}

// opsGatewayOverheadRecorder keeps per-minute samples in a ring buffer.
type opsGatewayOverheadRecorder struct {
	mu      sync.Mutex
	buckets [opsGatewayOverheadWindowMinutes]opsGatewayOverheadBucket
}

func (r *opsGatewayOverheadRecorder) record(at time.Time, processingMs, uploadMs int64, routingMs *int64) {
	sample := opsGatewayOverheadSample{processingMs: clampOpsLatencyMs(processingMs), uploadMs: clampOpsLatencyMs(uploadMs)}
	if routingMs != nil {
		sample.routingMs = clampOpsLatencyMs(*routingMs)
		sample.hasRouting = true
	}
	minute := at.Unix() / 60

	r.mu.Lock()
	defer r.mu.Unlock()
	bucket := &r.buckets[minute%opsGatewayOverheadWindowMinutes]
	if bucket.minute != minute {
		bucket.minute = minute
		bucket.seen = 0
		bucket.samples = bucket.samples[:0]
	}
	bucket.seen++
	if len(bucket.samples) < opsGatewayOverheadSamplesPerMinute {
		bucket.samples = append(bucket.samples, sample)
		return
	}
	if i := rand.Int64N(bucket.seen); i < opsGatewayOverheadSamplesPerMinute {
		bucket.samples[i] = sample
	}
}

// summary covers the minutes of [start, end] that are still in the window.
func (r *opsGatewayOverheadRecorder) summary(now, start, end time.Time) *OpsGatewayOverhead {
	windowStart := now.Add(-opsGatewayOverheadWindowMinutes * time.Minute)
	if start.After(windowStart) {
		windowStart = start
	}
	if end.After(now) {
		end = now
	}
	out := &OpsGatewayOverhead{}
	if !windowStart.Before(end) {
		return out
	}
	out.WindowMinutes = int(math.Ceil(end.Sub(windowStart).Minutes()))
	firstMinute, lastMinute := windowStart.Unix()/60, end.Unix()/60
	// The oldest ring slot may still hold the minute that is just leaving the window.
	if oldest := now.Unix()/60 - opsGatewayOverheadWindowMinutes + 1; firstMinute < oldest {
		firstMinute = oldest
	}

	var totals, processing, routings, uploads []int
	r.mu.Lock()
	for i := range r.buckets {
		bucket := &r.buckets[i]
		if bucket.minute == 0 || bucket.minute < firstMinute || bucket.minute > lastMinute {
			continue
		}
		out.SampleCount += bucket.seen
		for _, s := range bucket.samples {
			total := int(s.processingMs)
			processing = append(processing, int(s.processingMs))
			uploads = append(uploads, int(s.uploadMs))
			if s.hasRouting {
				total += int(s.routingMs)
				routings = append(routings, int(s.routingMs))
			}
			totals = append(totals, total)
		}
	}
	r.mu.Unlock()

	sort.Ints(totals)
	sort.Ints(processing)
	sort.Ints(routings)
	sort.Ints(uploads)
	out.P50 = opsNearestRank(totals, 0.50)
	out.P90 = opsNearestRank(totals, 0.90)
	out.P99 = opsNearestRank(totals, 0.99)
	out.Max = opsNearestRank(totals, 1)
	out.ProcessingP90 = opsNearestRank(processing, 0.90)
	out.RoutingP90 = opsNearestRank(routings, 0.90)
	out.UploadP50 = opsNearestRank(uploads, 0.50)
	out.UploadP90 = opsNearestRank(uploads, 0.90)
	out.UploadMax = opsNearestRank(uploads, 1)
	return out
}

// opsNearestRank returns the nearest-rank percentile of sorted values.
func opsNearestRank(sorted []int, p float64) *int {
	if len(sorted) == 0 {
		return nil
	}
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	if i < 0 {
		i = 0
	}
	v := sorted[i]
	return &v
}

func clampOpsLatencyMs(ms int64) int32 {
	if ms < 0 {
		return 0
	}
	if ms > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(ms)
}

// RecordGatewayOverhead records one gateway request's pre-upstream time:
// pre-processing without body upload, the upload itself, and routing.
// routingMs is nil when the request ended before account routing.
func (s *OpsService) RecordGatewayOverhead(at time.Time, processingMs, uploadMs int64, routingMs *int64) {
	if s == nil {
		return
	}
	s.gatewayOverhead.record(at, processingMs, uploadMs, routingMs)
}

// GatewayOverhead summarises recorded pre-upstream time for [start, end],
// limited to the last hour.
func (s *OpsService) GatewayOverhead(start, end time.Time) *OpsGatewayOverhead {
	if s == nil {
		return nil
	}
	return s.gatewayOverhead.summary(time.Now(), start, end)
}

// gatewayOverheadSummary is only meaningful for the unfiltered dashboard: the
// recorder does not keep platform or group.
func (s *OpsService) gatewayOverheadSummary(filter *OpsDashboardFilter) *OpsGatewayOverhead {
	if s == nil || filter == nil || strings.TrimSpace(filter.Platform) != "" || (filter.GroupID != nil && *filter.GroupID > 0) {
		return nil
	}
	return s.GatewayOverhead(filter.StartTime, filter.EndTime)
}
