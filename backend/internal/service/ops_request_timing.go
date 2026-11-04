package service

import (
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// opsRequestTimingKey stores the *OpsRequestTiming installed by the ops middleware.
const opsRequestTimingKey = "ops_request_timing"

// OpsRequestTiming splits a gateway request's time before routing into client
// upload (time spent waiting for request-body bytes) and the gateway's own
// pre-processing. It starts when the request enters the ops middleware, so it
// also covers body reads in the routing middleware that runs before the
// handler (model allowlist, composite routing). Pre-processing ends at the
// auth latency mark (SetOpsLatencyMs with OpsAuthLatencyMsKey).
type OpsRequestTiming struct {
	start time.Time
	// readNs is the time spent inside request-body Read calls so far. Bodies
	// may be read from other goroutines, hence atomics.
	readNs atomic.Int64

	marked       atomic.Bool
	processingMs atomic.Int64
	uploadMs     atomic.Int64
}

// StartOpsRequestTiming begins timing at start and routes the request body
// through a reader that measures how long reads wait for data.
func StartOpsRequestTiming(c *gin.Context, start time.Time) *OpsRequestTiming {
	if c == nil {
		return nil
	}
	t := &OpsRequestTiming{start: start}
	// Keep nil and NoBody as they are: handlers compare against them.
	if c.Request != nil && c.Request.Body != nil && c.Request.Body != http.NoBody {
		c.Request.Body = &opsTimedBody{ReadCloser: c.Request.Body, timing: t}
	}
	c.Set(opsRequestTimingKey, t)
	return t
}

func opsRequestTimingFrom(c *gin.Context) *OpsRequestTiming {
	if c == nil {
		return nil
	}
	v, ok := c.Get(opsRequestTimingKey)
	if !ok {
		return nil
	}
	t, _ := v.(*OpsRequestTiming)
	return t
}

// markPreprocessingDone snapshots the split at the end of pre-processing.
// Body reads after this point (streamed uploads) are not counted.
func (t *OpsRequestTiming) markPreprocessingDone(now time.Time) {
	upload := time.Duration(t.readNs.Load())
	processing := now.Sub(t.start) - upload
	if processing < 0 {
		processing = 0
	}
	t.uploadMs.Store(upload.Milliseconds())
	t.processingMs.Store(processing.Milliseconds())
	t.marked.Store(true)
}

// PreprocessingSplit returns the gateway's pre-processing time and the time
// spent waiting for the request body, both in milliseconds, up to the end of
// pre-processing. ok is false when the request never got that far.
func (t *OpsRequestTiming) PreprocessingSplit() (processingMs, uploadMs int64, ok bool) {
	if t == nil || !t.marked.Load() {
		return 0, 0, false
	}
	return t.processingMs.Load(), t.uploadMs.Load(), true
}

// opsTimedBody adds the time each Read blocks to the request's upload time.
// Errors pass through unchanged (handlers match *http.MaxBytesError).
type opsTimedBody struct {
	io.ReadCloser
	timing *OpsRequestTiming
}

func (b *opsTimedBody) Read(p []byte) (int, error) {
	start := time.Now()
	n, err := b.ReadCloser.Read(p)
	b.timing.readNs.Add(int64(time.Since(start)))
	return n, err
}
