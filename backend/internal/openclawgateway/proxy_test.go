package openclawgateway

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestStripIdentityRemovesProxyOwnedTailscaleClaims(t *testing.T) {
	h := http.Header{}
	for _, name := range []string{"Tailscale-User-Login", "Tailscale-User-Name", "Tailscale-Funnel-Request", "Tailscale-Headers-Info", "X-Forwarded-For", "Cookie"} {
		h.Set(name, "forged")
	}
	h.Set("Authorization", "Bearer native")
	h.Set("Origin", "https://tenant.example")
	StripIdentity(h)
	if len(h) != 2 || h.Get("Authorization") != "Bearer native" || h.Get("Origin") != "https://tenant.example" {
		t.Fatal("proxy identity stripping changed native auth or retained forged claims")
	}
}

func TestGatewayWithdrawalClosesActiveWebSocket(t *testing.T) {
	upgrader := websocket.Upgrader{}
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			kind, body, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if conn.WriteMessage(kind, body) != nil {
				return
			}
		}
	}))
	defer native.Close()
	upstream, _ := url.Parse(native.URL)
	var active atomic.Bool
	active.Store(true)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ServeProxy(w, r, httputil.NewSingleHostReverseProxy(upstream), func(context.Context) bool { return active.Load() })
	}))
	defer proxy.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, []byte("authenticated-session-fixture")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	active.Store(false)
	_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("withdrawal did not close the active connection")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("withdrawal waited for the client timeout")
	}
}

func TestGatewayHostCannotFallThroughToPanel(t *testing.T) {
	for _, host := range []string{"u1-0123456789ab.claw.example", "u1-0123456789ab.claw.example:443", "U1-0123456789AB.CLAW.EXAMPLE", "u1-0123456789ab.claw.example.", "invalid.claw.example", "nested.invalid.claw.example"} {
		if _, matched := TenantHost("https://{tenant}.claw.example", host); !matched {
			t.Fatalf("gateway host %q fell through", host)
		}
	}
	if _, matched := TenantHost("https://{tenant}.claw.example", "api.example"); matched {
		t.Fatal("captured the platform host")
	}
}
