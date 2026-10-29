package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGatewayHandlersRequireAuthenticatedOwner(t *testing.T) {
	h := NewOpenClawHandler(nil)
	for _, entry := range []struct {
		method  string
		handler gin.HandlerFunc
	}{{http.MethodPost, h.GatewayLaunch}, {http.MethodGet, h.GatewayDevices}, {http.MethodPost, h.GatewayDevices}} {
		r := gin.New()
		r.Handle(entry.method, "/instances/:instance_id/gateway", entry.handler)
		rw := httptest.NewRecorder()
		r.ServeHTTP(rw, httptest.NewRequest(entry.method, "/instances/7/gateway", nil))
		if rw.Code != http.StatusUnauthorized || rw.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("gateway operation did not enforce authentication or prevent caching")
		}
	}
}

func TestGatewayErrorsDoNotExposeOperatorDiagnostics(t *testing.T) {
	r := gin.New()
	r.GET("/", func(c *gin.Context) {
		writeOpenClawGatewayError(c, errors.New("secret-token private-worker-address sql detail"))
	})
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/", nil))
	if rw.Code != http.StatusServiceUnavailable || strings.Contains(rw.Body.String(), "secret-token") || strings.Contains(rw.Body.String(), "private-worker") {
		t.Fatal("gateway error leaked operator diagnostics")
	}
}
