package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"slices"
	"strings"
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

const syncCellsQuery = `SELECT i\.tenant_slug, link\.api_key_id, i\.status\s+FROM openclaw_instances i`
const syncStateRead = `SELECT value FROM settings WHERE key = \$1`
const syncStateWrite = `INSERT INTO settings \(key, value, updated_at\)`

type syncCell struct {
	tenant string
	keyID  int64
	status string
}

func expectSyncCells(mock sqlmock.Sqlmock, cells ...syncCell) {
	rows := sqlmock.NewRows([]string{"tenant_slug", "api_key_id", "status"})
	for _, cell := range cells {
		rows.AddRow(cell.tenant, cell.keyID, cell.status)
	}
	mock.ExpectQuery(syncCellsQuery).WillReturnRows(rows)
}

// storedSync captures the JSON written to the settings row.
type storedSync struct{ value map[string]openClawModelSyncRecord }

func (c *storedSync) Match(v driver.Value) bool {
	raw, ok := v.(string)
	if !ok {
		return false
	}
	c.value = map[string]openClawModelSyncRecord{}
	return json.Unmarshal([]byte(raw), &c.value) == nil
}

func syncTestGroup() *Group {
	return &Group{
		ID: 52, Name: OpenClawBillingGroupName, Description: OpenClawBillingGroupDescription,
		Platform: PlatformOpenAI, RateMultiplier: 0.3, Status: StatusActive,
		SubscriptionType: SubscriptionTypeStandard, MaxReasoningEffort: "max",
		MaxReasoningEffortOverLimit: ReasoningEffortOverLimitDowngrade,
		ModelAllowlist:              GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.6-sol", "gpt-6-sol"}},
	}
}

func newSyncService(t *testing.T, group *Group, fleet *modelSyncFleetStub, keys map[int64]string) (*OpenClawService, sqlmock.Sqlmock, *openClawGroupRepoStub) {
	t.Helper()
	t.Setenv("OPENCLAW_MODEL_GATEWAY_BASE_URL", "https://gateway.example/v1")
	t.Setenv("OPENCLAW_MODEL_MAPPING_JSON", "")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	groups := &openClawGroupRepoStub{byName: group}
	service := NewOpenClawService(db, fleet)
	service.SetAPIKeyService(&APIKeyService{groupRepo: groups, apiKeyRepo: &modelSyncKeyRepoStub{keys: keys}})
	return service, mock, groups
}

func syncFingerprint(t *testing.T, group *Group, keyID int64) string {
	t.Helper()
	mapping, enabled, err := configuredOpenClawModelMapping(strings.Repeat("x", 32), group)
	if err != nil || !enabled {
		t.Fatalf("mapping enabled=%v err=%v", enabled, err)
	}
	return openClawMappingFingerprint(mapping, keyID)
}

var (
	cellOne   = syncCell{"u1-aaaaaaaaaaaa", 10, "running"}
	cellTwo   = syncCell{"u2-bbbbbbbbbbbb", 11, "ready"}
	cellThree = syncCell{"u3-cccccccccccc", 12, "stopped"}
	syncKeys  = map[int64]string{10: "sk-first-workspace-key-0000", 11: "sk-second-workspace-key-000", 12: "sk-third-workspace-key-0000", 20: "sk-replaced-workspace-key-0"}
)

