package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrOpenClawInstanceNotFound    = errors.New("openclaw instance not found")
	ErrOpenClawInstanceExists      = errors.New("openclaw workspace already exists")
	ErrOpenClawConnectionNotFound  = errors.New("openclaw connection not found")
	ErrOpenClawInvalidKind         = errors.New("unsupported openclaw connection kind")
	ErrOpenClawFleetDisabled       = errors.New("openclaw fleet is disabled")
	ErrOpenClawDeletionScheduled   = errors.New("openclaw workspace deletion is already scheduled")
	ErrOpenClawRecreateCooldown    = errors.New("openclaw workspace recreation is temporarily blocked")
	ErrOpenClawProviderUnavailable = errors.New("openclaw provider setup is unavailable")
	ErrOpenClawApprovalRequired    = errors.New("openclaw administrator approval required")
	ErrOpenClawRequestPending      = errors.New("openclaw access request already pending")
	ErrOpenClawRequestNotFound     = errors.New("openclaw access request not found")
	ErrOpenClawGroupImmutable      = infraerrors.Forbidden("OPENCLAW_GROUP_IMMUTABLE", "OpenClaw workspace keys must remain in the openai-龙虾 group")
	ErrOpenClawBillingLocked       = infraerrors.Forbidden("OPENCLAW_BILLING_LOCKED", "The managed OpenClaw group is reserved and locked to openai-龙虾 at x0.3")
)

const (
	OpenClawDeletionGracePeriod = 7 * 24 * time.Hour
	OpenClawConnectionWeChat    = "wechat"
	OpenClawConnectionTelegram  = "telegram"
	OpenClawConnectionGWS       = "gws"
	OpenClawHubSlug             = "claw-hub-i"
	OpenClawHubDisplayName      = "Claw Hub I"
	// OpenClawBillingGroupName is the managed public group used by every
	// workspace key. Keeping one canonical group makes the workspace's model
	// pricing and balance billing deterministic for newly registered users.
	OpenClawBillingGroupName        = "openai-龙虾"
	OpenClawBillingGroupDescription = "Managed OpenClaw billing group"
	OpenClawBillingRateMultiplier   = 0.30
)

var openClawAllowedModels = []string{
	"gpt-5.6",
	"gpt-5.6-sol",
	"gpt-5.6-terra",
	"gpt-5.6-luna",
	"gpt-6.1-sol",
	"gpt-6-sol",
	"gpt-6-luna",
}

func openClawModelAllowlist() GroupModelAllowlist {
	return GroupModelAllowlist{Enabled: true, Models: append([]string(nil), openClawAllowedModels...)}
}

func openClawReasoningEffortMappings() []ReasoningEffortMapping {
	return []ReasoningEffortMapping{
		{From: "none", To: "low", MatchType: "exact", Model: "gpt-6.1-sol"},
		{From: "minimal", To: "low", MatchType: "exact", Model: "gpt-6.1-sol"},
	}
}

func isOpenClawAllowedModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	model = strings.TrimPrefix(model, "openai/")
	for _, allowed := range openClawAllowedModels {
		if model == allowed {
			return true
		}
	}
	return false
}

type OpenClawInstance struct {
	ID                  int64      `json:"id"`
	TenantSlug          string     `json:"tenant_slug"`
	Name                string     `json:"name"`
	Status              string     `json:"status"`
	WorkerHost          string     `json:"worker_host"`
	Port                *int       `json:"port,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	DeletionRequestedAt *time.Time `json:"deletion_requested_at,omitempty"`
	DeletionScheduledAt *time.Time `json:"deletion_scheduled_at,omitempty"`
}

type OpenClawLifecycle struct {
	Instance             *OpenClawInstance `json:"instance,omitempty"`
	RecreateAllowedAt    *time.Time        `json:"recreate_allowed_at,omitempty"`
	HubName              string            `json:"hub_name"`
	CanCreate            bool              `json:"can_create"`
	CanDelete            bool              `json:"can_delete"`
	SelfServiceEnabled   bool              `json:"self_service_enabled"`
	CreateRequestPending bool              `json:"create_request_pending"`
	DeleteRequestPending bool              `json:"delete_request_pending"`
}

// OpenClawAccessPolicy is deliberately separate from the general user record:
// an operator can gate lifecycle actions without changing model/API access.
type OpenClawAccessPolicy struct {
	UserID             int64  `json:"user_id"`
	SelfServiceEnabled bool   `json:"self_service_enabled"`
	HubName            string `json:"hub_name"`
}

type OpenClawAccessRequest struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	UserEmail  string     `json:"user_email,omitempty"`
	Action     string     `json:"action"`
	InstanceID *int64     `json:"instance_id,omitempty"`
	Reason     string     `json:"reason"`
	Status     string     `json:"status"`
	ReviewedBy *int64     `json:"reviewed_by,omitempty"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	ReviewNote string     `json:"review_note"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type OpenClawConnection struct {
	ID           int64      `json:"id"`
	InstanceID   int64      `json:"instance_id"`
	Kind         string     `json:"kind"`
	Status       string     `json:"status"`
	Label        string     `json:"label"`
	UsageCount   int64      `json:"usage_count"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	SetupURL     string     `json:"setup_url,omitempty"`
	SetupKey     string     `json:"setup_key,omitempty"`
	SetupMessage string     `json:"setup_message,omitempty"`
}

type OpenClawConnectionKindInfo struct {
	Kind           string `json:"kind"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	SortOrder      int    `json:"sort_order"`
	SetupAvailable bool   `json:"setup_available"`
}

// OpenClawAPIKey is the public billing identity attached to a workspace.
// The secret itself remains in the normal API-key list; the tenant dashboard
// only needs safe status and usage fields.
type OpenClawAPIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Quota      float64    `json:"quota"`
	QuotaUsed  float64    `json:"quota_used"`
	UsageCount int64      `json:"usage_count"`
	UsageCost  float64    `json:"usage_cost"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	secret     string     `json:"-"`
	groupID    *int64
}

type OpenClawDashboard struct {
	GatewayEnabled    bool             `json:"gateway_enabled"`
	EnrollmentEnabled bool             `json:"enrollment_enabled"`
	Instance          OpenClawInstance `json:"instance"`
	FleetState        string           `json:"fleet_state"`
	LatencyMS         *int             `json:"latency_ms,omitempty"`
	CheckedAt         time.Time        `json:"checked_at"`
	ConnectionCount   int              `json:"connection_count"`
	ConnectedCount    int              `json:"connected_count"`
	UsageCount        int64            `json:"usage_count"`
	LastUsedAt        *time.Time       `json:"last_used_at,omitempty"`
	APIKey            *OpenClawAPIKey  `json:"api_key,omitempty"`
}

