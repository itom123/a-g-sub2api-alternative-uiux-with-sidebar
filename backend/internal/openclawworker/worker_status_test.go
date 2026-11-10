package openclawworker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const statusTenant = "u1-0123456789ab"

func statusCellJSON(state string, running bool) []byte {
	return []byte(fmt.Sprintf(`{"id":%q,"name":%q,"running":%t,"status":%q,"paused":false,"restarting":false,"dead":false,"ports":{"18789/tcp":[{"HostIp":"127.0.0.1","HostPort":"19100"}]}}`, strings.Repeat("a", 64), "openclaw-cell-"+statusTenant, running, state))
}

type statusExitError int

func (e statusExitError) Error() string { return "sensitive CLI stderr: no such container" }
func (e statusExitError) ExitCode() int { return int(e) }

// Model direct Podman observations, without requiring executables, a shell or
// fixed host ports. Assert the entire permitted read-only command surface.
func statusPodman(t *testing.T, existsErr error, inspect func(int) ([]byte, error)) func(context.Context, ...string) ([]byte, error) {
	t.Helper()
	calls := 0
	return func(ctx context.Context, args ...string) ([]byte, error) {
		t.Helper()
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 3*time.Second {
			t.Error("Podman observation is missing its bounded deadline")
		}
		switch {
		case reflect.DeepEqual(args, []string{"container", "exists", "openclaw-cell-" + statusTenant}):
			return nil, existsErr
		case reflect.DeepEqual(args, []string{"inspect", "--format", cellInspectFormat, "openclaw-cell-" + statusTenant}):
			calls++
			return inspect(calls)
		default:
			t.Errorf("unexpected command: %q", args)
			return nil, errors.New("unexpected command")
		}
	}
}

// Serve one HTTP exchange over a pipe. The dial seam still has to receive the
// exact validated loopback binding; no listeners or Linux-only shell stubs.
func statusHealthDial(t *testing.T, response string, requests *atomic.Int32) func(context.Context, string, string) (net.Conn, error) {
	t.Helper()
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != "127.0.0.1:19100" {
			t.Errorf("unsafe health dial: %s %s", network, address)
			return nil, errors.New("unexpected address")
		}
		client, server := net.Pipe()
		_ = client.SetDeadline(time.Now().Add(time.Second))
		_ = server.SetDeadline(time.Now().Add(time.Second))
		done := make(chan struct{})
		t.Cleanup(func() {
			_ = client.Close()
			_ = server.Close()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("health fixture did not stop")
			}
		})
		go func() {
			defer close(done)
			defer func() { _ = server.Close() }()
			req, err := http.ReadRequest(bufio.NewReader(server))
			if err != nil {
				return // The identity guard may close before sending HTTP.
			}
			defer func() { _ = req.Body.Close() }()
			requests.Add(1)
			if req.Method != http.MethodGet || req.URL.RequestURI() != "/healthz" || req.Host != "127.0.0.1:19100" || req.Header.Get("Authorization") != "" {
				t.Error("unexpected health request")
			}
			_, _ = fmt.Fprint(server, response)
		}()
		return client, nil
	}
}

func statusRequest(w *Worker, action string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/cells/"+statusTenant+"/"+action, nil)
	req.Header.Set("Authorization", "Bearer "+w.Token)
	rw := httptest.NewRecorder()
	w.ServeHTTP(rw, req)
	return rw
}

