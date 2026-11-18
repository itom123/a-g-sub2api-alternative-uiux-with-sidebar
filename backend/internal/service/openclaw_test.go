package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"slices"
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
	defer func() { _ = db.Close() }()
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
	wantModels := []string{"gpt-5.6", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-6.1-sol", "gpt-6-sol", "gpt-6-luna", "gpt-6.2-sol", "gpt-6.2-astra"}
	if !repo.created.ModelAllowlist.Enabled || !slices.Equal(repo.created.ModelAllowlist.Models, wantModels) {
		t.Fatalf("created model allowlist = %#v", repo.created.ModelAllowlist)
	}
	wantMappings := []ReasoningEffortMapping{
		{From: "none", To: "low", MatchType: "exact", Model: "gpt-6.1-sol"},
		{From: "minimal", To: "low", MatchType: "exact", Model: "gpt-6.1-sol"},
		{From: "none", To: "low", MatchType: "exact", Model: "gpt-6.2-sol"},
		{From: "minimal", To: "low", MatchType: "exact", Model: "gpt-6.2-sol"},
		{From: "none", To: "low", MatchType: "exact", Model: "gpt-6.2-astra"},
		{From: "minimal", To: "low", MatchType: "exact", Model: "gpt-6.2-astra"},
	}
	if !slices.Equal(repo.created.ReasoningEffortMappings, wantMappings) {
		t.Fatalf("created reasoning mappings = %#v", repo.created.ReasoningEffortMappings)
	}
	// The create call already carries the defaults; no second write follows.
	if repo.updated != nil && (!repo.updated.ModelAllowlist.Enabled || !slices.Equal(repo.updated.ReasoningEffortMappings, wantMappings)) {
		t.Fatalf("follow-up repair dropped the defaults: %#v", repo.updated)
	}
}

func TestEnsureOpenClawGroupKeepsAdministratorModelPolicy(t *testing.T) {
	custom := []ReasoningEffortMapping{{From: "none", To: "medium", MatchType: "exact", Model: "gpt-6-sol"}}
	for name, tc := range map[string]struct {
		allowlist GroupModelAllowlist
		mappings  []ReasoningEffortMapping
	}{
		"allowlist disabled, no mappings": {GroupModelAllowlist{Enabled: false}, nil},
		"empty mappings slice":            {GroupModelAllowlist{Enabled: false, Models: []string{}}, []ReasoningEffortMapping{}},
		"custom allowlist and mappings":   {GroupModelAllowlist{Enabled: true, Models: []string{"gpt-6-sol"}}, custom},
	} {
		t.Run(name, func(t *testing.T) {
			repo := &openClawGroupRepoStub{byName: &Group{
				ID: 52, Name: OpenClawBillingGroupName, Description: OpenClawBillingGroupDescription,
				Platform: PlatformOpenAI, RateMultiplier: 0.15, Status: StatusActive,
				SubscriptionType: SubscriptionTypeStandard, MaxReasoningEffort: "max",
				MaxReasoningEffortOverLimit: ReasoningEffortOverLimitDowngrade,
				ModelAllowlist:              tc.allowlist, ReasoningEffortMappings: tc.mappings,
			}}
			apiKeys := &APIKeyService{groupRepo: repo}
			group, err := apiKeys.EnsureOpenClawGroup(context.Background())
			if err != nil {
				t.Fatalf("EnsureOpenClawGroup() error = %v", err)
			}
			if repo.updated != nil || repo.created != nil {
				t.Fatalf("existing group was written: created=%#v updated=%#v", repo.created, repo.updated)
			}
			if group.ModelAllowlist.Enabled != tc.allowlist.Enabled || !slices.Equal(group.ModelAllowlist.Models, tc.allowlist.Models) {
				t.Fatalf("model allowlist = %#v, want %#v", group.ModelAllowlist, tc.allowlist)
			}
			if !slices.Equal(group.ReasoningEffortMappings, tc.mappings) || group.RateMultiplier != 0.15 {
				t.Fatalf("group = %#v", group)
			}
		})
	}
}

func TestEnsureOpenClawGroupRepairKeepsAdministratorModelPolicy(t *testing.T) {
	// Another field needs repair: the write must still carry the owner's policy.
	repo := &openClawGroupRepoStub{byName: &Group{
		ID: 52, Name: OpenClawBillingGroupName, Description: "old", Platform: PlatformOpenAI,
		RateMultiplier: 0.15, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard,
		ModelAllowlist: GroupModelAllowlist{Enabled: false},
	}}
	apiKeys := &APIKeyService{groupRepo: repo}
	if _, err := apiKeys.EnsureOpenClawGroup(context.Background()); err != nil {
		t.Fatalf("EnsureOpenClawGroup() error = %v", err)
	}
	if repo.updated == nil || repo.updated.Description != OpenClawBillingGroupDescription {
		t.Fatalf("description was not repaired: %#v", repo.updated)
	}
	if repo.updated.ModelAllowlist.Enabled || len(repo.updated.ModelAllowlist.Models) != 0 || len(repo.updated.ReasoningEffortMappings) != 0 {
		t.Fatalf("repair reset the administrator's model policy: %#v", repo.updated)
	}
}

