package handler

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type OpenClawHandler struct {
	service *service.OpenClawService
}

func NewOpenClawHandler(openClawService *service.OpenClawService) *OpenClawHandler {
	return &OpenClawHandler{service: openClawService}
}

type openClawCreateInstanceRequest struct {
	Name string `json:"name" binding:"required"`
}

type openClawRenameInstanceRequest struct {
	Name string `json:"name" binding:"required"`
}

type openClawCreateConnectionRequest struct {
	Kind       string `json:"kind" binding:"required"`
	Label      string `json:"label"`
	Credential string `json:"credential"`
}

type openClawCompleteConnectionRequest struct {
	Credential string `json:"credential" binding:"required"`
}

type openClawAccessRequestRequest struct {
	Action     string `json:"action" binding:"required"`
	InstanceID *int64 `json:"instance_id"`
	Reason     string `json:"reason"`
}

type openClawAccessDecisionRequest struct {
	Approved bool   `json:"approved"`
	Note     string `json:"note"`
}

type openClawPolicyRequest struct {
	SelfServiceEnabled bool `json:"self_service_enabled"`
}

type careerPairRequest struct {
	Code   string `json:"code" binding:"required"`
	ChatID string `json:"chat_id" binding:"required"`
}

func (h *OpenClawHandler) GetCareerProfile(c *gin.Context) {
	userID, ok := openClawUserID(c)
	if !ok {
		return
	}
	profile, err := h.service.GetCareerProfile(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, profile)
}

func (h *OpenClawHandler) UpdateCareerProfile(c *gin.Context) {
	userID, ok := openClawUserID(c)
	if !ok {
		return
	}
	var req service.CareerProfileInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid career profile")
		return
	}
	profile, err := h.service.UpdateCareerProfile(c.Request.Context(), userID, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, profile)
}

func (h *OpenClawHandler) CreateCareerPairing(c *gin.Context) {
	userID, ok := openClawUserID(c)
	if !ok {
		return
	}
	pairing, err := h.service.CreateCareerPairing(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, pairing)
}

func (h *OpenClawHandler) UnpairCareerTelegram(c *gin.Context) {
	userID, ok := openClawUserID(c)
	if !ok {
		return
	}
	if err := h.service.UnpairCareerTelegram(c.Request.Context(), userID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"paired": false})
}

func (h *OpenClawHandler) GetCareerInternalProfile(c *gin.Context) {
	configured := strings.TrimSpace(os.Getenv("OPENCLAW_CONCIERGE_INTERNAL_TOKEN"))
	provided := strings.TrimSpace(c.GetHeader("X-OpenClaw-Concierge-Token"))
	if configured == "" || subtle.ConstantTimeCompare([]byte(configured), []byte(provided)) != 1 {
		response.Unauthorized(c, "concierge authorization required")
		return
	}
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "invalid user id")
		return
	}
	scheduleID, err := strconv.ParseInt(c.Query("schedule_id"), 10, 64)
	if err != nil || scheduleID <= 0 {
		response.BadRequest(c, "invalid schedule id")
		return
	}
	profile, err := h.service.GetCareerInternalProfile(c.Request.Context(), userID, scheduleID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, profile)
}

func (h *OpenClawHandler) PairCareerTelegram(c *gin.Context) {
	configured := strings.TrimSpace(os.Getenv("OPENCLAW_CONCIERGE_INTERNAL_TOKEN"))
	provided := strings.TrimSpace(c.GetHeader("X-OpenClaw-Concierge-Token"))
	if configured == "" || subtle.ConstantTimeCompare([]byte(configured), []byte(provided)) != 1 {
		response.Unauthorized(c, "concierge authorization required")
		return
	}
	var req careerPairRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "code and chat_id are required")
		return
	}
	if err := h.service.PairCareerTelegram(c.Request.Context(), req.Code, req.ChatID); err != nil {
		if errors.Is(err, service.ErrCareerPairingInvalid) {
			response.Error(c, http.StatusBadRequest, "pairing code is invalid or expired")
			return
		}
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"paired": true})
}