// A deploy or restart must never rewrite a cell: a write also resets the
// workspace's default agent model. The first run of this version records
// what the cells hold instead of writing them.
func TestSyncModelMappingsNeverRewritesCellsOnStartup(t *testing.T) {
	group := syncTestGroup()
	fleet := &modelSyncFleetStub{}
	service, mock, _ := newSyncService(t, group, fleet, syncKeys)
	ctx := context.Background()

	stored := &storedSync{}
	expectSyncCells(mock, cellOne, cellTwo, cellThree)
	mock.ExpectQuery(syncStateRead).WithArgs(openClawModelSyncSetting).WillReturnRows(sqlmock.NewRows([]string{"value"}))
	mock.ExpectExec(syncStateWrite).WithArgs(openClawModelSyncSetting, stored).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := service.SyncModelMappings(ctx); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	for _, cell := range []syncCell{cellOne, cellTwo, cellThree} {
		if writes := fleet.writes(cell.tenant); len(writes) != 0 {
			t.Fatalf("%s was rewritten on startup: %#v", cell.tenant, writes)
		}
		if stored.value[cell.tenant].Fingerprint != syncFingerprint(t, group, cell.keyID) {
			t.Fatalf("%s not recorded as delivered: %#v", cell.tenant, stored.value)
		}
		if state, _ := service.modelSync.state(cell.tenant, syncFingerprint(t, group, cell.keyID)); state != OpenClawModelSyncSynced {
			t.Fatalf("%s state = %q", cell.tenant, state)
		}
	}

	// An unchanged group writes nothing at all, not even the settings row.
	expectSyncCells(mock, cellOne, cellTwo, cellThree)
	if err := service.SyncModelMappings(ctx); err != nil {
		t.Fatalf("second pass: %v", err)
	}

	// After a restart the stored record is adopted: still no writes.
	restarted := NewOpenClawService(service.db, fleet)
	restarted.SetAPIKeyService(service.apiKeys)
	raw, _ := json.Marshal(stored.value)
	expectSyncCells(mock, cellOne, cellTwo, cellThree)
	mock.ExpectQuery(syncStateRead).WithArgs(openClawModelSyncSetting).WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(string(raw)))
	mock.ExpectExec(syncStateWrite).WithArgs(openClawModelSyncSetting, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := restarted.SyncModelMappings(ctx); err != nil {
		t.Fatalf("restarted pass: %v", err)
	}
	for _, cell := range []syncCell{cellOne, cellTwo, cellThree} {
		if writes := fleet.writes(cell.tenant); len(writes) != 0 {
			t.Fatalf("%s was rewritten after a restart", cell.tenant)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A model the administrator adds reaches every live cell; a stopped cell gets
// it when it runs again; a replaced billing key reaches only its own cell.
func TestSyncModelMappingsDeliversOnlyRealChanges(t *testing.T) {
	group := syncTestGroup()
	fleet := &modelSyncFleetStub{}
	service, mock, groups := newSyncService(t, group, fleet, syncKeys)
	ctx := context.Background()

	expectSyncCells(mock, cellOne, cellTwo, cellThree)
	mock.ExpectQuery(syncStateRead).WithArgs(openClawModelSyncSetting).WillReturnRows(sqlmock.NewRows([]string{"value"}))
	mock.ExpectExec(syncStateWrite).WithArgs(openClawModelSyncSetting, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := service.SyncModelMappings(ctx); err != nil {
		t.Fatal(err)
	}

	groups.byName.ModelAllowlist.Models = append(groups.byName.ModelAllowlist.Models, "gpt-6.2-sol")
	expectSyncCells(mock, cellOne, cellTwo, cellThree)
	mock.ExpectExec(syncStateWrite).WithArgs(openClawModelSyncSetting, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := service.SyncModelMappings(ctx); err != nil {
		t.Fatalf("group change: %v", err)
	}
	for _, cell := range []syncCell{cellOne, cellTwo} {
		writes := fleet.writes(cell.tenant)
		if len(writes) != 1 || !slices.Equal(openClawMappingAliases(writes[0]), []string{"gpt-5.6-sol", "gpt-6-sol", "gpt-6.2-sol"}) || writes[0].APIKey != syncKeys[cell.keyID] {
			t.Fatalf("%s writes = %#v", cell.tenant, writes)
		}
	}
	if len(fleet.writes(cellThree.tenant)) != 0 {
		t.Fatal("a stopped cell was written")
	}

	started := cellThree
	started.status = "running"
	expectSyncCells(mock, cellOne, cellTwo, started)
	mock.ExpectExec(syncStateWrite).WithArgs(openClawModelSyncSetting, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := service.SyncModelMappings(ctx); err != nil {
		t.Fatalf("restarted cell: %v", err)
	}
	if len(fleet.writes(cellThree.tenant)) != 1 || len(fleet.writes(cellOne.tenant)) != 1 {
		t.Fatal("only the restarted cell should have been written")
	}

	replaced := cellOne
	replaced.keyID = 20
	expectSyncCells(mock, replaced, cellTwo, started)
	mock.ExpectExec(syncStateWrite).WithArgs(openClawModelSyncSetting, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := service.SyncModelMappings(ctx); err != nil {
		t.Fatalf("replaced key: %v", err)
	}
	if writes := fleet.writes(cellOne.tenant); len(writes) != 2 || writes[1].APIKey != syncKeys[20] {
		t.Fatalf("replaced key not delivered: %#v", writes)
	}
	if len(fleet.writes(cellTwo.tenant)) != 1 {
		t.Fatal("an unaffected cell was written")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A cell that rejects the write is shown as failed, retried later rather than
// on every pass, and not recorded as delivered.
func TestSyncModelMappingsReportsAndSpacesOutFailures(t *testing.T) {
	group := syncTestGroup()
	fleet := &modelSyncFleetStub{fail: map[string]error{cellOne.tenant: errors.New("cell busy")}}
	service, mock, _ := newSyncService(t, group, fleet, syncKeys)
	ctx := context.Background()
	old, _ := json.Marshal(map[string]openClawModelSyncRecord{
		cellOne.tenant: {Fingerprint: "an older mapping"}, cellTwo.tenant: {Fingerprint: "an older mapping"},
	})

	stored := &storedSync{}
	expectSyncCells(mock, cellOne, cellTwo)
	mock.ExpectQuery(syncStateRead).WithArgs(openClawModelSyncSetting).WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(string(old)))
	mock.ExpectExec(syncStateWrite).WithArgs(openClawModelSyncSetting, stored).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := service.SyncModelMappings(ctx); err == nil {
		t.Fatal("a failed cell write was not reported")
	}
	if _, ok := stored.value[cellOne.tenant]; ok {
		t.Fatalf("a failed write was recorded as delivered: %#v", stored.value)
	}
	if stored.value[cellTwo.tenant].Fingerprint != syncFingerprint(t, group, cellTwo.keyID) {
		t.Fatalf("successful write not recorded: %#v", stored.value)
	}

	expectSyncCells(mock, cellOne, cellTwo)
	_ = service.SyncModelMappings(ctx)
	if len(fleet.writes(cellOne.tenant)) != 1 || len(fleet.writes(cellTwo.tenant)) != 1 {
		t.Fatalf("writes = %d/%d, want one each", len(fleet.writes(cellOne.tenant)), len(fleet.writes(cellTwo.tenant)))
	}
	if state, at := service.modelSync.state(cellOne.tenant, syncFingerprint(t, group, cellOne.keyID)); state != OpenClawModelSyncFailed || at == nil {
		t.Fatalf("failed cell state = %q", state)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A write made by create, provision or Repair before the first pass must not
// replace the stored record with that one tenant; the pass keeps it.
func TestSyncModelMappingsKeepsWritesMadeBeforeTheFirstPass(t *testing.T) {
	group := syncTestGroup()
	fleet := &modelSyncFleetStub{}
	service, mock, _ := newSyncService(t, group, fleet, syncKeys)
	ctx := context.Background()
	mapping, _, _ := configuredOpenClawModelMapping(syncKeys[10], group)
	service.recordModelSync(ctx, cellOne.tenant, mapping, cellOne.keyID, nil)

	stored := &storedSync{}
	expectSyncCells(mock, cellOne, cellTwo)
	mock.ExpectQuery(syncStateRead).WithArgs(openClawModelSyncSetting).WillReturnRows(sqlmock.NewRows([]string{"value"}))
	mock.ExpectExec(syncStateWrite).WithArgs(openClawModelSyncSetting, stored).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := service.SyncModelMappings(ctx); err != nil {
		t.Fatal(err)
	}
	if len(stored.value) != 2 || len(fleet.writes(cellOne.tenant))+len(fleet.writes(cellTwo.tenant)) != 0 {
		t.Fatalf("stored = %#v, writes = %d/%d", stored.value, len(fleet.writes(cellOne.tenant)), len(fleet.writes(cellTwo.tenant)))
	}
	// Once loaded, a later Repair is saved straight away.
	mock.ExpectExec(syncStateWrite).WithArgs(openClawModelSyncSetting, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	service.recordModelSync(ctx, cellTwo.tenant, mapping, cellTwo.keyID, nil)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Without the managed group there is nothing to deliver, and the pass must
// not create it.
func TestSyncModelMappingsNeverCreatesTheGroup(t *testing.T) {
	service, mock, groups := newSyncService(t, nil, &modelSyncFleetStub{}, syncKeys)
	expectSyncCells(mock, cellOne)
	if err := service.SyncModelMappings(context.Background()); err != nil {
		t.Fatal(err)
	}
	if groups.created != nil || groups.updated != nil {
		t.Fatalf("sync changed the group: created=%#v updated=%#v", groups.created, groups.updated)
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