type OpenClawAdminInstance struct {
	OpenClawInstance
	UserID          int64      `json:"user_id"`
	UserEmail       string     `json:"user_email"`
	BillingGroup    string     `json:"billing_group"`
	BillingRate     float64    `json:"billing_rate"`
	APIKeyStatus    string     `json:"api_key_status"`
	UsageCost       float64    `json:"usage_cost"`
	AllowedModels   []string   `json:"allowed_models"`
	DefaultModel    string     `json:"default_model"`
	MappingEnabled  bool       `json:"mapping_enabled"`
	ConnectionCount int        `json:"connection_count"`
	ConnectedCount  int        `json:"connected_count"`
	UsageCount      int64      `json:"usage_count"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
	LatencyMS       *int       `json:"latency_ms,omitempty"`
	LastSeenAt      *time.Time `json:"last_seen_at,omitempty"`
}

// OpenClawFleetClient is the narrow server-side boundary to the UK02 fleet
// manager. It intentionally exposes lifecycle operations only; customers never
// receive a shell, a container token, or a host path.
type OpenClawFleetClient interface {
	Enabled() bool
	Create(context.Context, string) (OpenClawFleetResult, error)
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Delete(context.Context, string) error
	Status(context.Context, string) (OpenClawFleetResult, error)
	Connect(context.Context, string, string, string) (OpenClawFleetResult, error)
}

// OpenClawFleetConfigurator is implemented by the private worker bridge. It
// receives a tenant's ordinary Sub2API key over the authenticated bridge and
// writes the model/provider map inside that tenant cell. The key never enters
// a browser response or a log line.
type OpenClawFleetConfigurator interface {
	Configure(context.Context, string, OpenClawModelMapping) error
}

type OpenClawModelAlias struct {
	Alias string `json:"alias"`
	Model string `json:"model"`
}

type OpenClawModelMapping struct {
	GatewayURL   string               `json:"gateway_url"`
	APIKey       string               `json:"api_key"`
	DefaultModel string               `json:"default_model,omitempty"`
	Models       []OpenClawModelAlias `json:"models"`
}

type OpenClawFleetResult struct {
	State              string                                       `json:"state"`
	Port               int                                          `json:"port,omitempty"`
	LatencyMS          *int                                         `json:"latency_ms,omitempty"`
	SetupURL           string                                       `json:"setup_url,omitempty"`
	SetupKey           string                                       `json:"setup_key,omitempty"`
	Message            string                                       `json:"message,omitempty"`
	BrowserURL         string                                       `json:"browser_url,omitempty"`
	Devices            *openclawgateway.Inventory                   `json:"devices,omitempty"`
	EnrollmentBindings []openclawgateway.EnrollmentBindingTransport `json:"enrollment_bindings,omitempty"`
	EnrollmentResult   *openclawgateway.EnrollmentBridgeResult      `json:"enrollment_result,omitempty"`
}

// Optional capability so lifecycle-only fleet implementations remain compatible.
type OpenClawGatewayLauncher interface {
	Dashboard(context.Context, string) (OpenClawFleetResult, error)
}

// HTTPOpenClawFleetClient speaks to the private UK02 bridge when the
// Sub2API process is not running on UK02 itself. The bridge token is supplied
// only through the server environment and is never returned to the browser.
type HTTPOpenClawFleetClient struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func NewHTTPOpenClawFleetClient(baseURL, token string) *HTTPOpenClawFleetClient {
	return &HTTPOpenClawFleetClient{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Token:   strings.TrimSpace(token),
		Client:  &http.Client{Timeout: 5 * time.Minute},
	}
}

func (c *HTTPOpenClawFleetClient) Enabled() bool {
	return c != nil && c.BaseURL != "" && len(c.Token) >= 32
}

func (c *HTTPOpenClawFleetClient) call(ctx context.Context, action, tenant string) (OpenClawFleetResult, error) {
	return c.callBody(ctx, action, tenant, nil)
}

func (c *HTTPOpenClawFleetClient) callBody(ctx context.Context, action, tenant string, body io.Reader) (OpenClawFleetResult, error) {
	if !c.Enabled() {
		return OpenClawFleetResult{}, ErrOpenClawFleetDisabled
	}
	if !validOpenClawTenantSlug(tenant) {
		return OpenClawFleetResult{}, errors.New("invalid openclaw tenant slug")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/cells/"+tenant+"/"+action, body)
	if err != nil {
		return OpenClawFleetResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return OpenClawFleetResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OpenClawFleetResult{}, fmt.Errorf("openclaw worker returned HTTP %d", resp.StatusCode)
	}
	var result OpenClawFleetResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&result); err != nil {
		return OpenClawFleetResult{}, err
	}
	return result, nil
}

func (c *HTTPOpenClawFleetClient) Create(ctx context.Context, tenant string) (OpenClawFleetResult, error) {
	return c.call(ctx, "create", tenant)
}
func (c *HTTPOpenClawFleetClient) Start(ctx context.Context, tenant string) error {
	_, err := c.call(ctx, "start", tenant)
	return err
}
func (c *HTTPOpenClawFleetClient) Stop(ctx context.Context, tenant string) error {
	_, err := c.call(ctx, "stop", tenant)
	return err
}
func (c *HTTPOpenClawFleetClient) Delete(ctx context.Context, tenant string) error {
	_, err := c.call(ctx, "delete", tenant)
	return err
}
func (c *HTTPOpenClawFleetClient) Status(ctx context.Context, tenant string) (OpenClawFleetResult, error) {
	return c.call(ctx, "status", tenant)
}

func (c *HTTPOpenClawFleetClient) Dashboard(ctx context.Context, tenant string) (OpenClawFleetResult, error) {
	return c.call(ctx, "dashboard", tenant)
}

func (c *HTTPOpenClawFleetClient) Connect(ctx context.Context, tenant, kind, credential string) (OpenClawFleetResult, error) {
	body, err := json.Marshal(map[string]string{"credential": credential, "account": "main", "name": map[string]string{"telegram": "Telegram", "wechat": "WeChat", "gws": "Google Workspace"}[kind]})
	if err != nil {
		return OpenClawFleetResult{}, err
	}
	return c.callBody(ctx, "connections/"+kind, tenant, strings.NewReader(string(body)))
}

func (c *HTTPOpenClawFleetClient) Configure(ctx context.Context, tenant string, mapping OpenClawModelMapping) error {
	body, err := json.Marshal(mapping)
	if err != nil {
		return err
	}
	_, err = c.callBody(ctx, "configure", tenant, strings.NewReader(string(body)))
	return err
}

// NewConfiguredOpenClawFleetClient prefers the private bridge when configured;
// otherwise it invokes the local CLI. This keeps the same control-plane API
// across a UK02 deployment and a future separately hosted Sub2API backend.
func NewConfiguredOpenClawFleetClient() OpenClawFleetClient {
	if baseURL := strings.TrimSpace(os.Getenv("OPENCLAW_FLEET_BRIDGE_URL")); baseURL != "" {
		return NewHTTPOpenClawFleetClient(baseURL, os.Getenv("OPENCLAW_FLEET_BRIDGE_TOKEN"))
	}
	return NewCommandOpenClawFleetClient()
}

// CommandOpenClawFleetClient invokes the local OpenClaw CLI without a shell.
// It is disabled by default so a development Sub2API process cannot mutate a
// fleet accidentally. UK02 enables it explicitly with OPENCLAW_FLEET_ENABLED=1.
type CommandOpenClawFleetClient struct {
	Binary  string
	Runtime string
}

func NewCommandOpenClawFleetClient() *CommandOpenClawFleetClient {
	binary := strings.TrimSpace(os.Getenv("OPENCLAW_BIN"))
	if binary == "" {
		binary = "openclaw"
	}
	runtime := strings.TrimSpace(os.Getenv("OPENCLAW_FLEET_RUNTIME"))
	if runtime == "" {
		runtime = "podman"
	}
	return &CommandOpenClawFleetClient{Binary: binary, Runtime: runtime}
}

func (c *CommandOpenClawFleetClient) Enabled() bool {
	return c != nil && (os.Getenv("OPENCLAW_FLEET_ENABLED") == "1" || strings.EqualFold(os.Getenv("OPENCLAW_FLEET_ENABLED"), "true"))
}

func (c *CommandOpenClawFleetClient) run(ctx context.Context, args ...string) ([]byte, error) {
	if !c.Enabled() {
		return nil, ErrOpenClawFleetDisabled
	}
	cmd := exec.CommandContext(ctx, c.Binary, args...)
	cmd.Env = append(os.Environ(), "OPENCLAW_FLEET_RUNTIME="+c.Runtime)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("openclaw fleet command failed: %s", message)
	}
	return out, nil
}

func (c *CommandOpenClawFleetClient) Create(ctx context.Context, tenant string) (OpenClawFleetResult, error) {
	args := []string{"fleet", "create", tenant, "--runtime", c.Runtime, "--json"}
	out, err := c.run(ctx, args...)
	if err != nil {
		return OpenClawFleetResult{}, err
	}
	var result struct {
		Port int `json:"port"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		// Older fleet builds may print human-readable output. Creation still
		// succeeded; status can be refreshed by a later fleet reconciliation.
		return OpenClawFleetResult{}, nil
	}
	return OpenClawFleetResult{Port: result.Port}, nil
}

func (c *CommandOpenClawFleetClient) Start(ctx context.Context, tenant string) error {
	_, err := c.run(ctx, "fleet", "start", tenant)
	return err
}

func (c *CommandOpenClawFleetClient) Stop(ctx context.Context, tenant string) error {
	_, err := c.run(ctx, "fleet", "stop", tenant)
	return err
}

func (c *CommandOpenClawFleetClient) Delete(ctx context.Context, tenant string) error {
	_, err := c.run(ctx, "fleet", "rm", "--force", "--purge-data", tenant)
	return err
}
func (c *CommandOpenClawFleetClient) Status(ctx context.Context, tenant string) (OpenClawFleetResult, error) {
	out, err := c.run(ctx, "fleet", "status", tenant, "--json")
	if err != nil {
		return OpenClawFleetResult{}, err
	}
	var result OpenClawFleetResult
	if err := json.Unmarshal(out, &result); err != nil {
		return OpenClawFleetResult{}, err
	}
	return result, nil
}

func (c *CommandOpenClawFleetClient) Dashboard(ctx context.Context, tenant string) (OpenClawFleetResult, error) {
	return OpenClawFleetResult{}, ErrOpenClawProviderUnavailable
}

func (c *CommandOpenClawFleetClient) Connect(ctx context.Context, tenant, kind, credential string) (OpenClawFleetResult, error) {
	return OpenClawFleetResult{}, ErrOpenClawProviderUnavailable
}

func (c *CommandOpenClawFleetClient) Configure(ctx context.Context, tenant string, mapping OpenClawModelMapping) error {
	return ErrOpenClawProviderUnavailable
}

type OpenClawService struct {
	db      *sql.DB
	fleet   OpenClawFleetClient
	apiKeys *APIKeyService
}

func NewOpenClawService(db *sql.DB, fleet OpenClawFleetClient) *OpenClawService {
	return &OpenClawService{db: db, fleet: fleet}
}

// SetAPIKeyService attaches the existing API-key and billing path without
// changing the small constructor used by service-level tests.
func (s *OpenClawService) SetAPIKeyService(apiKeys *APIKeyService) {
	s.apiKeys = apiKeys
}

