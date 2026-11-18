package service

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

type modelSyncFleetStub struct {
	OpenClawFleetClient
	mu         sync.Mutex
	configured map[string][]OpenClawModelMapping
	fail       map[string]error
}

func (f *modelSyncFleetStub) Enabled() bool { return true }

func (f *modelSyncFleetStub) Configure(_ context.Context, tenant string, mapping OpenClawModelMapping) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.configured == nil {
		f.configured = make(map[string][]OpenClawModelMapping)
	}
	f.configured[tenant] = append(f.configured[tenant], mapping)
	return f.fail[tenant]
}

func (f *modelSyncFleetStub) writes(tenant string) []OpenClawModelMapping {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]OpenClawModelMapping(nil), f.configured[tenant]...)
}

type modelSyncKeyRepoStub struct {
	APIKeyRepository
	keys map[int64]string
}

func (r *modelSyncKeyRepoStub) GetByID(_ context.Context, id int64) (*APIKey, error) {
	key, ok := r.keys[id]
	if !ok {
		return nil, ErrAPIKeyNotFound
	}
	return &APIKey{ID: id, Key: key}, nil
}

const liveCellsQuery = `SELECT i\.tenant_slug, link\.api_key_id\s+FROM openclaw_instances i`

func expectLiveCells(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(liveCellsQuery).WillReturnRows(sqlmock.NewRows([]string{"tenant_slug", "api_key_id"}).
		AddRow("u1-aaaaaaaaaaaa", int64(10)).
		AddRow("u2-bbbbbbbbbbbb", int64(11)))
}

