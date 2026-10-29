package handler

import (
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenClawHandler) GatewayDevices(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	userID, instanceID, ok := openClawIDs(c)
	if !ok {
		return
	}
	request := openclawgateway.Request{Action: "list"}
	if c.Request.Method == http.MethodPost {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<10)
		if c.ShouldBindJSON(&request) != nil || (request.Action != "approve" && request.Action != "reject" && request.Action != "remove") {
			response.BadRequest(c, "Choose a current device request and try again")
			return
		}
	}
	inventory, err := h.service.GatewayDevices(c.Request.Context(), userID, instanceID, request)
	if err != nil {
		writeOpenClawGatewayError(c, err)
		return
	}
	response.Success(c, gin.H{"devices": inventory})
}

func writeOpenClawGatewayError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrOpenClawInstanceNotFound):
		response.NotFound(c, "Workspace not found")
	case errors.Is(err, service.ErrOpenClawDeletionScheduled):
		response.Error(c, http.StatusConflict, "This workspace is scheduled for deletion")
	default:
		response.Error(c, http.StatusServiceUnavailable, "Your workspace connection is unavailable. Start your workspace and try again")
	}
}