func TestEnsureOpenClawGroupRepairsExistingManagedGroup(t *testing.T) {
	repo := &openClawGroupRepoStub{byName: &Group{
		ID:               52,
		Name:             OpenClawBillingGroupName,
		Description:      "old",
		Platform:         PlatformAnthropic,
		RateMultiplier:   0.225,
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
	if group.ID != 52 || group.Platform != PlatformOpenAI || group.RateMultiplier != 0.225 || group.IsExclusive || group.Status != StatusActive || group.SubscriptionType != SubscriptionTypeStandard {
		t.Fatalf("repaired group = %#v", group)
	}
	for range 2 {
		repo.updated = nil
		again, err := apiKeys.EnsureOpenClawGroup(context.Background())
		if err != nil || again.ID != 52 || again.Name != "openai-龙虾" || again.RateMultiplier != 0.225 || repo.created != nil || repo.updated != nil {
			t.Fatalf("repeat repair changed the canonical group: group=%#v err=%v", again, err)
		}
	}
}

func TestEnsureOpenClawGroupRepairsInvalidRate(t *testing.T) {
	for name, rate := range map[string]float64{"zero": 0, "negative": -0.1, "nan": math.NaN(), "infinity": math.Inf(1)} {
		t.Run(name, func(t *testing.T) {
			repo := &openClawGroupRepoStub{byName: &Group{
				ID: 52, Name: OpenClawBillingGroupName, Platform: PlatformOpenAI,
				RateMultiplier: rate, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard,
			}}
			apiKeys := &APIKeyService{groupRepo: repo}
			group, err := apiKeys.EnsureOpenClawGroup(context.Background())
			if err != nil || group.RateMultiplier != OpenClawBillingRateMultiplier || repo.updated == nil {
				t.Fatalf("invalid rate was not repaired: group=%#v err=%v", group, err)
			}
		})
	}
}

func TestConfiguredOpenClawModelMappingDefaultsToApprovedModels(t *testing.T) {
	t.Setenv("OPENCLAW_MODEL_GATEWAY_BASE_URL", "https://gateway.example/v1")
	t.Setenv("OPENCLAW_MODEL_MAPPING_JSON", "")

	for name, group := range map[string]*Group{
		"no group":          nil,
		"allowlist off":     {ModelAllowlist: GroupModelAllowlist{Enabled: false, Models: []string{"gpt-6-sol"}}},
		"allowlist missing": {},
	} {
		t.Run(name, func(t *testing.T) {
			mapping, enabled, err := configuredOpenClawModelMapping("sk-012345678901234567890", group)
			if err != nil {
				t.Fatalf("configuredOpenClawModelMapping() error = %v", err)
			}
			if !enabled || mapping.DefaultModel != "gpt-5.6-sol" {
				t.Fatalf("mapping = %#v, enabled = %v", mapping, enabled)
			}
			if got := openClawMappingAliases(mapping); !slices.Equal(got, openClawAllowedModels) {
				t.Fatalf("aliases = %v, want %v", got, openClawAllowedModels)
			}
		})
	}
}

func openClawMappingAliases(mapping OpenClawModelMapping) []string {
	aliases := make([]string, 0, len(mapping.Models))
	for _, model := range mapping.Models {
		aliases = append(aliases, model.Alias)
	}
	return aliases
}

// The administrator owns the group's allowlist; the cell must offer exactly
// what the gateway admits for the workspace key, without a code change.
func TestConfiguredOpenClawModelMappingFollowsGroupAllowlist(t *testing.T) {
	t.Setenv("OPENCLAW_MODEL_GATEWAY_BASE_URL", "https://gateway.example/v1")
	t.Setenv("OPENCLAW_MODEL_MAPPING_JSON", "")
	group := &Group{
		DefaultMappedModel: "openai/GPT-6.2-Sol",
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{
			"gpt-6.2-sol", " gpt-5.6-sol ", "GPT-5.6-SOL", "gpt-7-preview", "gpt-6*",
		}},
	}
	mapping, enabled, err := configuredOpenClawModelMapping("sk-012345678901234567890", group)
	if err != nil || !enabled {
		t.Fatalf("mapping enabled=%v err=%v", enabled, err)
	}
	// Exact entries keep the administrator's order; the wildcard expands to the
	// known models it matches, without repeating one.
	want := []string{"gpt-6.2-sol", "gpt-5.6-sol", "gpt-7-preview", "gpt-6.1-sol", "gpt-6-sol", "gpt-6-luna", "gpt-6.2-astra"}
	if got := openClawMappingAliases(mapping); !slices.Equal(got, want) {
		t.Fatalf("aliases = %v, want %v", got, want)
	}
	if mapping.DefaultModel != "gpt-6.2-sol" {
		t.Fatalf("default = %q, want the group's default_mapped_model", mapping.DefaultModel)
	}

	// A group default the allowlist excludes falls back to gpt-5.6-sol, then
	// to the first allowed model.
	group.DefaultMappedModel = "gpt-4o"
	if mapping, _, _ = configuredOpenClawModelMapping("sk-012345678901234567890", group); mapping.DefaultModel != "gpt-5.6-sol" {
		t.Fatalf("default = %q, want gpt-5.6-sol", mapping.DefaultModel)
	}
	group.ModelAllowlist.Models = []string{"gpt-6-luna"}
	if mapping, _, _ = configuredOpenClawModelMapping("sk-012345678901234567890", group); mapping.DefaultModel != "gpt-6-luna" {
		t.Fatalf("default = %q, want gpt-6-luna", mapping.DefaultModel)
	}

	// An allowlist that names no model a cell can offer must not block
	// workspace creation: the built-in list is offered, and reported as such.
	group.ModelAllowlist.Models = []string{"claude-*"}
	mapping, enabled, err = configuredOpenClawModelMapping("sk-012345678901234567890", group)
	if err != nil || !enabled || !slices.Equal(openClawMappingAliases(mapping), openClawAllowedModels) {
		t.Fatalf("fallback mapping = %#v enabled=%v err=%v", mapping, enabled, err)
	}
	if _, _, source := openClawSupportedModels(group); source != OpenClawModelsFallback {
		t.Fatalf("source = %q, want fallback", source)
	}
}

// OPENCLAW_MODEL_MAPPING_JSON only renames models the group allows. Entries
// for other models are ignored rather than offered, and a model the JSON does
// not mention is still offered, so editing the group never breaks a workspace.
func TestConfiguredOpenClawModelMappingJSONOnlyRenamesAllowedModels(t *testing.T) {
	t.Setenv("OPENCLAW_MODEL_GATEWAY_BASE_URL", "https://gateway.example/v1")
	raw, err := json.Marshal(map[string]any{
		"default_model": "gpt-5.6",
		"models": []OpenClawModelAlias{
			{Alias: "gpt-5.6", Model: "gpt-5.6-sol"},
			{Alias: "gpt-6-sol", Model: "gpt-6-astra"},
			{Alias: "gpt-5.6", Model: "gpt-6-sol"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCLAW_MODEL_MAPPING_JSON", string(raw))
	group := &Group{ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.6", "gpt-5.6-sol", "gpt-6-sol"}}}

	mapping, enabled, err := configuredOpenClawModelMapping("sk-012345678901234567890", group)
	if err != nil || !enabled {
		t.Fatalf("mapping enabled=%v err=%v", enabled, err)
	}
	want := []OpenClawModelAlias{{Alias: "gpt-5.6", Model: "gpt-5.6-sol"}, {Alias: "gpt-6-sol", Model: "gpt-6-sol"}}
	if !slices.Equal(mapping.Models, want) || mapping.DefaultModel != "gpt-5.6" {
		t.Fatalf("mapping = %#v", mapping)
	}
	for _, model := range mapping.Models {
		if model.Model == "gpt-6-astra" {
			t.Fatal("mapping offered gpt-6-astra, which the group does not allow")
		}
	}

	t.Setenv("OPENCLAW_MODEL_MAPPING_JSON", "{")
	if _, _, err := configuredOpenClawModelMapping("sk-012345678901234567890", group); err == nil {
		t.Fatal("malformed OPENCLAW_MODEL_MAPPING_JSON was accepted")
	}
}

func TestConfiguredOpenClawModelMappingDisabledWithoutGateway(t *testing.T) {
	t.Setenv("OPENCLAW_MODEL_GATEWAY_BASE_URL", "")
	t.Setenv("OPENCLAW_MODEL_MAPPING_JSON", "")
	if _, enabled, err := configuredOpenClawModelMapping("sk-012345678901234567890", nil); enabled || err != nil {
		t.Fatalf("enabled=%v err=%v", enabled, err)
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