// A model the administrator adds to the billing group reaches every live
// cell on the next pass; an unchanged group writes nothing.
func TestSyncModelMappingsPushesGroupChangesToLiveCells(t *testing.T) {
	t.Setenv("OPENCLAW_MODEL_GATEWAY_BASE_URL", "https://gateway.example/v1")
	t.Setenv("OPENCLAW_MODEL_MAPPING_JSON", "")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	group := &Group{
		ID: 52, Name: OpenClawBillingGroupName, Description: OpenClawBillingGroupDescription,
		Platform: PlatformOpenAI, RateMultiplier: 0.3, Status: StatusActive,
		SubscriptionType: SubscriptionTypeStandard, MaxReasoningEffort: "max",
		MaxReasoningEffortOverLimit: ReasoningEffortOverLimitDowngrade,
		ModelAllowlist:              GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.6-sol", "gpt-6-sol"}},
	}
	groups := &openClawGroupRepoStub{byName: group}
	fleet := &modelSyncFleetStub{}
	service := NewOpenClawService(db, fleet)
	service.SetAPIKeyService(&APIKeyService{groupRepo: groups, apiKeyRepo: &modelSyncKeyRepoStub{keys: map[int64]string{
		10: "sk-first-workspace-key-0000", 11: "sk-second-workspace-key-000",
	}}})
	ctx := context.Background()

	expectLiveCells(mock)
	if err := service.SyncModelMappings(ctx); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	first := fleet.writes("u1-aaaaaaaaaaaa")
	if len(first) != 1 || len(fleet.writes("u2-bbbbbbbbbbbb")) != 1 {
		t.Fatalf("first sync writes = %d/%d, want 1/1", len(first), len(fleet.writes("u2-bbbbbbbbbbbb")))
	}
	if got := openClawMappingAliases(first[0]); !slices.Equal(got, []string{"gpt-5.6-sol", "gpt-6-sol"}) || first[0].APIKey != "sk-first-workspace-key-0000" {
		t.Fatalf("first mapping = %#v", first[0])
	}

	expectLiveCells(mock)
	if err := service.SyncModelMappings(ctx); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(fleet.writes("u1-aaaaaaaaaaaa")) != 1 || len(fleet.writes("u2-bbbbbbbbbbbb")) != 1 {
		t.Fatal("an unchanged group was written again")
	}

	group.ModelAllowlist.Models = append(group.ModelAllowlist.Models, "gpt-6.2-sol")
	expectLiveCells(mock)
	if err := service.SyncModelMappings(ctx); err != nil {
		t.Fatalf("third sync: %v", err)
	}
	for _, tenant := range []string{"u1-aaaaaaaaaaaa", "u2-bbbbbbbbbbbb"} {
		writes := fleet.writes(tenant)
		if len(writes) != 2 || !slices.Contains(openClawMappingAliases(writes[1]), "gpt-6.2-sol") {
			t.Fatalf("%s writes = %#v, want the added model", tenant, writes)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A cell that rejects the write is reported as failed and retried later, not
// on every pass; the other cells are still written.
func TestSyncModelMappingsReportsAndSpacesOutFailures(t *testing.T) {
	t.Setenv("OPENCLAW_MODEL_GATEWAY_BASE_URL", "https://gateway.example/v1")
	t.Setenv("OPENCLAW_MODEL_MAPPING_JSON", "")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	group := &Group{ID: 52, Name: OpenClawBillingGroupName, Description: OpenClawBillingGroupDescription,
		Platform: PlatformOpenAI, RateMultiplier: 0.3, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard,
		MaxReasoningEffort: "max", MaxReasoningEffortOverLimit: ReasoningEffortOverLimitDowngrade}
	fleet := &modelSyncFleetStub{fail: map[string]error{"u1-aaaaaaaaaaaa": errors.New("cell busy")}}
	service := NewOpenClawService(db, fleet)
	service.SetAPIKeyService(&APIKeyService{groupRepo: &openClawGroupRepoStub{byName: group}, apiKeyRepo: &modelSyncKeyRepoStub{keys: map[int64]string{
		10: "sk-first-workspace-key-0000", 11: "sk-second-workspace-key-000",
	}}})

	expectLiveCells(mock)
	if err := service.SyncModelMappings(context.Background()); err == nil {
		t.Fatal("a failed cell write was not reported")
	}
	expectLiveCells(mock)
	_ = service.SyncModelMappings(context.Background())
	if len(fleet.writes("u1-aaaaaaaaaaaa")) != 1 || len(fleet.writes("u2-bbbbbbbbbbbb")) != 1 {
		t.Fatalf("writes = %d/%d, want one each", len(fleet.writes("u1-aaaaaaaaaaaa")), len(fleet.writes("u2-bbbbbbbbbbbb")))
	}

	desired, _, _ := configuredOpenClawModelMapping("sk-placeholder-000000000000", group)
	if state, at := service.modelSync.state("u1-aaaaaaaaaaaa", openClawMappingFingerprint(desired, 10)); state != OpenClawModelSyncFailed || at == nil {
		t.Fatalf("failed cell state = %q", state)
	}
	if state, _ := service.modelSync.state("u2-bbbbbbbbbbbb", openClawMappingFingerprint(desired, 11)); state != OpenClawModelSyncSynced {
		t.Fatalf("written cell state = %q", state)
	}
	// A different key or model list is a new mapping the cell does not hold.
	if state, _ := service.modelSync.state("u2-bbbbbbbbbbbb", openClawMappingFingerprint(desired, 99)); state != OpenClawModelSyncPending {
		t.Fatalf("stale cell state = %q", state)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncModelMappingsSkipsWhenNoCellIsLive(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	groups := &openClawGroupRepoStub{}
	service := NewOpenClawService(db, &modelSyncFleetStub{})
	service.SetAPIKeyService(&APIKeyService{groupRepo: groups})
	mock.ExpectQuery(liveCellsQuery).WillReturnRows(sqlmock.NewRows([]string{"tenant_slug", "api_key_id"}))
	if err := service.SyncModelMappings(context.Background()); err != nil {
		t.Fatal(err)
	}
	// With no workspace to serve, the sync must not create the billing group.
	if groups.created != nil {
		t.Fatalf("sync created a group: %#v", groups.created)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdminInstanceModelPolicyReportsMappingErrors(t *testing.T) {
	t.Setenv("OPENCLAW_MODEL_GATEWAY_BASE_URL", "not a url")
	t.Setenv("OPENCLAW_MODEL_MAPPING_JSON", "")
	var item OpenClawAdminInstance
	item.applyModelPolicy(&Group{ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-6-sol"}}}, sql.NullInt64{Int64: 10, Valid: true}, &openClawModelSync{})
	if item.MappingEnabled || item.MappingError == "" {
		t.Fatalf("mapping error hidden: %#v", item)
	}
	if !slices.Equal(item.AllowedModels, []string{"gpt-6-sol"}) || item.ModelsSource != OpenClawModelsFromGroup || item.DefaultModel != "gpt-6-sol" {
		t.Fatalf("models = %#v", item)
	}
}