// ValidateAPIKeyGroupChange prevents a customer from moving the managed
// workspace key to another group and thereby bypassing managed OpenClaw pricing.
func (s *OpenClawService) ValidateAPIKeyGroupChange(ctx context.Context, apiKeyID, userID, groupID int64) error {
	var instanceID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT i.id
		FROM openclaw_api_keys link
		JOIN openclaw_instances i ON i.id = link.instance_id AND i.user_id = $2
		WHERE link.api_key_id = $1`, apiKeyID, userID).Scan(&instanceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	group, err := s.apiKeys.EnsureOpenClawGroup(ctx)
	if err != nil {
		return err
	}
	if groupID != group.ID {
		return ErrOpenClawGroupImmutable
	}
	return nil
}

func (s *OpenClawService) GetAccessPolicy(ctx context.Context, userID int64) (OpenClawAccessPolicy, error) {
	policy := OpenClawAccessPolicy{UserID: userID, SelfServiceEnabled: true, HubName: OpenClawHubDisplayName}
	err := s.db.QueryRowContext(ctx, `
		SELECT self_service_enabled
		FROM openclaw_user_policy WHERE user_id = $1`, userID).Scan(&policy.SelfServiceEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return policy, nil
	}
	if err != nil {
		return OpenClawAccessPolicy{}, err
	}
	return policy, nil
}

func (s *OpenClawService) SetAccessPolicy(ctx context.Context, userID, adminID int64, enabled bool) (OpenClawAccessPolicy, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO openclaw_user_policy (user_id, self_service_enabled, updated_by, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			self_service_enabled = EXCLUDED.self_service_enabled,
			updated_by = EXCLUDED.updated_by,
			updated_at = NOW()`, userID, enabled, adminID)
	if err != nil {
		return OpenClawAccessPolicy{}, err
	}
	return s.GetAccessPolicy(ctx, userID)
}

func (s *OpenClawService) CreateAccessRequest(ctx context.Context, userID int64, action string, instanceID *int64, reason string) (OpenClawAccessRequest, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	if action != "create" && action != "delete" {
		return OpenClawAccessRequest{}, errors.New("openclaw access request action must be create or delete")
	}
	if action == "delete" {
		if instanceID == nil || *instanceID <= 0 {
			return OpenClawAccessRequest{}, errors.New("instance_id is required for a delete request")
		}
		if _, err := s.GetInstance(ctx, userID, *instanceID); err != nil {
			return OpenClawAccessRequest{}, err
		}
	} else {
		instanceID = nil
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > 1000 {
		return OpenClawAccessRequest{}, errors.New("request reason must contain at most 1000 characters")
	}
	var pendingID int64
	check := `SELECT id FROM openclaw_access_requests WHERE user_id = $1 AND action = $2 AND COALESCE(instance_id, 0) = COALESCE($3::bigint, 0) AND status = 'pending'`
	if err := s.db.QueryRowContext(ctx, check, userID, action, instanceID).Scan(&pendingID); err == nil {
		return OpenClawAccessRequest{}, ErrOpenClawRequestPending
	} else if !errors.Is(err, sql.ErrNoRows) {
		return OpenClawAccessRequest{}, err
	}
	return s.scanAccessRequest(s.db.QueryRowContext(ctx, `
		INSERT INTO openclaw_access_requests (user_id, action, instance_id, reason)
		VALUES ($1, $2, $3, $4)
		RETURNING id, user_id, action, instance_id, reason, status, reviewed_by, reviewed_at,
	          review_note, created_at, updated_at`, userID, action, instanceID, reason))
}

func (s *OpenClawService) ListAccessRequests(ctx context.Context, status string) ([]OpenClawAccessRequest, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	query := `
		SELECT r.id, r.user_id, u.email, r.action, r.instance_id, r.reason, r.status,
		       r.reviewed_by, r.reviewed_at, r.review_note, r.created_at, r.updated_at
		FROM openclaw_access_requests r JOIN users u ON u.id = r.user_id`
	args := []any{}
	if status != "" {
		query += ` WHERE r.status = $1`
		args = append(args, status)
	}
	query += ` ORDER BY r.created_at ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var requests []OpenClawAccessRequest
	for rows.Next() {
		var request OpenClawAccessRequest
		var instanceID, reviewedBy sql.NullInt64
		var reviewedAt sql.NullTime
		if err := rows.Scan(&request.ID, &request.UserID, &request.UserEmail, &request.Action, &instanceID, &request.Reason, &request.Status, &reviewedBy, &reviewedAt, &request.ReviewNote, &request.CreatedAt, &request.UpdatedAt); err != nil {
			return nil, err
		}
		if instanceID.Valid {
			value := instanceID.Int64
			request.InstanceID = &value
		}
		if reviewedBy.Valid {
			value := reviewedBy.Int64
			request.ReviewedBy = &value
		}
		if reviewedAt.Valid {
			request.ReviewedAt = &reviewedAt.Time
		}
		requests = append(requests, request)
	}
	return requests, rows.Err()
}

func (s *OpenClawService) ReviewAccessRequest(ctx context.Context, requestID, adminID int64, approved bool, note string) (OpenClawAccessRequest, error) {
	status := "rejected"
	if approved {
		status = "approved"
	}
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 1000 {
		return OpenClawAccessRequest{}, errors.New("review note must contain at most 1000 characters")
	}
	request, err := s.scanAccessRequest(s.db.QueryRowContext(ctx, `
		UPDATE openclaw_access_requests
		SET status = $1, reviewed_by = $2, reviewed_at = NOW(), review_note = $3, updated_at = NOW()
		WHERE id = $4 AND status = 'pending'
		RETURNING id, user_id, action, instance_id, reason, status, reviewed_by, reviewed_at,
		          review_note, created_at, updated_at`, status, adminID, note, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return OpenClawAccessRequest{}, ErrOpenClawRequestNotFound
	}
	return request, err
}

func (s *OpenClawService) scanAccessRequest(scanner openClawScanner) (OpenClawAccessRequest, error) {
	var request OpenClawAccessRequest
	var instanceID, reviewedBy sql.NullInt64
	var reviewedAt sql.NullTime
	if err := scanner.Scan(&request.ID, &request.UserID, &request.Action, &instanceID, &request.Reason, &request.Status, &reviewedBy, &reviewedAt, &request.ReviewNote, &request.CreatedAt, &request.UpdatedAt); err != nil {
		return OpenClawAccessRequest{}, err
	}
	if instanceID.Valid {
		value := instanceID.Int64
		request.InstanceID = &value
	}
	if reviewedBy.Valid {
		value := reviewedBy.Int64
		request.ReviewedBy = &value
	}
	if reviewedAt.Valid {
		request.ReviewedAt = &reviewedAt.Time
	}
	return request, nil
}

func (s *OpenClawService) consumeApprovedRequest(ctx context.Context, userID int64, action string, instanceID *int64) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE openclaw_access_requests
		SET status = 'consumed', updated_at = NOW()
		WHERE id = (
			SELECT id FROM openclaw_access_requests
			WHERE user_id = $1 AND action = $2 AND status = 'approved'
			  AND COALESCE(instance_id, 0) = COALESCE($3::bigint, 0)
			ORDER BY reviewed_at ASC, id ASC LIMIT 1
		)`, userID, action, instanceID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrOpenClawApprovalRequired
	}
	return nil
}

func (s *OpenClawService) authorizeLifecycleAction(ctx context.Context, userID int64, action string, instanceID *int64) error {
	policy, err := s.GetAccessPolicy(ctx, userID)
	if err != nil {
		return err
	}
	if policy.SelfServiceEnabled {
		return nil
	}
	return s.consumeApprovedRequest(ctx, userID, action, instanceID)
}