func TestWorkerStatusWhileMutationLocked(t *testing.T) {
	w := &Worker{
		Token: strings.Repeat("t", 32),
		// A status result must not depend on wrapper/registry availability.
		Binary: filepath.Join(t.TempDir(), "managed-wrapper-must-not-run"),
	}
	var reads atomic.Int32
	w.podmanRead = statusPodman(t, nil, func(int) ([]byte, error) {
		reads.Add(1)
		return statusCellJSON("running", true), nil
	})
	w.statusDial = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("unhealthy") }
	w.mu.Lock()
	defer w.mu.Unlock()
	rw := statusRequest(w, "status")
	var result Result
	if rw.Code != http.StatusOK || json.Unmarshal(rw.Body.Bytes(), &result) != nil || result.State != "running" || result.Port != 19100 || result.LatencyMS != nil {
		t.Fatalf("status under mutation lock: %d %s", rw.Code, rw.Body.String())
	}
	if rw.Header().Get("Cache-Control") != "no-store" {
		t.Error("status must not be cached")
	}
	for _, action := range []string{"create", "start", "stop", "delete", "dashboard", "devices", "configure", "enrollment", "connections/telegram", "connections/wechat", "connections/gws"} {
		rw := statusRequest(w, action)
		if rw.Code != http.StatusConflict {
			t.Errorf("second mutator %q: got %d, want 409", action, rw.Code)
		}
	}
	if reads.Load() != 2 { // Initial inspect plus recheck after failed health dial.
		t.Errorf("mutators ran while busy, or status missed recheck: %d reads", reads.Load())
	}
}

