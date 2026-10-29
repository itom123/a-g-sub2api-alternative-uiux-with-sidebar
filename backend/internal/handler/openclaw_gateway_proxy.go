package handler

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
	"github.com/gin-gonic/gin"
)

// Caddy's certificate permission check reveals no credentials or owner details.
func (h *OpenClawHandler) GatewayTLS(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if !h.service.GatewayTLSAllowed(ctx, c.Query("domain")) {
		c.Status(http.StatusForbidden)
		return
	}
	c.Status(http.StatusOK)
}

// GatewayIngress runs before application logging, CORS, auth and SPA fallback.
// Native authentication is performed by the owning gateway, never by Host.
func (h *OpenClawHandler) GatewayIngress(c *gin.Context) {
	tenant, matched := openclawgateway.TenantHost(os.Getenv("OPENCLAW_GATEWAY_PUBLIC_ORIGIN_TEMPLATE"), c.Request.Host)
	if !matched {
		c.Next()
		return
	}
	c.Abort()
	h.service.ProxyGateway(c.Writer, c.Request, tenant)
}