func (s *OpenClawService) hasRequest(ctx context.Context, userID int64, action, status string, instanceID *int64) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM openclaw_access_requests
			WHERE user_id = $1 AND action = $2 AND status = $3
			  AND COALESCE(instance_id, 0) = COALESCE($4::bigint, 0)
		)`, userID, action, status, instanceID).Scan(&exists)
	return exists, err
}

func (s *OpenClawService) ListInstances(ctx context.Context, userID int64) ([]OpenClawInstance, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_slug, name, status, worker_host, port, COALESCE(last_error, ''), created_at, updated_at,
		       deletion_requested_at, deletion_scheduled_at
		FROM openclaw_instances WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var instances []OpenClawInstance
	for rows.Next() {
		instance, err := scanOpenClawInstance(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	return instances, rows.Err()
}

const createOpenClawInstanceSQL = `
	INSERT INTO openclaw_instances (user_id, tenant_slug, name, status, worker_host, hub_id)
	VALUES ($1, $2, $3, 'provisioning', $4::text,
	        (SELECT id FROM openclaw_hubs WHERE slug = $4::text))
	RETURNING id, tenant_slug, name, status, worker_host, port, COALESCE(last_error, ''), created_at, updated_at,
	          deletion_requested_at, deletion_scheduled_at`

func (s *OpenClawService) CreateInstance(ctx context.Context, userID int64, name string) (OpenClawInstance, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return OpenClawInstance{}, errors.New("instance name must contain 1 to 100 characters")
	}
	var recreateAllowedAt time.Time
	if err := s.db.QueryRowContext(ctx, `SELECT recreate_allowed_at FROM openclaw_user_cooldowns WHERE user_id = $1`, userID).Scan(&recreateAllowedAt); err == nil {
		if time.Now().UTC().Before(recreateAllowedAt) {
			return OpenClawInstance{}, ErrOpenClawRecreateCooldown
		}
		_, _ = s.db.ExecContext(ctx, `DELETE FROM openclaw_user_cooldowns WHERE user_id = $1`, userID)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return OpenClawInstance{}, err
	}
	var existingID int64
	var existingStatus string
	var deletionScheduledAt sql.NullTime
	if err := s.db.QueryRowContext(ctx, `SELECT id, status, deletion_scheduled_at FROM openclaw_instances WHERE user_id = $1`, userID).Scan(&existingID, &existingStatus, &deletionScheduledAt); err == nil {
		if existingStatus == "deletion_scheduled" || deletionScheduledAt.Valid {
			return OpenClawInstance{}, ErrOpenClawDeletionScheduled
		}
		return OpenClawInstance{}, ErrOpenClawInstanceExists
	} else if !errors.Is(err, sql.ErrNoRows) {
		return OpenClawInstance{}, err
	}
	if err := s.authorizeLifecycleAction(ctx, userID, "create", nil); err != nil {
		return OpenClawInstance{}, err
	}
	slug, err := newOpenClawTenantSlug(userID)
	if err != nil {
		return OpenClawInstance{}, err
	}
	instance, err := scanOpenClawInstance(s.db.QueryRowContext(ctx, createOpenClawInstanceSQL,
		userID, slug, name, OpenClawHubSlug))
	if err != nil {
		// The unique index is the concurrency guard when two browser tabs submit
		// at the same time. Keep the public error user-friendly.
		if strings.Contains(strings.ToLower(err.Error()), "idx_openclaw_instances_one_per_user") || strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			return OpenClawInstance{}, ErrOpenClawInstanceExists
		}
		return OpenClawInstance{}, err
	}
	// Create the billing identity before touching UK02. A workspace is not
	// considered complete unless its API key is present in the user's normal
	// key list and linked to this tenant.
	var billingKey *OpenClawAPIKey
	if s.apiKeys != nil {
		var keyErr error
		billingKey, keyErr = s.ensureInstanceAPIKey(ctx, userID, instance.ID, name)
		if keyErr != nil {
			_, _ = s.db.ExecContext(ctx, `DELETE FROM openclaw_instances WHERE id = $1 AND user_id = $2`, instance.ID, userID)
			return OpenClawInstance{}, fmt.Errorf("create openclaw billing key: %w", keyErr)
		}
	}
	mapping, mappingEnabled, mappingErr := configuredOpenClawModelMapping(func() string {
		if billingKey != nil {
			return billingKey.secret
		}
		return ""
	}())
	if mappingErr != nil {
		if s.apiKeys != nil {
			_ = s.deleteInstanceAPIKey(ctx, userID, instance.ID)
		}
		_, _ = s.db.ExecContext(ctx, `DELETE FROM openclaw_instances WHERE id = $1 AND user_id = $2`, instance.ID, userID)
		return OpenClawInstance{}, fmt.Errorf("configure OpenClaw model mapping: %w", mappingErr)
	}

	if s.fleet == nil || !s.fleet.Enabled() {
		// Do not leave a dashboard record that looks like a workspace exists
		// when the UK02 bridge cannot create its actual tenant cell.
		if s.apiKeys != nil {
			_ = s.deleteInstanceAPIKey(ctx, userID, instance.ID)
		}
		_, _ = s.db.ExecContext(ctx, `DELETE FROM openclaw_instances WHERE id = $1 AND user_id = $2`, instance.ID, userID)
		return OpenClawInstance{}, ErrOpenClawFleetDisabled
	}

	provisionCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	result, fleetErr := s.fleet.Create(provisionCtx, instance.TenantSlug)
	if fleetErr != nil {
		_, updateErr := s.db.ExecContext(ctx, `UPDATE openclaw_instances SET status = 'error', last_error = $1, updated_at = NOW() WHERE id = $2 AND user_id = $3`, fleetErr.Error(), instance.ID, userID)
		if updateErr != nil {
			return OpenClawInstance{}, updateErr
		}
		instance.Status = "error"
		instance.LastError = fleetErr.Error()
		return instance, nil
	}
	if mappingEnabled {
		configurator, ok := s.fleet.(OpenClawFleetConfigurator)
		if !ok {
			message := "OpenClaw worker does not support model mapping"
			_, _ = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET status = 'error', last_error = $1, updated_at = NOW() WHERE id = $2 AND user_id = $3`, message, instance.ID, userID)
			instance.Status = "error"
			instance.LastError = message
			return instance, nil
		}
		if configErr := configurator.Configure(provisionCtx, instance.TenantSlug, mapping); configErr != nil {
			_, _ = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET status = 'error', last_error = $1, updated_at = NOW() WHERE id = $2 AND user_id = $3`, configErr.Error(), instance.ID, userID)
			instance.Status = "error"
			instance.LastError = configErr.Error()
			return instance, nil
		}
	}
	if result.Port > 0 {
		instance.Port = &result.Port
		_, err = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET status = 'ready', port = $1, last_error = NULL, updated_at = NOW() WHERE id = $2 AND user_id = $3`, result.Port, instance.ID, userID)
	} else {
		_, err = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET status = 'ready', last_error = NULL, updated_at = NOW() WHERE id = $1 AND user_id = $2`, instance.ID, userID)
	}
	if err != nil {
		return OpenClawInstance{}, err
	}
	instance.Status = "ready"
	instance.LastError = ""
	return instance, nil
}

func (s *OpenClawService) RenameInstance(ctx context.Context, userID, instanceID int64, name string) (OpenClawInstance, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return OpenClawInstance{}, errors.New("instance name must contain 1 to 100 characters")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE openclaw_instances SET name = $1, updated_at = NOW() WHERE id = $2 AND user_id = $3`, name, instanceID, userID)
	if err != nil {
		return OpenClawInstance{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return OpenClawInstance{}, ErrOpenClawInstanceNotFound
	}
	return s.GetInstance(ctx, userID, instanceID)
}