func TestWorkerStatusRuntimeStates(t *testing.T) {
	for _, test := range []struct {
		name       string
		state      string
		running    bool
		existsErr  error
		inspectErr error
		payload    []byte
		response   string
		dialErr    error
		want       string
		port       int
		latency    bool
		wantHTTP   int32
	}{
		{name: "running-healthy", state: "running", running: true, response: "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n", want: "running", port: 19100, latency: true, wantHTTP: 1},
		{name: "running-unhealthy", state: "running", running: true, response: "HTTP/1.1 503 Unavailable\r\nContent-Length: 0\r\n\r\n", want: "running", port: 19100, wantHTTP: 1},
		{name: "running-dial-failure", state: "running", running: true, dialErr: errors.New("connection refused"), want: "running", port: 19100},
		{name: "running-health-timeout", state: "running", running: true, dialErr: context.DeadlineExceeded, want: "running", port: 19100},
		{name: "running-malformed-http", state: "running", running: true, response: "not an HTTP response\r\n\r\n", want: "running", port: 19100, wantHTTP: 1},
		{name: "redirect-not-followed", state: "running", running: true, response: "HTTP/1.1 302 Found\r\nLocation: http://127.0.0.1:19101/escape\r\nContent-Length: 0\r\n\r\n", want: "running", port: 19100, wantHTTP: 1},
		{name: "exited", state: "exited", want: "stopped", port: 19100},
		{name: "stopped", state: "stopped", want: "stopped", port: 19100},
		{name: "stopped-without-active-binding", payload: []byte(strings.Replace(string(statusCellJSON("exited", false)), `"ports":{"18789/tcp":[{"HostIp":"127.0.0.1","HostPort":"19100"}]}`, `"ports":{}`, 1)), want: "stopped"},
		{name: "missing", existsErr: statusExitError(1), want: "missing"},
		{name: "missing-wrapped-exit", existsErr: fmt.Errorf("runtime: %w", statusExitError(1)), want: "missing"},
		{name: "wrong-container-name", payload: []byte(strings.Replace(string(statusCellJSON("running", true)), statusTenant, "u2-abcdef012345", 1))},
		{name: "invalid-container-id", payload: []byte(strings.Replace(string(statusCellJSON("running", true)), strings.Repeat("a", 64), strings.Repeat("z", 64), 1))},
		{name: "non-loopback-binding", payload: []byte(strings.Replace(string(statusCellJSON("running", true)), "127.0.0.1", "0.0.0.0", 1))},
		{name: "out-of-range-binding", payload: []byte(strings.Replace(string(statusCellJSON("running", true)), "19100", "19200", 1))},
		{name: "paused", payload: []byte(strings.Replace(string(statusCellJSON("running", true)), `"paused":false`, `"paused":true`, 1))},
		{name: "restarting", payload: []byte(strings.Replace(string(statusCellJSON("running", true)), `"restarting":false`, `"restarting":true`, 1))},
		{name: "storage-error-not-missing", existsErr: statusExitError(125)},
		{name: "unknown-exit-not-missing", existsErr: statusExitError(2)},
		{name: "permission-error-not-missing", existsErr: errors.New("permission denied: no such container; secret")},
		{name: "runtime-timeout-not-missing", existsErr: context.DeadlineExceeded},
		{name: "inspect-timeout-not-missing", inspectErr: context.DeadlineExceeded},
		{name: "inspect-error-not-missing", inspectErr: statusExitError(1)},
		{name: "malformed-inspect", payload: []byte(`{`)},
		{name: "empty-inspect", payload: []byte(`[]`)},
		{name: "multiple-inspect-results", payload: append(statusCellJSON("running", true), statusCellJSON("running", true)...)},
		{name: "starting-not-stopped", state: "created"},
		{name: "removing-not-stopped", state: "removing"},
		{name: "stopping-not-stopped", state: "stopping", running: true},
		{name: "unknown-not-stopped", state: "unknown"},
		{name: "contradictory-running-flag", state: "running"},
		{name: "contradictory-exited-flag", state: "exited", running: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			var dials atomic.Int32
			inspects := 0
			w := &Worker{Token: strings.Repeat("t", 32), Binary: filepath.Join(t.TempDir(), "must-not-run")}
			w.podmanRead = statusPodman(t, test.existsErr, func(call int) ([]byte, error) {
				inspects++
				if call > 1 && dials.Load() != 1 {
					t.Error("identity recheck did not follow the single fixed dial")
				}
				if test.payload != nil {
					return test.payload, test.inspectErr
				}
				return statusCellJSON(test.state, test.running), test.inspectErr
			})
			health := statusHealthDial(t, test.response, &requests)
			w.statusDial = func(ctx context.Context, network, address string) (net.Conn, error) {
				dials.Add(1)
				if test.dialErr != nil {
					return nil, test.dialErr
				}
				return health(ctx, network, address)
			}
			rw := statusRequest(w, "status")
			if test.want == "" {
				if rw.Code != http.StatusBadGateway || strings.Contains(rw.Body.String(), "sensitive") || strings.Contains(rw.Body.String(), "secret") || strings.Contains(rw.Body.String(), "no such") {
					t.Fatalf("expected redacted status error: %d %s", rw.Code, rw.Body.String())
				}
			} else {
				var result Result
				if rw.Code != http.StatusOK || json.Unmarshal(rw.Body.Bytes(), &result) != nil || result.State != test.want || result.Port != test.port || (result.LatencyMS != nil) != test.latency {
					t.Fatalf("unexpected status: %d %s", rw.Code, rw.Body.String())
				}
			}
			if requests.Load() != test.wantHTTP || dials.Load() > 1 {
				t.Errorf("unexpected health traffic: %d requests, %d dials", requests.Load(), dials.Load())
			}
			if test.want == "running" {
				wantInspects := 3
				if test.dialErr != nil {
					wantInspects = 2
				}
				if dials.Load() != 1 || inspects != wantInspects {
					t.Errorf("running observation missed guarded probe: %d dials, %d inspects", dials.Load(), inspects)
				}
			}
			if test.want != "running" && dials.Load() != 0 {
				t.Error("probed a non-running/invalid observation")
			}
			if test.existsErr != nil && inspects != 0 {
				t.Error("inspected after canonical absence or existence error")
			}
		})
	}
}

