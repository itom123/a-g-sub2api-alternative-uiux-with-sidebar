package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// TelegramHandler manages the Telegram bot settings and notices.
type TelegramHandler struct {
	telegram *service.TelegramService
}

func NewTelegramHandler(telegram *service.TelegramService) *TelegramHandler {
	return &TelegramHandler{telegram: telegram}
}

// GetSettings GET /api/v1/admin/telegram/settings
func (h *TelegramHandler) GetSettings(c *gin.Context) {
	settings, err := h.telegram.GetSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

// UpdateSettings PUT /api/v1/admin/telegram/settings
func (h *TelegramHandler) UpdateSettings(c *gin.Context) {
	var req service.TelegramBotSettingsInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	settings, err := h.telegram.UpdateSettings(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

// SendTest POST /api/v1/admin/telegram/test
func (h *TelegramHandler) SendTest(c *gin.Context) {
	sent, err := h.telegram.SendTestMessage(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"sent": sent})
}

type telegramBroadcastRequest struct {
	Text string `json:"text"`
}

// Broadcast POST /api/v1/admin/telegram/broadcast
func (h *TelegramHandler) Broadcast(c *gin.Context) {
	var req telegramBroadcastRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	recipients, err := h.telegram.Broadcast(c.Request.Context(), req.Text)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"recipients": recipients})
}
