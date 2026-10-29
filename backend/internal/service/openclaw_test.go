package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestOpenClawGatewayOwnerAllowlist(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_ALL_ELIGIBLE", "")
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "")
	t.Setenv("OPENCLAW_GATEWAY_USER_IDS", "1, 42")
	if openClawGatewayEnabled(42) {
		t.Fatal("enabled without feature flag")
	}
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "1")
	if !openClawGatewayEnabled(42) || openClawGatewayEnabled(4) || openClawGatewayEnabled(0) {
		t.Fatal("owner allowlist did not match exact identity")
	}
	t.Setenv("OPENCLAW_GATEWAY_USER_IDS", "*")
	if openClawGatewayEnabled(42) {
		t.Fatal("wildcard enabled an untested owner")
	}
}

func TestOpenClawGatewayEligibleRolloutStillRequiresMasterFlag(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_USER_IDS", "")
	t.Setenv("OPENCLAW_GATEWAY_ALL_ELIGIBLE", "1")
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "0")
	if openClawGatewayEnabled(42) {
		t.Fatal("rollout ignored master disable")
	}
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "1")
	if !openClawGatewayEnabled(42) || openClawGatewayEnabled(0) || openClawGatewayEnabled(-1) {
		t.Fatal("invalid eligible rollout identity")
	}
	t.Setenv("OPENCLAW_GATEWAY_ALL_ELIGIBLE", "true")
	if openClawGatewayEnabled(42) {
		t.Fatal("ambiguous rollout setting enabled access")
	}
}