func TestWorkerStatusRejectsChangedOrAmbiguousIdentity(t *testing.T) {
	valid := string(statusCellJSON("running", true))
	for _, changed := range []struct {
		name string
		out  []byte
		err  error
	}{
		{name: "id", out: []byte(strings.Replace(valid, strings.Repeat("a", 64), strings.Repeat("b", 64), 1))},
		{name: "binding", out: []byte(strings.ReplaceAll(valid, "19100", "19101"))},
		{name: "name", out: []byte(strings.Replace(valid, statusTenant, "u2-abcdef012345", 1))},
		{name: "stopped", out: statusCellJSON("exited", false)},
		{name: "paused", out: []byte(strings.Replace(valid, `"paused":false`, `"paused":true`, 1))},
		{name: "malformed", out: []byte(`{`)},
		{name: "inspect-failure", err: statusExitError(1)},
	} {
		for _, phase := range []int{2, 3} {
			t.Run(fmt.Sprintf("%s-inspect-%d", changed.name, phase), func(t *testing.T) {
				var requests atomic.Int32
				w := &Worker{}
				w.podmanRead = statusPodman(t, nil, func(call int) ([]byte, error) {
					if call == phase {
						return changed.out, changed.err
					}
					return []byte(valid), nil
				})
				w.statusDial = statusHealthDial(t, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n", &requests)
				if result, err := w.execute(context.Background(), statusTenant, "status"); err == nil || result.State != "" {
					t.Fatalf("accepted changed identity: %+v %v", result, err)
				}
				if want := int32(phase - 2); requests.Load() != want {
					t.Errorf("HTTP sent before target verified: got %d, want %d", requests.Load(), want)
				}
			})
		}
	}
}

func TestWorkerStatusRechecksAfterFailedDial(t *testing.T) {
	w := &Worker{}
	w.podmanRead = statusPodman(t, nil, func(call int) ([]byte, error) {
		if call == 1 {
			return statusCellJSON("running", true), nil
		}
		return statusCellJSON("exited", false), nil
	})
	w.statusDial = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("connection refused") }
	if _, err := w.execute(context.Background(), statusTenant, "status"); err == nil {
		t.Fatal("stop during failed health dial was disguised as running")
	}
}

func TestWorkerStatusDeadlineAndCancellation(t *testing.T) {
	w := &Worker{Token: strings.Repeat("t", 32)}
	var dialed bool
	w.podmanRead = func(ctx context.Context, args ...string) ([]byte, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 3*time.Second {
			t.Error("runtime read is missing its three-second cap")
		}
		if args[0] == "container" {
			return nil, nil
		}
		return statusCellJSON("running", true), nil
	}
	w.statusDial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		dialed = true
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second {
			t.Error("health dial is missing its two-second cap")
		}
		return nil, errors.New("unhealthy")
	}
	if rw := statusRequest(w, "status"); rw.Code != http.StatusOK || !dialed {
		t.Fatalf("status deadline fixture: %d", rw.Code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.podmanRead = func(context.Context, ...string) ([]byte, error) {
		cancel()
		return nil, statusExitError(1)
	}
	if result, err := w.execute(ctx, statusTenant, "status"); err == nil || result.State == "missing" {
		t.Fatal("canceled absence check was reported as missing")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	w.podmanRead = func(ctx context.Context, _ ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if result, err := w.execute(ctx, statusTenant, "status"); err == nil || result.State != "" {
		t.Fatal("deadline was reported as a runtime state")
	}
}

func TestWorkerStatusHealthClientIsFixedAndReadOnly(t *testing.T) {
	target := gatewayCellTarget{ID: strings.Repeat("a", 64), Address: "127.0.0.1:19100"}
	client := cellHealthClient(target, nil)
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || !transport.DisableKeepAlives || !transport.DisableCompression {
		t.Fatal("health client must not use proxy discovery or pooled connections")
	}
	if err := client.CheckRedirect(&http.Request{}, nil); err != http.ErrUseLastResponse {
		t.Fatal("health client follows redirects")
	}
	if _, err := transport.DialContext(context.Background(), "tcp", "127.0.0.1:19101"); err == nil {
		t.Fatal("health transport accepted an unvalidated address")
	}
}

func TestWorkerStatusRejectsInvalidTenantBeforeRuntimeRead(t *testing.T) {
	w := &Worker{podmanRead: func(context.Context, ...string) ([]byte, error) {
		t.Error("invalid tenant reached Podman")
		return nil, nil
	}}
	if _, err := w.execute(context.Background(), "u1;unexpected", "status"); err == nil {
		t.Fatal("accepted invalid tenant")
	}
}