func (s *OpenClawService) GetLifecycle(ctx context.Context, userID int64) (OpenClawLifecycle, error) {
	instances, err := s.ListInstances(ctx, userID)
	if err != nil {
		return OpenClawLifecycle{}, err
	}
	policy, err := s.GetAccessPolicy(ctx, userID)
	if err != nil {
		return OpenClawLifecycle{}, err
	}
	createPending, err := s.hasRequest(ctx, userID, "create", "pending", nil)
	if err != nil {
		return OpenClawLifecycle{}, err
	}
	createApproved, err := s.hasRequest(ctx, userID, "create", "approved", nil)
	if err != nil {
		return OpenClawLifecycle{}, err
	}
	lifecycle := OpenClawLifecycle{
		HubName:              policy.HubName,
		CanCreate:            len(instances) == 0 && (policy.SelfServiceEnabled || createApproved),
		CanDelete:            len(instances) > 0 && policy.SelfServiceEnabled,
		SelfServiceEnabled:   policy.SelfServiceEnabled,
		CreateRequestPending: createPending,
	}
	if len(instances) > 0 {
		lifecycle.Instance = &instances[0]
		deletePending, requestErr := s.hasRequest(ctx, userID, "delete", "pending", &instances[0].ID)
		if requestErr != nil {
			return OpenClawLifecycle{}, requestErr
		}
		lifecycle.DeleteRequestPending = deletePending
		deleteApproved, requestErr := s.hasRequest(ctx, userID, "delete", "approved", &instances[0].ID)
		if requestErr != nil {
			return OpenClawLifecycle{}, requestErr
		}
		lifecycle.CanDelete = policy.SelfServiceEnabled || deleteApproved
	}
	var allowedAt time.Time
	err = s.db.QueryRowContext(ctx, `SELECT recreate_allowed_at FROM openclaw_user_cooldowns WHERE user_id = $1`, userID).Scan(&allowedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return lifecycle, nil
	}
	if err != nil {
		return OpenClawLifecycle{}, err
	}
	if time.Now().UTC().Before(allowedAt) {
		lifecycle.RecreateAllowedAt = &allowedAt
		lifecycle.CanCreate = false
		return lifecycle, nil
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM openclaw_user_cooldowns WHERE user_id = $1`, userID)
	return lifecycle, nil
}

// ProvisionInstance reconciles a control-plane workspace with its UK02 cell.
// It is safe to repeat: the worker returns the existing cell when one already
// exists and creates it only when the tenant is actually missing.
func (s *OpenClawService) ProvisionInstance(ctx context.Context, userID, instanceID int64) (OpenClawInstance, error) {
	instance, err := s.GetInstance(ctx, userID, instanceID)
	if err != nil {
		return OpenClawInstance{}, err
	}
	if instance.DeletionScheduledAt != nil {
		return OpenClawInstance{}, ErrOpenClawDeletionScheduled
	}
	if s.fleet == nil || !s.fleet.Enabled() {
		return OpenClawInstance{}, ErrOpenClawFleetDisabled
	}
	if err := s.authorizeLifecycleAction(ctx, userID, "create", nil); err != nil {
		return OpenClawInstance{}, err
	}
	var billingKey *OpenClawAPIKey
	if s.apiKeys != nil {
		billingKey, err = s.ensureInstanceAPIKey(ctx, userID, instanceID, instance.Name)
		if err != nil {
			return OpenClawInstance{}, fmt.Errorf("ensure openclaw billing key: %w", err)
		}
	}
	var mapping OpenClawModelMapping
	var mappingEnabled bool
	if billingKey != nil {
		fullKey, keyErr := s.apiKeys.GetByID(ctx, billingKey.ID)
		if keyErr != nil {
			return OpenClawInstance{}, fmt.Errorf("load openclaw billing key: %w", keyErr)
		}
		mapping, mappingEnabled, err = configuredOpenClawModelMapping(fullKey.Key)
		if err != nil {
			return OpenClawInstance{}, fmt.Errorf("configure OpenClaw model mapping: %w", err)
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET status = 'provisioning', last_error = NULL, updated_at = NOW() WHERE id = $1 AND user_id = $2`, instanceID, userID)
	if err != nil {
		return OpenClawInstance{}, err
	}
	provisionCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	result, fleetErr := s.fleet.Create(provisionCtx, instance.TenantSlug)
	if fleetErr != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET status = 'error', last_error = $1, updated_at = NOW() WHERE id = $2 AND user_id = $3`, fleetErr.Error(), instanceID, userID)
		return OpenClawInstance{}, fleetErr
	}
	if mappingEnabled {
		configurator, ok := s.fleet.(OpenClawFleetConfigurator)
		if !ok {
			message := "OpenClaw worker does not support model mapping"
			_, _ = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET status = 'error', last_error = $1, updated_at = NOW() WHERE id = $2 AND user_id = $3`, message, instanceID, userID)
			return OpenClawInstance{}, errors.New(message)
		}
		if configErr := configurator.Configure(provisionCtx, instance.TenantSlug, mapping); configErr != nil {
			_, _ = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET status = 'error', last_error = $1, updated_at = NOW() WHERE id = $2 AND user_id = $3`, configErr.Error(), instanceID, userID)
			return OpenClawInstance{}, fmt.Errorf("configure OpenClaw model mapping: %w", configErr)
		}
	}
	if result.Port > 0 {
		_, err = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET status = 'ready', port = $1, last_error = NULL, updated_at = NOW() WHERE id = $2 AND user_id = $3`, result.Port, instanceID, userID)
	} else {
		_, err = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET status = 'ready', last_error = NULL, updated_at = NOW() WHERE id = $1 AND user_id = $2`, instanceID, userID)
	}
	if err != nil {
		return OpenClawInstance{}, err
	}
	return s.GetInstance(ctx, userID, instanceID)
}

func (s *OpenClawService) GetInstance(ctx context.Context, userID, instanceID int64) (OpenClawInstance, error) {
	instance, err := scanOpenClawInstance(s.db.QueryRowContext(ctx, `
		SELECT id, tenant_slug, name, status, worker_host, port, COALESCE(last_error, ''), created_at, updated_at,
		       deletion_requested_at, deletion_scheduled_at
		FROM openclaw_instances WHERE id = $1 AND user_id = $2`, instanceID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return OpenClawInstance{}, ErrOpenClawInstanceNotFound
	}
	if err != nil {
		return OpenClawInstance{}, err
	}
	return instance, nil
}

func (s *OpenClawService) StartInstance(ctx context.Context, userID, instanceID int64) error {
	instance, err := s.GetInstance(ctx, userID, instanceID)
	if err != nil {
		return err
	}
	if instance.DeletionScheduledAt != nil {
		return ErrOpenClawDeletionScheduled
	}
	if s.fleet == nil || !s.fleet.Enabled() {
		return ErrOpenClawFleetDisabled
	}
	if err := s.fleet.Start(ctx, instance.TenantSlug); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET status = 'ready', last_error = NULL, updated_at = NOW() WHERE id = $1 AND user_id = $2`, instanceID, userID)
	return err
}