func (h *OpenClawHandler) ListCareerSchedules(c *gin.Context) {
	userID, ok := openClawUserID(c)
	if !ok {
		return
	}
	items, err := h.service.ListCareerSchedules(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if items == nil {
		items = []service.CareerSchedule{}
	}
	response.Success(c, gin.H{"items": items})
}

func (h *OpenClawHandler) CreateCareerSchedule(c *gin.Context) {
	userID, ok := openClawUserID(c)
	if !ok {
		return
	}
	var req service.CareerScheduleInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid career schedule")
		return
	}
	item, err := h.service.CreateCareerSchedule(c.Request.Context(), userID, req)
	if err != nil {
		if errors.Is(err, service.ErrCareerTelegramNotPaired) {
			response.Error(c, http.StatusConflict, "pair Telegram before creating a schedule")
			return
		}
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, item)
}

func (h *OpenClawHandler) DeleteCareerSchedule(c *gin.Context) {
	userID, ok := openClawUserID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("schedule_id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid schedule id")
		return
	}
	if err := h.service.DeleteCareerSchedule(c.Request.Context(), userID, id); err != nil {
		if errors.Is(err, service.ErrCareerScheduleNotFound) {
			response.NotFound(c, "career schedule not found")
			return
		}
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *OpenClawHandler) ListInstances(c *gin.Context) {
	userID, ok := openClawUserID(c)
	if !ok {
		return
	}
	instances, err := h.service.ListInstances(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if instances == nil {
		instances = []service.OpenClawInstance{}
	}
	response.Success(c, gin.H{"items": instances})
}

func (h *OpenClawHandler) Dashboard(c *gin.Context) {
	userID, ok := openClawUserID(c)
	if !ok {
		return
	}
	dashboard, err := h.service.GetDashboard(c.Request.Context(), userID)
	if err != nil {
		writeOpenClawError(c, err)
		return
	}
	response.Success(c, dashboard)
}

func (h *OpenClawHandler) GatewayLaunch(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Referrer-Policy", "no-referrer")
	userID, instanceID, ok := openClawIDs(c)
	if !ok {
		return
	}
	url, err := h.service.GetGatewayLaunchURL(c.Request.Context(), userID, instanceID)
	if err != nil {
		writeOpenClawGatewayError(c, err)
		return
	}
	response.Success(c, gin.H{"url": url})
}

func (h *OpenClawHandler) Lifecycle(c *gin.Context) {
	userID, ok := openClawUserID(c)
	if !ok {
		return
	}
	lifecycle, err := h.service.GetLifecycle(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, lifecycle)
}

func (h *OpenClawHandler) ListConnectionKinds(c *gin.Context) {
	kinds, err := h.service.ListConnectionKinds(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if kinds == nil {
		kinds = []service.OpenClawConnectionKindInfo{}
	}
	response.Success(c, gin.H{"items": kinds})
}

func (h *OpenClawHandler) CreateAccessRequest(c *gin.Context) {
	userID, ok := openClawUserID(c)
	if !ok {
		return
	}
	var req openClawAccessRequestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "action is required")
		return
	}
	request, err := h.service.CreateAccessRequest(c.Request.Context(), userID, req.Action, req.InstanceID, req.Reason)
	if err != nil {
		writeOpenClawError(c, err)
		return
	}
	response.Created(c, request)
}

func (h *OpenClawHandler) CreateInstance(c *gin.Context) {
	userID, ok := openClawUserID(c)
	if !ok {
		return
	}
	var req openClawCreateInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "instance name is required")
		return
	}
	instance, err := h.service.CreateInstance(c.Request.Context(), userID, req.Name)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, instance)
}

func (h *OpenClawHandler) GetInstance(c *gin.Context) {
	userID, instanceID, ok := openClawIDs(c)
	if !ok {
		return
	}
	instance, err := h.service.GetInstance(c.Request.Context(), userID, instanceID)
	if err != nil {
		writeOpenClawError(c, err)
		return
	}
	response.Success(c, instance)
}

func (h *OpenClawHandler) RenameInstance(c *gin.Context) {
	userID, instanceID, ok := openClawIDs(c)
	if !ok {
		return
	}
	var req openClawRenameInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "instance name is required")
		return
	}
	instance, err := h.service.RenameInstance(c.Request.Context(), userID, instanceID, req.Name)
	if err != nil {
		writeOpenClawError(c, err)
		return
	}
	response.Success(c, instance)
}

