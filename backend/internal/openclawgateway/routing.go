package openclawgateway

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var TenantPattern = regexp.MustCompile(`^u[1-9][0-9]{0,18}-[a-f0-9]{12}$`)
var hostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$`)

// PublicOrigin requires a separate DNS origin for every cell.
func PublicOrigin(template, tenant string) (*url.URL, error) {
	if !TenantPattern.MatchString(tenant) || !strings.HasPrefix(template, "https://{tenant}.") || strings.Count(template, "{tenant}") != 1 {
		return nil, errors.New("invalid gateway origin configuration")
	}
	u, err := url.Parse(strings.Replace(template, "{tenant}", tenant, 1))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || !hostPattern.MatchString(u.Host) || len(u.Host) > 253 {
		return nil, errors.New("invalid gateway origin configuration")
	}
	return u, nil
}

// TenantHost distinguishes an invalid gateway host from a non-gateway request.
// Never use forwarded Host headers for this routing decision.
func TenantHost(template, host string) (tenant string, gatewayHost bool) {
	probe, err := PublicOrigin(template, "u1-000000000000")
	if err != nil {
		return "", false
	}
	suffix := strings.TrimPrefix(probe.Host, "u1-000000000000")
	if hostname, _, err := net.SplitHostPort(host); err == nil {
		host = hostname
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if !strings.HasSuffix(host, suffix) {
		return "", false
	}
	tenant = strings.TrimSuffix(host, suffix)
	if !TenantPattern.MatchString(tenant) {
		return "", true
	}
	return tenant, true
}