func (s *OpenClawService) StopInstance(ctx context.Context, userID, instanceID int64) error {
	instance, err := s.GetInstance(ctx, userID, instanceID)
	if err != nil {
		return err
	}
	if instance.DeletionScheduledAt != nil {
		return ErrOpenClawDeletionScheduled
	}
	if s.fleet == nil || !s.fleet.Enabled() {
		return ErrOpenClawFleetDisabled
	}
	if err := s.fleet.Stop(ctx, instance.TenantSlug); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET status = 'stopped', updated_at = NOW() WHERE id = $1 AND user_id = $2`, instanceID, userID)
	return err
}

func (s *OpenClawService) DeleteInstance(ctx context.Context, userID, instanceID int64) (OpenClawInstance, error) {
	return s.deleteInstance(ctx, userID, instanceID, false)
}

func (s *OpenClawService) deleteInstance(ctx context.Context, userID, instanceID int64, bypassApproval bool) (OpenClawInstance, error) {
	instance, err := s.GetInstance(ctx, userID, instanceID)
	if err != nil {
		return OpenClawInstance{}, err
	}
	if instance.DeletionScheduledAt != nil {
		return instance, nil
	}
	if !bypassApproval {
		if err := s.authorizeLifecycleAction(ctx, userID, "delete", &instanceID); err != nil {
			return OpenClawInstance{}, err
		}
	}
	updated, err := scanOpenClawInstance(s.db.QueryRowContext(ctx, `
		UPDATE openclaw_instances
		SET status = 'deletion_scheduled',
		    deletion_requested_at = COALESCE(deletion_requested_at, NOW()),
		    deletion_scheduled_at = COALESCE(deletion_scheduled_at, NOW() + INTERVAL '7 days'),
		    last_error = NULL,
		    updated_at = NOW()
		WHERE id = $1 AND user_id = $2
		RETURNING id, tenant_slug, name, status, worker_host, port, COALESCE(last_error, ''), created_at, updated_at,
		          deletion_requested_at, deletion_scheduled_at`, instanceID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return OpenClawInstance{}, ErrOpenClawInstanceNotFound
	}
	if err != nil {
		return OpenClawInstance{}, err
	}
	return updated, nil
}

// ProcessDueDeletions is the only path that removes a tenant from UK02 and
// then deletes its control-plane row. The worker retries failures on the next
// pass, so a bridge outage cannot silently turn into a false deletion.
func (s *OpenClawService) ProcessDueDeletions(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, tenant_slug
		FROM openclaw_instances
		WHERE status = 'deletion_scheduled' AND deletion_scheduled_at <= NOW()
		ORDER BY deletion_scheduled_at ASC
		LIMIT 100`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	type dueDeletion struct {
		id     int64
		userID int64
		tenant string
	}
	var due []dueDeletion
	for rows.Next() {
		var item dueDeletion
		if err := rows.Scan(&item.id, &item.userID, &item.tenant); err != nil {
			return err
		}
		due = append(due, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range due {
		if s.fleet == nil || !s.fleet.Enabled() {
			_, _ = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET last_error = $1, updated_at = NOW() WHERE id = $2`, ErrOpenClawFleetDisabled.Error(), item.id)
			continue
		}
		if err := s.fleet.Delete(ctx, item.tenant); err != nil {
			_, _ = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET last_error = $1, updated_at = NOW() WHERE id = $2`, err.Error(), item.id)
			continue
		}
		if err := s.deleteInstanceAPIKey(ctx, item.userID, item.id); err != nil {
			_, _ = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET last_error = $1, updated_at = NOW() WHERE id = $2`, err.Error(), item.id)
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM openclaw_instances WHERE id = $1 AND status = 'deletion_scheduled'`, item.id); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO openclaw_user_cooldowns (user_id, deleted_at, recreate_allowed_at)
			VALUES ($1, NOW(), NOW() + INTERVAL '7 days')
			ON CONFLICT (user_id) DO UPDATE SET deleted_at = EXCLUDED.deleted_at, recreate_allowed_at = EXCLUDED.recreate_allowed_at`, item.userID); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *OpenClawService) StartDeletionWorker(interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), interval)
			_ = s.ProcessDueDeletions(ctx)
			cancel()
		}
	}()
}

func (s *OpenClawService) loadInstanceAPIKey(ctx context.Context, instanceID int64) (*OpenClawAPIKey, error) {
	var key OpenClawAPIKey
	var groupID sql.NullInt64
	var lastUsed sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT k.id, k.name, k.status, k.quota, k.quota_used, k.group_id, k.last_used_at,
		       COUNT(u.id), COALESCE(SUM(u.actual_cost), 0)
		FROM openclaw_api_keys link
		JOIN api_keys k ON k.id = link.api_key_id AND k.deleted_at IS NULL
		LEFT JOIN usage_logs u ON u.api_key_id = k.id
		WHERE link.instance_id = $1
		GROUP BY k.id, k.name, k.status, k.quota, k.quota_used, k.group_id, k.last_used_at`, instanceID).
		Scan(&key.ID, &key.Name, &key.Status, &key.Quota, &key.QuotaUsed, &groupID, &lastUsed, &key.UsageCount, &key.UsageCost)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastUsed.Valid {
		key.LastUsedAt = &lastUsed.Time
	}
	if groupID.Valid {
		value := groupID.Int64
		key.groupID = &value
	}
	return &key, nil
}

// ensureInstanceAPIKey creates the workspace's ordinary Sub2API credential.
// Requests made with that credential therefore follow the same group pricing,
// user balance, quota, and usage-log paths as every other customer key.
func (s *OpenClawService) ensureInstanceAPIKey(ctx context.Context, userID, instanceID int64, workspaceName string) (*OpenClawAPIKey, error) {
	if s.apiKeys == nil {
		return nil, nil
	}
	group, err := s.apiKeys.EnsureOpenClawGroup(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO account_groups (account_id, group_id, priority)
		SELECT id, $1, priority
		FROM accounts
		WHERE platform = 'openai'
		  AND type = 'oauth'
		  AND status = 'active'
		  AND deleted_at IS NULL
		ON CONFLICT (account_id, group_id) DO NOTHING`, group.ID); err != nil {
		return nil, fmt.Errorf("ensure OpenClaw account pool: %w", err)
	}
	if key, err := s.loadInstanceAPIKey(ctx, instanceID); err != nil {
		return nil, err
	} else if key != nil {
		if key.groupID == nil || *key.groupID != group.ID {
			groupID := group.ID
			if _, err := s.apiKeys.update(ctx, key.ID, userID, UpdateAPIKeyRequest{GroupID: &groupID}, true); err != nil {
				return nil, fmt.Errorf("repair OpenClaw API key group: %w", err)
			}
			key.groupID = &groupID
		}
		return key, nil
	}
	// A customer may have removed the linked key from the ordinary key page.
	// Clear the stale link so the next reconciliation can create its replacement.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM openclaw_api_keys WHERE instance_id = $1`, instanceID); err != nil {
		return nil, err
	}
	groupID := group.ID
	key, err := s.apiKeys.create(ctx, userID, CreateAPIKeyRequest{
		Name:    "OpenClaw · " + strings.TrimSpace(workspaceName),
		GroupID: &groupID,
	}, true)
	if err != nil {
		return nil, err
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO openclaw_api_keys (instance_id, api_key_id)
		VALUES ($1, $2) ON CONFLICT (instance_id) DO NOTHING`, instanceID, key.ID)
	if err != nil {
		_ = s.apiKeys.Delete(ctx, key.ID, userID)
		return nil, err
	}
	createdForInstance := false
	if count, _ := result.RowsAffected(); count == 0 {
		// Another request won the unique link race. Remove the unused key and
		// return the already-linked credential.
		_ = s.apiKeys.Delete(ctx, key.ID, userID)
	} else {
		createdForInstance = true
	}
	public, err := s.loadInstanceAPIKey(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if public != nil && createdForInstance {
		public.secret = key.Key
	}
	return public, nil
}

func configuredOpenClawModelMapping(apiKey string) (OpenClawModelMapping, bool, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("OPENCLAW_MODEL_GATEWAY_BASE_URL")), "/")
	raw := strings.TrimSpace(os.Getenv("OPENCLAW_MODEL_MAPPING_JSON"))
	if baseURL == "" && raw == "" {
		return OpenClawModelMapping{}, false, nil
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return OpenClawModelMapping{}, false, errors.New("OPENCLAW_MODEL_GATEWAY_BASE_URL must be an http(s) URL")
	}
	if len(apiKey) < 20 {
		return OpenClawModelMapping{}, false, errors.New("workspace API key is unavailable for model mapping")
	}
	var payload struct {
		DefaultModel string               `json:"default_model"`
		Models       []OpenClawModelAlias `json:"models"`
	}
	if raw == "" {
		payload.DefaultModel = "gpt-5.6-sol"
		for _, model := range openClawAllowedModels {
			payload.Models = append(payload.Models, OpenClawModelAlias{Alias: model, Model: model})
		}
	} else if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return OpenClawModelMapping{}, false, fmt.Errorf("OPENCLAW_MODEL_MAPPING_JSON is invalid: %w", err)
	}
	if len(payload.Models) != len(openClawAllowedModels) {
		return OpenClawModelMapping{}, false, fmt.Errorf("OpenClaw mapping must contain exactly %d approved models", len(openClawAllowedModels))
	}
	seenModels := make(map[string]struct{}, len(payload.Models))
	seenAliases := make(map[string]struct{}, len(payload.Models))
	for _, model := range payload.Models {
		if strings.TrimSpace(model.Alias) == "" || strings.TrimSpace(model.Model) == "" || len([]rune(model.Alias)) > 100 || len([]rune(model.Model)) > 200 {
			return OpenClawModelMapping{}, false, errors.New("OpenClaw model aliases must contain non-empty names under the configured limits")
		}
		alias := strings.TrimSpace(model.Alias)
		target := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(model.Model)), "openai/")
		if !isOpenClawAllowedModel(alias) {
			return OpenClawModelMapping{}, false, fmt.Errorf("OpenClaw model alias %q is not approved", model.Alias)
		}
		if !isOpenClawAllowedModel(target) {
			return OpenClawModelMapping{}, false, fmt.Errorf("OpenClaw model %q is not approved", model.Model)
		}
		if _, exists := seenAliases[alias]; exists {
			return OpenClawModelMapping{}, false, fmt.Errorf("OpenClaw model alias %q is duplicated", alias)
		}
		seenAliases[alias] = struct{}{}
		seenModels[target] = struct{}{}
	}
	for _, model := range openClawAllowedModels {
		if _, ok := seenModels[model]; !ok {
			return OpenClawModelMapping{}, false, fmt.Errorf("OpenClaw mapping is missing approved model %q", model)
		}
	}
	if payload.DefaultModel == "" {
		payload.DefaultModel = payload.Models[0].Alias
	}
	defaultFound := false
	for _, model := range payload.Models {
		if model.Alias == payload.DefaultModel {
			defaultFound = true
			break
		}
	}
	if !defaultFound {
		return OpenClawModelMapping{}, false, errors.New("OpenClaw default_model must match an approved alias")
	}
	return OpenClawModelMapping{
		GatewayURL:   baseURL,
		APIKey:       apiKey,
		DefaultModel: payload.DefaultModel,
		Models:       payload.Models,
	}, true, nil
}

func (s *OpenClawService) deleteInstanceAPIKey(ctx context.Context, userID, instanceID int64) error {
	if s.apiKeys == nil {
		return nil
	}
	var keyID int64
	err := s.db.QueryRowContext(ctx, `SELECT api_key_id FROM openclaw_api_keys WHERE instance_id = $1`, instanceID).Scan(&keyID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.apiKeys.Delete(ctx, keyID, userID); err != nil && !errors.Is(err, ErrAPIKeyNotFound) {
		return err
	}
	return nil
}

func (s *OpenClawService) ListConnections(ctx context.Context, userID, instanceID int64) ([]OpenClawConnection, error) {
	if _, err := s.GetInstance(ctx, userID, instanceID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, instance_id, kind, status, label, usage_count, last_used_at, created_at, updated_at
		FROM openclaw_connections WHERE instance_id = $1 ORDER BY created_at ASC`, instanceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var connections []OpenClawConnection
	for rows.Next() {
		var connection OpenClawConnection
		if err := rows.Scan(&connection.ID, &connection.InstanceID, &connection.Kind, &connection.Status, &connection.Label, &connection.UsageCount, &connection.LastUsedAt, &connection.CreatedAt, &connection.UpdatedAt); err != nil {
			return nil, err
		}
		connections = append(connections, connection)
	}
	return connections, rows.Err()
}

func (s *OpenClawService) CreateConnection(ctx context.Context, userID, instanceID int64, kind, label, credential string) (OpenClawConnection, error) {
	if !validOpenClawConnectionKind(kind) && !s.connectionKindAvailable(ctx, kind) {
		return OpenClawConnection{}, ErrOpenClawInvalidKind
	}
	instance, err := s.GetInstance(ctx, userID, instanceID)
	if err != nil {
		return OpenClawConnection{}, err
	}
	if instance.DeletionScheduledAt != nil {
		return OpenClawConnection{}, ErrOpenClawDeletionScheduled
	}
	credential = strings.TrimSpace(credential)
	if kind != OpenClawConnectionTelegram && kind != OpenClawConnectionWeChat && kind != OpenClawConnectionGWS {
		return OpenClawConnection{}, ErrOpenClawProviderUnavailable
	}
	if kind == OpenClawConnectionTelegram && credential == "" {
		return OpenClawConnection{}, errors.New("telegram bot token is required")
	}
	var existing OpenClawConnection
	existingErr := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, kind, status, label, usage_count, last_used_at, created_at, updated_at
		FROM openclaw_connections WHERE instance_id = $1 AND kind = $2 ORDER BY id ASC LIMIT 1`, instanceID, kind).Scan(
		&existing.ID, &existing.InstanceID, &existing.Kind, &existing.Status, &existing.Label, &existing.UsageCount, &existing.LastUsedAt, &existing.CreatedAt, &existing.UpdatedAt)
	if existingErr == nil && existing.Status == "connected" {
		return existing, nil
	}
	if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
		return OpenClawConnection{}, existingErr
	}
	if s.fleet == nil || !s.fleet.Enabled() {
		return OpenClawConnection{}, ErrOpenClawFleetDisabled
	}
	connectCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := s.fleet.Connect(connectCtx, instance.TenantSlug, kind, credential)
	if err != nil {
		return OpenClawConnection{}, err
	}
	label = strings.TrimSpace(label)
	if len([]rune(label)) > 100 {
		return OpenClawConnection{}, errors.New("connection label must contain at most 100 characters")
	}
	status := result.State
	if status == "" {
		status = "connected"
	}
	var connection OpenClawConnection
	if existingErr == nil {
		err = s.db.QueryRowContext(ctx, `
			UPDATE openclaw_connections SET status = $1, label = $2, updated_at = NOW()
			WHERE id = $3
			RETURNING id, instance_id, kind, status, label, usage_count, last_used_at, created_at, updated_at`, status, label, existing.ID).Scan(
			&connection.ID, &connection.InstanceID, &connection.Kind, &connection.Status, &connection.Label, &connection.UsageCount, &connection.LastUsedAt, &connection.CreatedAt, &connection.UpdatedAt)
	} else {
		err = s.db.QueryRowContext(ctx, `
			INSERT INTO openclaw_connections (instance_id, kind, status, label)
			VALUES ($1, $2, $3, $4)
			RETURNING id, instance_id, kind, status, label, usage_count, last_used_at, created_at, updated_at`, instanceID, kind, status, label).Scan(
			&connection.ID, &connection.InstanceID, &connection.Kind, &connection.Status, &connection.Label, &connection.UsageCount, &connection.LastUsedAt, &connection.CreatedAt, &connection.UpdatedAt)
	}
	if err != nil {
		return OpenClawConnection{}, err
	}
	connection.SetupURL = result.SetupURL
	connection.SetupKey = result.SetupKey
	connection.SetupMessage = result.Message
	return connection, nil
}

// CompleteConnection resumes a provider flow that was started by
// CreateConnection. The session key is kept in the browser only long enough
// to finish that one login and is never stored in the control-plane database.
func (s *OpenClawService) CompleteConnection(ctx context.Context, userID, instanceID, connectionID int64, credential string) (OpenClawConnection, error) {
	instance, err := s.GetInstance(ctx, userID, instanceID)
	if err != nil {
		return OpenClawConnection{}, err
	}
	var connection OpenClawConnection
	err = s.db.QueryRowContext(ctx, `
		SELECT c.id, c.instance_id, c.kind, c.status, c.label, c.usage_count, c.last_used_at, c.created_at, c.updated_at
		FROM openclaw_connections c
		JOIN openclaw_instances i ON i.id = c.instance_id AND i.user_id = $1
		WHERE c.id = $2 AND c.instance_id = $3`, userID, connectionID, instanceID).Scan(
		&connection.ID, &connection.InstanceID, &connection.Kind, &connection.Status, &connection.Label, &connection.UsageCount, &connection.LastUsedAt, &connection.CreatedAt, &connection.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return OpenClawConnection{}, ErrOpenClawConnectionNotFound
	}
	if err != nil {
		return OpenClawConnection{}, err
	}
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return OpenClawConnection{}, errors.New("provider session key is required")
	}
	if s.fleet == nil || !s.fleet.Enabled() {
		return OpenClawConnection{}, ErrOpenClawFleetDisabled
	}
	connectCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := s.fleet.Connect(connectCtx, instance.TenantSlug, connection.Kind, credential)
	if err != nil {
		return OpenClawConnection{}, err
	}
	status := result.State
	if status == "" {
		status = "connected"
	}
	err = s.db.QueryRowContext(ctx, `
		UPDATE openclaw_connections SET status = $1, updated_at = NOW()
		WHERE id = $2 AND instance_id = $3
		RETURNING id, instance_id, kind, status, label, usage_count, last_used_at, created_at, updated_at`, status, connectionID, instanceID).Scan(
		&connection.ID, &connection.InstanceID, &connection.Kind, &connection.Status, &connection.Label, &connection.UsageCount, &connection.LastUsedAt, &connection.CreatedAt, &connection.UpdatedAt)
	if err != nil {
		return OpenClawConnection{}, err
	}
	connection.SetupURL = result.SetupURL
	connection.SetupKey = result.SetupKey
	connection.SetupMessage = result.Message
	return connection, nil
}

func (s *OpenClawService) ListConnectionKinds(ctx context.Context) ([]OpenClawConnectionKindInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT kind, title, description, sort_order
		FROM openclaw_connection_catalog
		WHERE enabled = TRUE AND kind <> $1
		ORDER BY sort_order ASC, kind ASC`, OpenClawConnectionGWS)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var kinds []OpenClawConnectionKindInfo
	for rows.Next() {
		var kind OpenClawConnectionKindInfo
		if err := rows.Scan(&kind.Kind, &kind.Title, &kind.Description, &kind.SortOrder); err != nil {
			return nil, err
		}
		kind.SetupAvailable = s.connectionSetupAvailable(kind.Kind)
		kinds = append(kinds, kind)
	}
	return kinds, rows.Err()
}

// connectionSetupAvailable describes the provider actions the UK02 worker
// actually exposes today. Telegram is implemented by the tenant bridge;
// WeChat QR and Google Workspace OAuth still require their own bridge flows.
func (s *OpenClawService) connectionSetupAvailable(kind string) bool {
	if s.fleet == nil || !s.fleet.Enabled() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case OpenClawConnectionTelegram, OpenClawConnectionWeChat, OpenClawConnectionGWS:
		return true
	default:
		return false
	}
}

