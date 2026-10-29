package handler

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *OpenClawHandler) GatewayEnrollment(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	userID, instanceID, ok := openClawIDs(c)
	if !ok {
		return
	}
	if c.Request.Method == http.MethodPost {
		var input struct {
			Client string `json:"client"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<10)
		if c.ShouldBindJSON(&input) != nil {
			response.BadRequest(c, "Choose macOS or iPhone")
			return
		}
		input.Client = strings.ToLower(strings.TrimSpace(input.Client))
		result, err := h.service.CreateGatewayEnrollment(c.Request.Context(), userID, instanceID, input.Client)
		if err != nil {
			writeOpenClawGatewayError(c, err)
			return
		}
		response.Success(c, result)
		return
	}
	id := strings.TrimSpace(c.Query("id"))
	if id == "" {
		response.BadRequest(c, "Enrollment id is required")
		return
	}
	if c.Request.Method == http.MethodDelete {
		if err := h.service.CancelGatewayEnrollment(c.Request.Context(), userID, instanceID, id); err != nil {
			writeOpenClawGatewayError(c, err)
			return
		}
		response.Success(c, gin.H{"cancelled": true})
		return
	}
	result, err := h.service.GatewayEnrollmentStatus(c.Request.Context(), userID, instanceID, id)
	if err != nil {
		writeOpenClawGatewayError(c, err)
		return
	}
	response.Success(c, result)
}
