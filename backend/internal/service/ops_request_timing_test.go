//go:build unit

package service

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// opsSlowBody delivers its data after a delay, like a client still uploading.
type opsSlowBody struct {
	data  []byte
	delay time.Duration
}

func (b *opsSlowBody) Read(p []byte) (int, error) {
	if b.delay > 0 {
		time.Sleep(b.delay)
		b.delay = 0
	}
	if len(b.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}

func newOpsTimingTestContext(body io.Reader) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", body)
	return c
}

func TestOpsRequestTimingSplitsUploadFromProcessing(t *testing.T) {
	c := newOpsTimingTestContext(&opsSlowBody{data: []byte(`{"model":"m"}`), delay: 120 * time.Millisecond})
	// 200ms of gateway work before this point, then a body that takes 120ms to arrive.
	timing := StartOpsRequestTiming(c, time.Now().Add(-200*time.Millisecond))

	_, _, ok := timing.PreprocessingSplit()
	require.False(t, ok, "no split before pre-processing ends")

	body, err := io.ReadAll(c.Request.Body)
	require.NoError(t, err)
	require.Equal(t, `{"model":"m"}`, string(body))
	SetOpsLatencyMs(c, OpsAuthLatencyMsKey, 1)

	processingMs, uploadMs, ok := timing.PreprocessingSplit()
	require.True(t, ok)
	require.GreaterOrEqual(t, uploadMs, int64(120))
	require.Less(t, uploadMs, int64(120+150))
	// Without the subtraction pre-processing would be at least 320ms.
	require.GreaterOrEqual(t, processingMs, int64(200), "gateway work is kept")
	require.Less(t, processingMs, int64(200+100), "upload is taken out of pre-processing")
	v, _ := c.Get(OpsAuthLatencyMsKey)
	require.Equal(t, int64(1), v, "the handler's own auth latency is kept as given")
}

func TestOpsRequestTimingIgnoresReadsAfterPreprocessing(t *testing.T) {
	c := newOpsTimingTestContext(&opsSlowBody{data: []byte("streamed"), delay: 40 * time.Millisecond})
	timing := StartOpsRequestTiming(c, time.Now())
	SetOpsLatencyMs(c, OpsAuthLatencyMsKey, 0)
	before, upload, ok := timing.PreprocessingSplit()
	require.True(t, ok)
	require.Zero(t, upload)

	// A body streamed to the upstream after routing is not part of the split.
	_, err := io.ReadAll(c.Request.Body)
	require.NoError(t, err)
	after, upload, _ := timing.PreprocessingSplit()
	require.Equal(t, before, after)
	require.Zero(t, upload)

	// Only the auth mark ends pre-processing.
	SetOpsLatencyMs(c, OpsRoutingLatencyMsKey, 5)
	again, _, _ := timing.PreprocessingSplit()
	require.Equal(t, before, again)
}

func TestOpsRequestTimingKeepsEmptyBodiesAndErrors(t *testing.T) {
	c := newOpsTimingTestContext(nil)
	require.Equal(t, http.NoBody, c.Request.Body)
	StartOpsRequestTiming(c, time.Now())
	require.Equal(t, http.NoBody, c.Request.Body, "NoBody stays comparable")

	c = newOpsTimingTestContext(nil)
	c.Request.Body = nil
	StartOpsRequestTiming(c, time.Now())
	require.Nil(t, c.Request.Body)

	// The body-size limit error still reaches handlers unchanged.
	c = newOpsTimingTestContext(strings.NewReader(strings.Repeat("x", 64)))
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16)
	StartOpsRequestTiming(c, time.Now())
	_, err := io.ReadAll(c.Request.Body)
	var maxErr *http.MaxBytesError
	require.True(t, errors.As(err, &maxErr))
	require.Equal(t, int64(16), maxErr.Limit)

	// Without the ops middleware nothing is timed and nothing breaks.
	c = newOpsTimingTestContext(strings.NewReader("{}"))
	SetOpsLatencyMs(c, OpsAuthLatencyMsKey, 3)
	require.Nil(t, opsRequestTimingFrom(c))
	var none *OpsRequestTiming
	_, _, ok := none.PreprocessingSplit()
	require.False(t, ok)
}
