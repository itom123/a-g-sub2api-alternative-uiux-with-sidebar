package service

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

// An existing managed workspace is the entitlement. Its owner, linked billing
// credential, and billing group must remain active. Managed Claw Hub groups in
// the deployed system use ordinary metered billing, not user subscriptions.
func (s *OpenClawService) gatewayTenantOwner(ctx context.Context, tenant string) (int64, error) {
	var userID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT i.user_id FROM openclaw_instances i
		JOIN users u ON u.id = i.user_id AND u.deleted_at IS NULL AND u.status = 'active'
		JOIN openclaw_api_keys link ON link.instance_id = i.id
		JOIN api_keys k ON k.id = link.api_key_id AND k.user_id = i.user_id
		JOIN groups g ON g.id = k.group_id
		WHERE i.tenant_slug = $1 AND i.status IN ('ready', 'running')
		  AND i.deletion_scheduled_at IS NULL
		  AND k.deleted_at IS NULL AND k.status = 'active'
		  AND (k.expires_at IS NULL OR k.expires_at > NOW())
		  AND g.deleted_at IS NULL AND g.status = 'active'`, tenant).Scan(&userID)
	if err != nil || !openClawGatewayEnabled(userID) {
		return 0, ErrOpenClawProviderUnavailable
	}
	return userID, nil
}

func (s *OpenClawService) GatewayTLSAllowed(ctx context.Context, host string) bool {
	template := os.Getenv("OPENCLAW_GATEWAY_PUBLIC_ORIGIN_TEMPLATE")
	tenant, matched := openclawgateway.TenantHost(template, host)
	if !matched || tenant == "" {
		return false
	}
	origin, err := openclawgateway.PublicOrigin(template, tenant)
	if err != nil || origin.Host != host {
		return false
	}
	_, err = s.gatewayTenantOwner(ctx, tenant)
	return err == nil
}

func (s *OpenClawService) ProxyGateway(w http.ResponseWriter, r *http.Request, tenant string) {
	w.Header().Set("Cache-Control", "no-store")
	deny := func() { http.Error(w, "Workspace connection unavailable", http.StatusNotFound) }
	if !openclawgateway.TenantPattern.MatchString(tenant) {
		deny()
		return
	}
	origin, err := openclawgateway.PublicOrigin(os.Getenv("OPENCLAW_GATEWAY_PUBLIC_ORIGIN_TEMPLATE"), tenant)
	if err != nil || r.Host != origin.Host || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != origin.String()) {
		deny()
		return
	}
	userID, err := s.gatewayTenantOwner(r.Context(), tenant)
	if err != nil {
		deny()
		return
	}
	fleet, ok := s.fleet.(*HTTPOpenClawFleetClient)
	if !ok || !fleet.Enabled() {
		deny()
		return
	}
	upstream, err := url.Parse(fleet.BaseURL)
	// This deployment's management bridge must remain loopback HTTP.
	if err != nil || upstream.Scheme != "http" || upstream.Hostname() != "127.0.0.1" || upstream.User != nil || upstream.RawQuery != "" || upstream.Fragment != "" || upstream.Path != "" {
		deny()
		return
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ResponseHeaderTimeout: 15 * time.Second, DisableCompression: true, MaxIdleConnsPerHost: 2}
	defer transport.CloseIdleConnections()
	proxy := &httputil.ReverseProxy{Transport: transport, Rewrite: func(p *httputil.ProxyRequest) {
		nativeAuth := p.In.Header.Get("Authorization")
		openclawgateway.StripIdentity(p.Out.Header)
		p.Out.URL.Scheme, p.Out.URL.Host = upstream.Scheme, upstream.Host
		p.Out.URL.Path = "/v1/gateway/" + tenant + p.In.URL.Path
		p.Out.URL.RawPath = ""
		p.Out.Host = origin.Host
		p.Out.Header.Set("Authorization", "Bearer "+fleet.Token)
		if nativeAuth != "" {
			p.Out.Header.Set("X-ClawHub-Native-Authorization", nativeAuth)
		}
	}}
	allowed := func(ctx context.Context) bool {
		owner, err := s.gatewayTenantOwner(ctx, tenant)
		return err == nil && owner == userID && !strings.Contains(r.Host, ":")
	}
	openclawgateway.ServeProxy(w, r, proxy, allowed)
}