func (h *OpenClawHandler) ProvisionInstance(c *gin.Context) {
	userID, instanceID, ok := openClawIDs(c)
	if !ok {
		return
	}
	instance, err := h.service.ProvisionInstance(c.Request.Context(), userID, instanceID)
	if err != nil {
		writeOpenClawError(c, err)
		return
	}
	response.Success(c, instance)
}

func (h *OpenClawHandler) StartInstance(c *gin.Context) {
	h.lifecycle(c, func(userID, instanceID int64) error {
		return h.service.StartInstance(c.Request.Context(), userID, instanceID)
	})
}

func (h *OpenClawHandler) StopInstance(c *gin.Context) {
	h.lifecycle(c, func(userID, instanceID int64) error {
		return h.service.StopInstance(c.Request.Context(), userID, instanceID)
	})
}

func (h *OpenClawHandler) DeleteInstance(c *gin.Context) {
	userID, instanceID, ok := openClawIDs(c)
	if !ok {
		return
	}
	instance, err := h.service.DeleteInstance(c.Request.Context(), userID, instanceID)
	if err != nil {
		writeOpenClawError(c, err)
		return
	}
	response.Success(c, instance)
}

func (h *OpenClawHandler) ListConnections(c *gin.Context) {
	userID, instanceID, ok := openClawIDs(c)
	if !ok {
		return
	}
	connections, err := h.service.ListConnections(c.Request.Context(), userID, instanceID)
	if err != nil {
		writeOpenClawError(c, err)
		return
	}
	if connections == nil {
		connections = []service.OpenClawConnection{}
	}
	response.Success(c, gin.H{"items": connections})
}

func (h *OpenClawHandler) CreateConnection(c *gin.Context) {
	userID, instanceID, ok := openClawIDs(c)
	if !ok {
		return
	}
	var req openClawCreateConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "connection kind is required")
		return
	}
	connection, err := h.service.CreateConnection(c.Request.Context(), userID, instanceID, strings.ToLower(strings.TrimSpace(req.Kind)), req.Label, req.Credential)
	if err != nil {
		writeOpenClawError(c, err)
		return
	}
	response.Created(c, connection)
}

func (h *OpenClawHandler) DeleteConnection(c *gin.Context) {
	userID, instanceID, ok := openClawIDs(c)
	if !ok {
		return
	}
	connectionID, err := strconv.ParseInt(c.Param("connection_id"), 10, 64)
	if err != nil || connectionID <= 0 {
		response.BadRequest(c, "invalid connection id")
		return
	}
	if err := h.service.DeleteConnection(c.Request.Context(), userID, instanceID, connectionID); err != nil {
		writeOpenClawError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *OpenClawHandler) CompleteConnection(c *gin.Context) {
	userID, instanceID, ok := openClawIDs(c)
	if !ok {
		return
	}
	connectionID, err := strconv.ParseInt(c.Param("connection_id"), 10, 64)
	if err != nil || connectionID <= 0 {
		response.BadRequest(c, "invalid connection id")
		return
	}
	var req openClawCompleteConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "provider session key is required")
		return
	}
	connection, err := h.service.CompleteConnection(c.Request.Context(), userID, instanceID, connectionID, req.Credential)
	if err != nil {
		writeOpenClawError(c, err)
		return
	}
	response.Success(c, connection)
}

func (h *OpenClawHandler) AdminListInstances(c *gin.Context) {
	items, err := h.service.ListAdminInstances(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if items == nil {
		items = []service.OpenClawAdminInstance{}
	}
	response.Success(c, gin.H{"items": items})
}

func (h *OpenClawHandler) AdminListAccessRequests(c *gin.Context) {
	requests, err := h.service.ListAccessRequests(c.Request.Context(), c.Query("status"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if requests == nil {
		requests = []service.OpenClawAccessRequest{}
	}
	response.Success(c, gin.H{"items": requests})
}

func (h *OpenClawHandler) AdminReviewAccessRequest(c *gin.Context) {
	requestID, err := strconv.ParseInt(c.Param("request_id"), 10, 64)
	if err != nil || requestID <= 0 {
		response.BadRequest(c, "invalid request id")
		return
	}
	adminID, ok := openClawUserID(c)
	if !ok {
		return
	}
	var req openClawAccessDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "approved is required")
		return
	}
	request, err := h.service.ReviewAccessRequest(c.Request.Context(), requestID, adminID, req.Approved, req.Note)
	if err != nil {
		writeOpenClawError(c, err)
		return
	}
	response.Success(c, request)
}

func (h *OpenClawHandler) AdminGetAccessPolicy(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "invalid user id")
		return
	}
	policy, err := h.service.GetAccessPolicy(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, policy)
}

