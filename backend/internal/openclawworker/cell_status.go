package openclawworker

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const cellInspectFormat = `{"id":{{json .ID}},"name":{{json .Name}},"running":{{json .State.Running}},"status":{{json .State.Status}},"paused":{{json .State.Paused}},"restarting":{{json .State.Restarting}},"dead":{{json .State.Dead}},"ports":{{json .NetworkSettings.Ports}}}`

var errCellUnavailable = errors.New("gateway cell unavailable")

type cellObservation struct {
	Target gatewayCellTarget
	State  string
	Port   int
}

// readPodman never goes through the managed wrapper: even its fleet list can
// apply firewall rules. Only fixed, read-only Podman argv are passed here.
func (w *Worker) readPodman(ctx context.Context, args ...string) ([]byte, error) {
	if w.podmanRead != nil {
		return w.podmanRead(ctx, args...)
	}
	cmd := exec.CommandContext(ctx, "podman", args...) // #nosec G702 -- Fixed executable and read-only argv with validated container name.
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

func (w *Worker) inspectCell(ctx context.Context, tenant string) (cellObservation, error) {
	if !tenantPattern.MatchString(tenant) {
		return cellObservation{}, errCellUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := w.readPodman(ctx, "inspect", "--format", cellInspectFormat, "openclaw-cell-"+tenant)
	if err != nil || ctx.Err() != nil {
		return cellObservation{}, errCellUnavailable
	}
	return parseCellObservation(out, tenant)
}

func parseCellObservation(out []byte, tenant string) (cellObservation, error) {
	var cell struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Running    *bool  `json:"running"`
		Status     string `json:"status"`
		Paused     *bool  `json:"paused"`
		Restarting *bool  `json:"restarting"`
		Dead       *bool  `json:"dead"`
		Ports      map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"ports"`
	}
	if !tenantPattern.MatchString(tenant) || len(out) > 16<<10 || json.Unmarshal(out, &cell) != nil || len(cell.ID) != 64 || strings.TrimPrefix(cell.Name, "/") != "openclaw-cell-"+tenant {
		return cellObservation{}, errCellUnavailable
	}
	if _, err := hex.DecodeString(cell.ID); err != nil || cell.Running == nil || cell.Paused == nil || cell.Restarting == nil || cell.Dead == nil || *cell.Paused || *cell.Restarting || *cell.Dead {
		return cellObservation{}, errCellUnavailable
	}
	state := ""
	switch {
	case *cell.Running && cell.Status == "running":
		state = "running"
	case !*cell.Running && (cell.Status == "exited" || cell.Status == "stopped"):
		state = "stopped"
	default:
		// A false running flag alone is not proof of a stopped container.
		return cellObservation{}, errCellUnavailable
	}
	observed := cellObservation{Target: gatewayCellTarget{ID: cell.ID}, State: state}
	bindings := cell.Ports["18789/tcp"]
	if state == "stopped" && len(bindings) == 0 {
		// Stopped containers may have no active network mapping. Reject a
		// differently published port rather than trusting a stale registry.
		for _, published := range cell.Ports {
			if len(published) != 0 {
				return cellObservation{}, errCellUnavailable
			}
		}
		return observed, nil
	}
	if len(bindings) != 1 || bindings[0].HostIP != "127.0.0.1" {
		return cellObservation{}, errCellUnavailable
	}
	port, err := strconv.Atoi(bindings[0].HostPort)
	if err != nil || port < 19100 || port >= 19200 {
		return cellObservation{}, errCellUnavailable
	}
	observed.Port = port
	observed.Target.Address = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	return observed, nil
}

func (w *Worker) cellStatus(parent context.Context, tenant string) (Result, error) {
	if !tenantPattern.MatchString(tenant) {
		return Result{}, errCellUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	existsCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	_, err := w.readPodman(existsCtx, "container", "exists", "openclaw-cell-"+tenant)
	contextErr := existsCtx.Err()
	stop()
	if contextErr != nil {
		return Result{}, errCellUnavailable
	}
	if err != nil {
		// Podman's documented container-exists contract is 0=present,
		// 1=absent, 125=storage error. Never classify inspect/stderr failures.
		// https://docs.podman.io/en/latest/markdown/podman-container-exists.1.html
		var exit interface{ ExitCode() int }
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return Result{State: "missing"}, nil
		}
		return Result{}, errCellUnavailable
	}
	observed, err := w.inspectCell(ctx, tenant)
	if err != nil {
		// Exists followed by a failed inspect is ambiguous (e.g. concurrent
		// deletion), not a canonical absence observation.
		return Result{}, errCellUnavailable
	}
	result := Result{State: observed.State, Port: observed.Port}
	if observed.State == "running" {
		result.LatencyMS, err = w.probeCell(ctx, tenant, observed.Target)
		if err != nil {
			return Result{}, errCellUnavailable
		}
	}
	if ctx.Err() != nil {
		return Result{}, errCellUnavailable
	}
	return result, nil
}

// A health failure does not override a positive runtime observation. Identity
// or state changes, and failed runtime rechecks, do invalidate that observation.
func (w *Worker) probeCell(parent context.Context, tenant string, target gatewayCellTarget) (*int, error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	started := time.Now()
	dial := w.statusDial
	if dial == nil {
		dial = (&net.Dialer{Timeout: 2 * time.Second}).DialContext
	}
	conn, dialErr := dial(ctx, "tcp", target.Address)
	if conn != nil {
		defer func() { _ = conn.Close() }()
	}
	verify := func() error {
		actual, err := w.gatewayCell(parent, tenant)
		if err != nil || actual != target || parent.Err() != nil {
			return errCellUnavailable
		}
		return nil
	}
	// Check after even a failed dial so a concurrent stop/delete/rebind does
	// not get disguised as a harmless failed health probe. No HTTP bytes are
	// sent until the successfully dialled socket's target has been rechecked.
	if err := verify(); err != nil {
		return nil, err
	}
	if dialErr != nil || conn == nil {
		return nil, nil
	}
	client := cellHealthClient(target, conn)
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+target.Address+"/healthz", nil)
	if err != nil {
		return nil, errCellUnavailable
	}
	resp, probeErr := client.Do(req)
	value := int(time.Since(started).Milliseconds())
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err := verify(); err != nil {
		return nil, err
	}
	if probeErr != nil || resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil
	}
	return &value, nil
}

// Hand the HTTP transport only the already-dialled and revalidated socket.
// There is no fallback dial, proxy discovery, connection reuse or redirect.
func cellHealthClient(target gatewayCellTarget, conn net.Conn) *http.Client {
	var useConnection sync.Once
	transport := &http.Transport{
		Proxy: nil, DisableCompression: true, DisableKeepAlives: true,
		DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
			var selected net.Conn
			if network == "tcp" && address == target.Address {
				useConnection.Do(func() { selected = conn })
			}
			if selected == nil {
				return nil, errCellUnavailable
			}
			return selected, nil
		},
	}
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
