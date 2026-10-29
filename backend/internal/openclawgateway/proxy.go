package openclawgateway

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"
)

// StripIdentity prevents platform cookies and client-controlled proxy identities
// from becoming native gateway authentication or a local-device pairing bypass.
func StripIdentity(h http.Header) {
	for name := range h {
		if strings.HasPrefix(strings.ToLower(name), "tailscale-") {
			h.Del(name)
		}
	}
	for _, name := range []string{"Cookie", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP", "CF-Connecting-IP", "True-Client-IP", "X-ClawHub-Native-Authorization"} {
		h.Del(name)
	}
}

// ServeProxy continually rechecks access, including after WebSocket upgrade.
// A policy error fails closed and closes already-issued native connections.
func ServeProxy(w http.ResponseWriter, r *http.Request, proxy *httputil.ReverseProxy, allowed func(context.Context) bool) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkCtx, stop := context.WithTimeout(ctx, 2*time.Second)
				ok := allowed(checkCtx)
				stop()
				if !ok {
					cancel()
					return
				}
			}
		}
	}()
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Workspace connection unavailable", http.StatusBadGateway)
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		response.Header.Set("Cache-Control", "no-store")
		response.Header.Set("Referrer-Policy", "no-referrer")
		response.Header.Del("Set-Cookie")
		if response.StatusCode == http.StatusSwitchingProtocols {
			// ReverseProxy transfers ownership by clearing response.Body on upgrade.
			// Capture the stream now so policy cancellation can still close it.
			stream := response.Body
			context.AfterFunc(ctx, func() { _ = stream.Close() })
		}
		return nil
	}
	proxy.ServeHTTP(w, r.WithContext(ctx))
}
