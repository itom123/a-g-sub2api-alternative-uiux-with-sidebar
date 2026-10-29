package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// TelegramHandler lets a user link their account to the Telegram bot.
type TelegramHandler struct {
	telegram *service.TelegramService
}

func NewTelegramHandler(telegram *service.TelegramService) *TelegramHandler {
	return &TelegramHandler{telegram: telegram}
}

// GetStatus GET /api/v1/user/telegram
func (h *TelegramHandler) GetStatus(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	status, err := h.telegram.GetLinkStatus(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}

// ResetBindCode POST /api/v1/user/telegram/bind-code/reset
func (h *TelegramHandler) ResetBindCode(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	status, err := h.telegram.ResetBindCode(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}

// Unlink DELETE /api/v1/user/telegram
func (h *TelegramHandler) Unlink(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if err := h.telegram.Unlink(c.Request.Context(), subject.UserID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"linked": false})
}