func TestGatewayLaunchCannotResolveAnotherOwnersCell(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "1")
	t.Setenv("OPENCLAW_GATEWAY_USER_IDS", "42")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`FROM openclaw_instances WHERE id = \$1 AND user_id = \$2`).WithArgs(int64(7), int64(42)).WillReturnError(sql.ErrNoRows)
	s := NewOpenClawService(db, nil)
	if _, err := s.GetGatewayLaunchURL(context.Background(), 42, 7); !errors.Is(err, ErrOpenClawInstanceNotFound) {
		t.Fatalf("wrong-owner lookup = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type openClawGroupRepoStub struct {
	GroupRepository
	active  []Group
	byName  *Group
	byErr   error
	created *Group
	updated *Group
}

func (s *openClawGroupRepoStub) ListActive(context.Context) ([]Group, error) {
	return append([]Group(nil), s.active...), nil
}

func (s *openClawGroupRepoStub) GetByName(context.Context, string) (*Group, error) {
	if s.byErr != nil {
		return nil, s.byErr
	}
	if s.byName == nil {
		return nil, ErrGroupNotFound
	}
	clone := *s.byName
	return &clone, nil
}

func (s *openClawGroupRepoStub) Create(_ context.Context, group *Group) error {
	group.ID = 41
	s.created = group
	return nil
}

func (s *openClawGroupRepoStub) Update(_ context.Context, group *Group) error {
	clone := *group
	s.updated = &clone
	s.byName = &clone
	return nil
}

func TestEnsureOpenClawGroupCreatesCanonicalPublicGroup(t *testing.T) {
	repo := &openClawGroupRepoStub{}
	apiKeys := &APIKeyService{groupRepo: repo}

	group, err := apiKeys.EnsureOpenClawGroup(context.Background())
	if err != nil {
		t.Fatalf("EnsureOpenClawGroup() error = %v", err)
	}
	if group.ID != 41 {
		t.Fatalf("group ID = %d, want 41", group.ID)
	}
	if repo.created == nil {
		t.Fatal("EnsureOpenClawGroup() did not create the group")
	}
	if repo.created.Name != OpenClawBillingGroupName || repo.created.Platform != PlatformOpenAI {
		t.Fatalf("created group identity = %#v", repo.created)
	}
	if repo.created.RateMultiplier != OpenClawBillingRateMultiplier {
		t.Fatalf("created rate multiplier = %v, want %v", repo.created.RateMultiplier, OpenClawBillingRateMultiplier)
	}
	if repo.created.IsExclusive || repo.created.Status != StatusActive || repo.created.SubscriptionType != SubscriptionTypeStandard {
		t.Fatalf("created group is not public and active: %#v", repo.created)
	}
}

func TestEnsureOpenClawGroupRepairsExistingManagedGroup(t *testing.T) {
	repo := &openClawGroupRepoStub{byName: &Group{
		ID:               52,
		Name:             OpenClawBillingGroupName,
		Description:      "old",
		Platform:         PlatformAnthropic,
		RateMultiplier:   1,
		IsExclusive:      true,
		Status:           StatusDisabled,
		SubscriptionType: SubscriptionTypeSubscription,
	}}
	apiKeys := &APIKeyService{groupRepo: repo}

	group, err := apiKeys.EnsureOpenClawGroup(context.Background())
	if err != nil {
		t.Fatalf("EnsureOpenClawGroup() error = %v", err)
	}
	if repo.updated == nil {
		t.Fatal("EnsureOpenClawGroup() did not repair the existing group")
	}
	if group.ID != 52 || group.Platform != PlatformOpenAI || group.RateMultiplier != OpenClawBillingRateMultiplier || group.IsExclusive || group.Status != StatusActive || group.SubscriptionType != SubscriptionTypeStandard {
		t.Fatalf("repaired group = %#v", group)
	}
	for range 2 {
		repo.updated = nil
		again, err := apiKeys.EnsureOpenClawGroup(context.Background())
		if err != nil || again.ID != 52 || again.Name != "openai-龙虾" || again.RateMultiplier != 0.3 || repo.created != nil || repo.updated != nil {
			t.Fatalf("repeat repair changed the canonical group: group=%#v err=%v", again, err)
		}
	}
}

func TestConfiguredOpenClawModelMappingDefaultsToApprovedModels(t *testing.T) {
	t.Setenv("OPENCLAW_MODEL_GATEWAY_BASE_URL", "https://gateway.example/v1")
	t.Setenv("OPENCLAW_MODEL_MAPPING_JSON", "")

	mapping, enabled, err := configuredOpenClawModelMapping("sk-012345678901234567890")
	if err != nil {
		t.Fatalf("configuredOpenClawModelMapping() error = %v", err)
	}
	if !enabled || mapping.DefaultModel != "gpt-5.6-sol" {
		t.Fatalf("mapping = %#v, enabled = %v", mapping, enabled)
	}
	if len(mapping.Models) != len(openClawAllowedModels) {
		t.Fatalf("model count = %d, want %d", len(mapping.Models), len(openClawAllowedModels))
	}
}

func TestConfiguredOpenClawModelMappingRejectsAstra(t *testing.T) {
	t.Setenv("OPENCLAW_MODEL_GATEWAY_BASE_URL", "https://gateway.example/v1")
	models := make([]OpenClawModelAlias, 0, len(openClawAllowedModels))
	for _, model := range openClawAllowedModels {
		models = append(models, OpenClawModelAlias{Alias: model, Model: model})
	}
	models[len(models)-1].Model = "gpt-6-astra"
	raw, err := json.Marshal(struct {
		DefaultModel string               `json:"default_model"`
		Models       []OpenClawModelAlias `json:"models"`
	}{DefaultModel: "gpt-5.6-sol", Models: models})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCLAW_MODEL_MAPPING_JSON", string(raw))

	_, _, err = configuredOpenClawModelMapping("sk-012345678901234567890")
	if err == nil {
		t.Fatal("configuredOpenClawModelMapping() accepted gpt-6-astra")
	}
}

func TestValidateAPIKeyGroupChangeLocksLinkedWorkspaceKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`SELECT i\.id\s+FROM openclaw_api_keys`).
		WithArgs(int64(7), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(99)))
	repo := &openClawGroupRepoStub{byName: &Group{ID: 41, Name: OpenClawBillingGroupName, Platform: PlatformOpenAI, RateMultiplier: 0.4, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard}}
	apiKeys := &APIKeyService{groupRepo: repo}
	service := NewOpenClawService(db, nil)
	service.SetAPIKeyService(apiKeys)

	if err := service.ValidateAPIKeyGroupChange(context.Background(), 7, 42, 41); err != nil {
		t.Fatalf("allowed CLAW group change returned error: %v", err)
	}
	mock.ExpectQuery(`SELECT i\.id\s+FROM openclaw_api_keys`).
		WithArgs(int64(7), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(99)))
	if err := service.ValidateAPIKeyGroupChange(context.Background(), 7, 42, 99); !errors.Is(err, ErrOpenClawGroupImmutable) {
		t.Fatalf("wrong group change error = %v, want ErrOpenClawGroupImmutable", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNewOpenClawTenantSlugIsUserScopedAndSafe(t *testing.T) {
	slug, err := newOpenClawTenantSlug(42)
	if err != nil {
		t.Fatalf("newOpenClawTenantSlug() error = %v", err)
	}
	if len(slug) < 9 || slug[:3] != "u42" {
		t.Fatalf("slug = %q, want user-scoped prefix", slug)
	}
	for _, char := range slug {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			t.Fatalf("slug contains unsafe character %q: %q", char, slug)
		}
	}
}

func TestValidOpenClawConnectionKind(t *testing.T) {
	for _, kind := range []string{OpenClawConnectionWeChat, OpenClawConnectionTelegram, OpenClawConnectionGWS} {
		if !validOpenClawConnectionKind(kind) {
			t.Errorf("validOpenClawConnectionKind(%q) = false", kind)
		}
	}
	for _, kind := range []string{"gmail", "shell", "", "WECHAT"} {
		if validOpenClawConnectionKind(kind) {
			t.Errorf("validOpenClawConnectionKind(%q) = true", kind)
		}
	}
}

func TestListConnectionKindsOmitsGoogleWorkspaceFromDashboardCatalog(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`FROM openclaw_connection_catalog\s+WHERE enabled = TRUE AND kind <> \$1\s+ORDER BY sort_order ASC, kind ASC`).
		WithArgs(OpenClawConnectionGWS).
		WillReturnRows(sqlmock.NewRows([]string{"kind", "title", "description", "sort_order"}).
			AddRow(OpenClawConnectionWeChat, "WeChat", "WeChat description", 10).
			AddRow(OpenClawConnectionTelegram, "Telegram", "Telegram description", 20))

	service := NewOpenClawService(db, nil)
	kinds, err := service.ListConnectionKinds(context.Background())
	if err != nil {
		t.Fatalf("ListConnectionKinds() error = %v", err)
	}
	if len(kinds) != 2 {
		t.Fatalf("ListConnectionKinds() returned %d kinds, want 2", len(kinds))
	}
	for _, kind := range kinds {
		if kind.Kind == OpenClawConnectionGWS {
			t.Fatal("ListConnectionKinds() returned Google Workspace")
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestValidOpenClawTenantSlugRejectsPathTraversal(t *testing.T) {
	for _, slug := range []string{"../etc", "u1-abc", "u1-0123456789ab/stop", "u1-0123456789ab;rm"} {
		if validOpenClawTenantSlug(slug) {
			t.Errorf("validOpenClawTenantSlug(%q) = true", slug)
		}
	}
	if !validOpenClawTenantSlug("u42-0123456789ab") {
		t.Fatal("validOpenClawTenantSlug rejected a generated-style slug")
	}
}

func TestCommandOpenClawFleetClientDisabledByDefault(t *testing.T) {
	t.Setenv("OPENCLAW_FLEET_ENABLED", "0")
	client := NewCommandOpenClawFleetClient()
	if client.Enabled() {
		t.Fatal("fleet client unexpectedly enabled")
	}
	_, err := client.Create(context.Background(), "u1-safe")
	if !errors.Is(err, ErrOpenClawFleetDisabled) {
		t.Fatalf("Create() error = %v, want ErrOpenClawFleetDisabled", err)
	}
}

func TestGetOpenClawInstanceAlwaysScopesByUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = db.Close() }()
	now := time.Now()
	mock.ExpectQuery(`FROM openclaw_instances WHERE id = \$1 AND user_id = \$2`).
		WithArgs(int64(7), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_slug", "name", "status", "worker_host", "port", "last_error", "created_at", "updated_at", "deletion_requested_at", "deletion_scheduled_at"}).
			AddRow(int64(7), "u42-0123456789ab", "private", "planned", "default", nil, "", now, now, nil, nil))

	service := NewOpenClawService(db, nil)
	instance, err := service.GetInstance(context.Background(), 42, 7)
	if err != nil {
		t.Fatalf("GetInstance() error = %v", err)
	}
	if instance.TenantSlug != "u42-0123456789ab" {
		t.Fatalf("TenantSlug = %q", instance.TenantSlug)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