func (h *OpenClawHandler) AdminSetAccessPolicy(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "invalid user id")
		return
	}
	adminID, ok := openClawUserID(c)
	if !ok {
		return
	}
	var req openClawPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "self_service_enabled is required")
		return
	}
	policy, err := h.service.SetAccessPolicy(c.Request.Context(), userID, adminID, req.SelfServiceEnabled)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, policy)
}

func (h *OpenClawHandler) AdminDeleteInstance(c *gin.Context) {
	instanceID, err := strconv.ParseInt(c.Param("instance_id"), 10, 64)
	if err != nil || instanceID <= 0 {
		response.BadRequest(c, "invalid instance id")
		return
	}
	instance, err := h.service.DeleteInstanceAdmin(c.Request.Context(), instanceID)
	if err != nil {
		writeOpenClawError(c, err)
		return
	}
	response.Success(c, instance)
}

func (h *OpenClawHandler) AdminReconcileInstance(c *gin.Context) {
	instanceID, err := strconv.ParseInt(c.Param("instance_id"), 10, 64)
	if err != nil || instanceID <= 0 {
		response.BadRequest(c, "invalid instance id")
		return
	}
	instance, err := h.service.ReconcileInstanceAdmin(c.Request.Context(), instanceID)
	if err != nil {
		writeOpenClawError(c, err)
		return
	}
	response.Success(c, instance)
}

func (h *OpenClawHandler) lifecycle(c *gin.Context, fn func(int64, int64) error) {
	userID, instanceID, ok := openClawIDs(c)
	if !ok {
		return
	}
	if err := fn(userID, instanceID); err != nil {
		writeOpenClawError(c, err)
		return
	}
	response.Success(c, gin.H{"ok": true})
}

func openClawUserID(c *gin.Context) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "authentication required")
		return 0, false
	}
	return subject.UserID, true
}

func openClawIDs(c *gin.Context) (int64, int64, bool) {
	userID, ok := openClawUserID(c)
	if !ok {
		return 0, 0, false
	}
	instanceID, err := strconv.ParseInt(c.Param("instance_id"), 10, 64)
	if err != nil || instanceID <= 0 {
		response.BadRequest(c, "invalid instance id")
		return 0, 0, false
	}
	return userID, instanceID, true
}

func writeOpenClawError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrOpenClawInstanceExists):
		response.Error(c, http.StatusConflict, "each user can have only one openclaw workspace")
	case errors.Is(err, service.ErrOpenClawDeletionScheduled):
		response.Error(c, http.StatusConflict, "workspace deletion is already queued; wait for the seven-day deletion window")
	case errors.Is(err, service.ErrOpenClawRecreateCooldown):
		response.Error(c, http.StatusConflict, "workspace creation is blocked for seven days after deletion")
	case errors.Is(err, service.ErrOpenClawProviderUnavailable):
		response.Error(c, http.StatusServiceUnavailable, "this provider is not available through the UK02 bridge yet")
	case errors.Is(err, service.ErrOpenClawInstanceNotFound), errors.Is(err, service.ErrOpenClawConnectionNotFound):
		response.NotFound(c, "openclaw resource not found")
	case errors.Is(err, service.ErrOpenClawInvalidKind):
		response.BadRequest(c, "unsupported connection kind")
	case errors.Is(err, service.ErrOpenClawFleetDisabled):
		response.Error(c, http.StatusServiceUnavailable, "openclaw fleet is not enabled")
	case errors.Is(err, service.ErrOpenClawApprovalRequired):
		response.Error(c, http.StatusForbidden, "administrator approval is required for this OpenClaw action")
	case errors.Is(err, service.ErrOpenClawRequestPending):
		response.Error(c, http.StatusConflict, "an administrator approval request is already pending")
	case errors.Is(err, service.ErrOpenClawRequestNotFound):
		response.NotFound(c, "openclaw access request not found")
	case errors.Is(err, service.ErrOpenClawGroupImmutable):
		response.Error(c, http.StatusForbidden, "OpenClaw workspace keys must remain in the CLAW group")
	default:
		response.ErrorFrom(c, err)
	}
}
