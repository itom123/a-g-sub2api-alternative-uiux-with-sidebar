package openclawworker

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

type gatewayCellTarget struct {
	ID      string
	Address string
}

// Inspect only the selected container's nonsecret identity and loopback mapping.
// Recheck this identity after TCP dial to reject host-port reuse races.
func (w *Worker) gatewayCell(ctx context.Context, tenant string) (gatewayCellTarget, error) {
	if !tenantPattern.MatchString(tenant) {
		return gatewayCellTarget{}, errors.New("invalid gateway tenant")
	}
	format := `{"id":{{json .ID}},"name":{{json .Name}},"running":{{json .State.Running}},"ports":{{json .NetworkSettings.Ports}}}`
	cmd := exec.CommandContext(ctx, "podman", "inspect", "--format", format, "openclaw-cell-"+tenant) // #nosec G702 -- Fixed executable and validated container name.
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return gatewayCellTarget{}, errors.New("gateway cell unavailable")
	}
	return parseGatewayCell(out, tenant)
}

func parseGatewayCell(out []byte, tenant string) (gatewayCellTarget, error) {
	var cell struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Running bool   `json:"running"`
		Ports   map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"ports"`
	}
	invalid := errors.New("gateway cell unavailable")
	if len(out) > 16<<10 || json.Unmarshal(out, &cell) != nil || len(cell.ID) != 64 || strings.TrimPrefix(cell.Name, "/") != "openclaw-cell-"+tenant || !cell.Running {
		return gatewayCellTarget{}, invalid
	}
	bindings := cell.Ports["18789/tcp"]
	if len(bindings) != 1 || bindings[0].HostIP != "127.0.0.1" {
		return gatewayCellTarget{}, invalid
	}
	port, err := strconv.Atoi(bindings[0].HostPort)
	if err != nil || port < 19100 || port >= 19200 {
		return gatewayCellTarget{}, invalid
	}
	return gatewayCellTarget{ID: cell.ID, Address: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}, nil
}

func (w *Worker) proxyGateway(rw http.ResponseWriter, r *http.Request) {
	deny := func() { http.Error(rw, "Workspace connection unavailable", http.StatusNotFound) }
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/v1/gateway/"), "/", 2)
	if len(parts) != 2 || !tenantPattern.MatchString(parts[0]) || os.Getenv("OPENCLAW_GATEWAY_ENABLED") != "1" {
		deny()
		return
	}
	tenant := parts[0]
	// Do not treat a provisioned signing key as rollout authorization, or let
	// an enrollment token fall through to ordinary shared authentication.
	if strings.HasPrefix(strings.TrimPrefix(r.Header.Get("X-ClawHub-Native-Authorization"), "Bearer "), "ag-enroll-v1.") && os.Getenv("OPENCLAW_DEVICE_ENROLLMENT_ENABLED") != "1" {
		deny()
		return
	}
	nativeAdmissionID, enrollmentTicket := w.enrollmentAdmission(tenant, r.Header.Get("X-ClawHub-Native-Authorization"))
	queryEnrollmentID := strings.TrimSpace(r.URL.Query().Get("clawhub_enrollment"))
	origin, err := gatewayPublicOrigin(os.Getenv("OPENCLAW_GATEWAY_PUBLIC_ORIGIN_TEMPLATE"), tenant)
	if err != nil || r.Host != origin.Host || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != origin.String()) {
		deny()
		return
	}
	checkCtx, stop := context.WithTimeout(r.Context(), 3*time.Second)
	target, err := w.gatewayCell(checkCtx, tenant)
	stop()
	if err != nil {
		deny()
		return
	}
	if nativeAdmissionID != "" && websocketUpgradeRequested(r) {
		w.proxyEnrollmentWebSocket(rw, r, tenant, parts[1], origin, target, nativeAdmissionID, enrollmentTicket)
		return
	}
	// Native clients carry the one-time admission ticket in the first signed
	// connect frame, so it cannot be present in the HTTP upgrade headers. The
	// setup URL carries only the opaque enrollment id to select the observer;
	// the observer verifies the ticket against the worker secret before binding.
	if queryEnrollmentID != "" && websocketUpgradeRequested(r) && os.Getenv("OPENCLAW_DEVICE_ENROLLMENT_ENABLED") == "1" {
		w.proxyEnrollmentWebSocket(rw, r, tenant, parts[1], origin, target, queryEnrollmentID, "")
		return
	}
	transport := &http.Transport{Proxy: nil, DisableCompression: true, ResponseHeaderTimeout: 15 * time.Second, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		conn, err := (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, "tcp", target.Address)
		if err != nil {
			return nil, errors.New("gateway connection unavailable")
		}
		verifyCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		actual, err := w.gatewayCell(verifyCtx, tenant)
		if err != nil || actual != target {
			_ = conn.Close()
			return nil, errors.New("gateway identity changed")
		}
		return conn, nil
	}}
	defer transport.CloseIdleConnections()
	proxy := &httputil.ReverseProxy{Transport: transport, Rewrite: func(p *httputil.ProxyRequest) {
		nativeAuth := p.In.Header.Get("X-ClawHub-Native-Authorization")
		openclawgateway.StripIdentity(p.Out.Header)
		p.Out.Header.Del("Authorization")
		if nativeAdmissionID != "" {
			// An admission ticket only opens the native pairing handshake. It is
			// deliberately removed before the gateway sees Authorization, so it
			// cannot be mistaken for a shared/operator credential.
			p.Out.Header.Del("Authorization")
			p.Out.Header.Set("X-ClawHub-Enrollment-ID", nativeAdmissionID)
		} else if nativeAuth != "" {
			p.Out.Header.Set("Authorization", nativeAuth)
		}
		p.Out.URL.Scheme, p.Out.URL.Host = "http", target.Address
		p.Out.URL.Path, p.Out.URL.RawPath = "/"+parts[1], ""
		p.Out.Host = origin.Host
		// A fixed non-loopback proxy peer prevents localhost auto-pairing. Native
		// gateway authentication and signed browser device identity remain required.
		p.Out.Header.Set("X-Forwarded-For", "198.18.0.1")
		p.Out.Header.Set("X-Real-IP", "198.18.0.1")
		p.Out.Header.Set("X-Forwarded-Host", origin.Host)
		p.Out.Header.Set("X-Forwarded-Proto", "https")
	}}
	rw.Header().Del("Content-Type")
	openclawgateway.ServeProxy(rw, r, proxy, func(ctx context.Context) bool {
		if os.Getenv("OPENCLAW_GATEWAY_ENABLED") != "1" {
			return false
		}
		current, err := w.gatewayCell(ctx, tenant)
		return err == nil && current == target
	})
}

func (w *Worker) enrollmentAdmission(tenant, authorization string) (string, string) {
	if len(w.EnrollmentSecret) < 32 {
		return "", ""
	}
	candidate := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	if candidate == authorization {
		return "", ""
	}
	id, ok := openclawgateway.VerifyEnrollmentAdmissionTicket(candidate, tenant, time.Now(), w.EnrollmentSecret)
	if !ok {
		return "", ""
	}
	return id, candidate
}