func (s *OpenClawService) connectionKindAvailable(ctx context.Context, kind string) bool {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM openclaw_connection_catalog WHERE kind = $1 AND enabled = TRUE)`, kind).Scan(&exists); err != nil {
		return false
	}
	return exists
}

func (s *OpenClawService) GetDashboard(ctx context.Context, userID int64) (OpenClawDashboard, error) {
	instances, err := s.ListInstances(ctx, userID)
	if err != nil {
		return OpenClawDashboard{}, err
	}
	if len(instances) == 0 {
		return OpenClawDashboard{}, ErrOpenClawInstanceNotFound
	}
	instance := instances[0]
	dashboard := OpenClawDashboard{Instance: instance, FleetState: instance.Status, CheckedAt: time.Now(), GatewayEnabled: openClawGatewayEnabled(userID), EnrollmentEnabled: openClawEnrollmentEnabled(userID)}
	if s.apiKeys != nil && instance.DeletionScheduledAt == nil {
		apiKey, keyErr := s.ensureInstanceAPIKey(ctx, userID, instance.ID, instance.Name)
		if keyErr != nil {
			return OpenClawDashboard{}, fmt.Errorf("ensure openclaw billing key: %w", keyErr)
		}
		dashboard.APIKey = apiKey
	}
	var total, connected int
	var usage int64
	var lastUsed sql.NullTime
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'connected'), COALESCE(SUM(usage_count), 0), MAX(last_used_at)
		FROM openclaw_connections WHERE instance_id = $1`, instance.ID).Scan(&total, &connected, &usage, &lastUsed); err != nil {
		return OpenClawDashboard{}, err
	}
	dashboard.ConnectionCount = total
	dashboard.ConnectedCount = connected
	dashboard.UsageCount = usage
	if lastUsed.Valid {
		dashboard.LastUsedAt = &lastUsed.Time
	}
	if dashboard.APIKey == nil && s.apiKeys != nil {
		apiKey, keyErr := s.loadInstanceAPIKey(ctx, instance.ID)
		if keyErr != nil {
			return OpenClawDashboard{}, keyErr
		}
		dashboard.APIKey = apiKey
	}
	if s.fleet != nil && s.fleet.Enabled() {
		status, statusErr := s.fleet.Status(ctx, instance.TenantSlug)
		if statusErr != nil {
			// Health telemetry must not make the tenant console unusable while a
			// worker is restarting. Keep the last control-plane data visible and
			// label the bridge as unavailable until the next successful check.
			dashboard.FleetState = "unavailable"
			return dashboard, nil
		}
		dashboard.FleetState = status.State
		dashboard.LatencyMS = status.LatencyMS
		_, _ = s.db.ExecContext(ctx, `UPDATE openclaw_instances SET status = $1, last_latency_ms = $2, last_seen_at = NOW(), updated_at = NOW() WHERE id = $3 AND user_id = $4`, status.State, status.LatencyMS, instance.ID, userID)
	}
	return dashboard, nil
}

// GetGatewayLaunchURL issues the native OpenClaw browser handoff for the
// authenticated owner's cell. The URL is short-lived and is returned only to
// that owner; fleet credentials and container addresses never cross this API.
func (s *OpenClawService) GetGatewayLaunchURL(ctx context.Context, userID, instanceID int64) (string, error) {
	if !openClawGatewayEnabled(userID) {
		return "", ErrOpenClawProviderUnavailable
	}
	instance, err := s.GetInstance(ctx, userID, instanceID)
	if err != nil {
		return "", err
	}
	if s.fleet == nil || !s.fleet.Enabled() {
		return "", ErrOpenClawFleetDisabled
	}
	if instance.DeletionScheduledAt != nil {
		return "", ErrOpenClawDeletionScheduled
	}
	if instance.Status != "running" && instance.Status != "ready" {
		return "", ErrOpenClawProviderUnavailable
	}
	owner, err := s.gatewayTenantOwner(ctx, instance.TenantSlug)
	if err != nil || owner != userID {
		return "", ErrOpenClawProviderUnavailable
	}
	launcher, ok := s.fleet.(OpenClawGatewayLauncher)
	if !ok {
		return "", ErrOpenClawProviderUnavailable
	}
	result, err := launcher.Dashboard(ctx, instance.TenantSlug)
	if err != nil {
		return "", ErrOpenClawProviderUnavailable
	}
	if strings.TrimSpace(result.BrowserURL) == "" {
		return "", ErrOpenClawProviderUnavailable
	}
	return result.BrowserURL, nil
}

func openClawGatewayEnabled(userID int64) bool {
	if os.Getenv("OPENCLAW_GATEWAY_ENABLED") != "1" || userID <= 0 {
		return false
	}
	// Explicit post-canary rollout for current and future eligible workspace
	// owners. Entitlement and ownership remain checked at each service boundary.
	if os.Getenv("OPENCLAW_GATEWAY_ALL_ELIGIBLE") == "1" {
		return true
	}
	for _, id := range strings.Split(os.Getenv("OPENCLAW_GATEWAY_USER_IDS"), ",") {
		if strings.TrimSpace(id) == strconv.FormatInt(userID, 10) {
			return true
		}
	}
	return false
}

func (s *OpenClawService) ListAdminInstances(ctx context.Context) ([]OpenClawAdminInstance, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.tenant_slug, i.name, i.status, i.worker_host, i.port, COALESCE(i.last_error, ''),
		       i.created_at, i.updated_at, i.deletion_requested_at, i.deletion_scheduled_at,
		       i.user_id, u.email, i.last_latency_ms, i.last_seen_at,
		       COALESCE((SELECT g.name FROM openclaw_api_keys link JOIN api_keys k ON k.id = link.api_key_id AND k.deleted_at IS NULL LEFT JOIN groups g ON g.id = k.group_id WHERE link.instance_id = i.id LIMIT 1), ''),
		       COALESCE((SELECT g.rate_multiplier FROM openclaw_api_keys link JOIN api_keys k ON k.id = link.api_key_id AND k.deleted_at IS NULL JOIN groups g ON g.id = k.group_id WHERE link.instance_id = i.id LIMIT 1), 0),
		       COALESCE((SELECT k.status FROM openclaw_api_keys link JOIN api_keys k ON k.id = link.api_key_id AND k.deleted_at IS NULL WHERE link.instance_id = i.id LIMIT 1), ''),
		       COALESCE((SELECT SUM(ul.actual_cost) FROM openclaw_api_keys link JOIN usage_logs ul ON ul.api_key_id = link.api_key_id WHERE link.instance_id = i.id), 0),
		       COUNT(c.id), COUNT(c.id) FILTER (WHERE c.status = 'connected'),
		       COALESCE(SUM(c.usage_count), 0), MAX(c.last_used_at)
		FROM openclaw_instances i
		JOIN users u ON u.id = i.user_id
		LEFT JOIN openclaw_connections c ON c.instance_id = i.id
		GROUP BY i.id, u.email ORDER BY i.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var items []OpenClawAdminInstance
	for rows.Next() {
		var item OpenClawAdminInstance
		var port, latency sql.NullInt64
		var billingGroup, apiKeyStatus sql.NullString
		var billingRate, usageCost sql.NullFloat64
		var requestedAt, scheduledAt, lastSeen, lastUsed sql.NullTime
		if err := rows.Scan(&item.ID, &item.TenantSlug, &item.Name, &item.Status, &item.WorkerHost, &port, &item.LastError, &item.CreatedAt, &item.UpdatedAt, &requestedAt, &scheduledAt, &item.UserID, &item.UserEmail, &latency, &lastSeen, &billingGroup, &billingRate, &apiKeyStatus, &usageCost, &item.ConnectionCount, &item.ConnectedCount, &item.UsageCount, &lastUsed); err != nil {
			return nil, err
		}
		item.BillingGroup = billingGroup.String
		item.BillingRate = billingRate.Float64
		item.APIKeyStatus = apiKeyStatus.String
		item.UsageCost = usageCost.Float64
		item.AllowedModels = append([]string(nil), openClawAllowedModels...)
		item.DefaultModel = "gpt-5.6-sol"
		_, item.MappingEnabled, _ = configuredOpenClawModelMapping(strings.Repeat("x", 32))
		if port.Valid {
			value := int(port.Int64)
			item.Port = &value
		}
		if latency.Valid {
			value := int(latency.Int64)
			item.LatencyMS = &value
		}
		if lastSeen.Valid {
			item.LastSeenAt = &lastSeen.Time
		}
		if requestedAt.Valid {
			item.DeletionRequestedAt = &requestedAt.Time
		}
		if scheduledAt.Valid {
			item.DeletionScheduledAt = &scheduledAt.Time
		}
		if lastUsed.Valid {
			item.LastUsedAt = &lastUsed.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *OpenClawService) DeleteInstanceAdmin(ctx context.Context, instanceID int64) (OpenClawInstance, error) {
	var userID int64
	if err := s.db.QueryRowContext(ctx, `SELECT user_id FROM openclaw_instances WHERE id = $1`, instanceID).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OpenClawInstance{}, ErrOpenClawInstanceNotFound
		}
		return OpenClawInstance{}, err
	}
	return s.deleteInstance(ctx, userID, instanceID, true)
}

// ReconcileInstanceAdmin repairs the managed OpenClaw key/group association for a
// workspace without exposing its credential or changing its lifecycle state.
func (s *OpenClawService) ReconcileInstanceAdmin(ctx context.Context, instanceID int64) (OpenClawInstance, error) {
	var userID int64
	if err := s.db.QueryRowContext(ctx, `SELECT user_id FROM openclaw_instances WHERE id = $1`, instanceID).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OpenClawInstance{}, ErrOpenClawInstanceNotFound
		}
		return OpenClawInstance{}, err
	}
	instance, err := s.GetInstance(ctx, userID, instanceID)
	if err != nil {
		return OpenClawInstance{}, err
	}
	if instance.DeletionScheduledAt != nil {
		return OpenClawInstance{}, ErrOpenClawDeletionScheduled
	}
	if s.apiKeys != nil {
		billingKey, err := s.ensureInstanceAPIKey(ctx, userID, instanceID, instance.Name)
		if err != nil {
			return OpenClawInstance{}, err
		}
		if billingKey != nil && s.fleet != nil && s.fleet.Enabled() {
			fullKey, keyErr := s.apiKeys.GetByID(ctx, billingKey.ID)
			if keyErr != nil {
				return OpenClawInstance{}, keyErr
			}
			mapping, enabled, mappingErr := configuredOpenClawModelMapping(fullKey.Key)
			if mappingErr != nil {
				return OpenClawInstance{}, mappingErr
			}
			if enabled {
				configurator, ok := s.fleet.(OpenClawFleetConfigurator)
				if !ok {
					return OpenClawInstance{}, errors.New("OpenClaw worker does not support model mapping")
				}
				if err := configurator.Configure(ctx, instance.TenantSlug, mapping); err != nil {
					return OpenClawInstance{}, err
				}
			}
		}
	}
	return instance, nil
}

func (s *OpenClawService) DeleteConnection(ctx context.Context, userID, instanceID, connectionID int64) error {
	if _, err := s.GetInstance(ctx, userID, instanceID); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM openclaw_connections WHERE id = $1 AND instance_id = $2`, connectionID, instanceID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrOpenClawConnectionNotFound
	}
	return nil
}

type openClawScanner interface {
	Scan(...any) error
}

func scanOpenClawInstance(scanner openClawScanner) (OpenClawInstance, error) {
	var instance OpenClawInstance
	var port sql.NullInt64
	var requestedAt, scheduledAt sql.NullTime
	if err := scanner.Scan(&instance.ID, &instance.TenantSlug, &instance.Name, &instance.Status, &instance.WorkerHost, &port, &instance.LastError, &instance.CreatedAt, &instance.UpdatedAt, &requestedAt, &scheduledAt); err != nil {
		return OpenClawInstance{}, err
	}
	if port.Valid {
		value := int(port.Int64)
		instance.Port = &value
	}
	if requestedAt.Valid {
		instance.DeletionRequestedAt = &requestedAt.Time
	}
	if scheduledAt.Valid {
		instance.DeletionScheduledAt = &scheduledAt.Time
	}
	return instance, nil
}

func newOpenClawTenantSlug(userID int64) (string, error) {
	if userID <= 0 {
		return "", errors.New("invalid user id")
	}
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "u" + strconv.FormatInt(userID, 10) + "-" + hex.EncodeToString(buf), nil
}

func validOpenClawTenantSlug(slug string) bool {
	if len(slug) < 4 || len(slug) > 40 || slug[0] != 'u' {
		return false
	}
	separator := strings.IndexByte(slug, '-')
	if separator <= 1 || separator == len(slug)-1 {
		return false
	}
	for _, char := range slug[1:] {
		if (char < 'a' || char > 'f') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return len(slug)-separator-1 == 12
}

func validOpenClawConnectionKind(kind string) bool {
	switch kind {
	case OpenClawConnectionWeChat, OpenClawConnectionTelegram, OpenClawConnectionGWS:
		return true
	default:
		return false
	}
}
