package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// openClawPreferredDefaultModel is the agent default when the group names no
// usable default_mapped_model.
const openClawPreferredDefaultModel = "gpt-5.6-sol"

// Model-mapping sources reported to the admin fleet.
const (
	OpenClawModelsFromGroup    = "group"
	OpenClawModelsFromDefaults = "defaults"
	// OpenClawModelsFallback: the allowlist is on but names no model a cell
	// can offer, so the built-in list is used.
	OpenClawModelsFallback = "fallback"
)

// openClawSupportedModels lists the concrete models a workspace offers. The
// managed billing group's model allowlist is the single source: the gateway
// admits exactly those models for the workspace key, so the cell offers the
// same set, in the administrator's order. Wildcard entries expand against the
// built-in defaults because a cell needs concrete model IDs. A group without
// an enabled allowlist admits every model and the cell offers the defaults.
func openClawSupportedModels(group *Group) (models []string, defaultModel, source string) {
	source = OpenClawModelsFromDefaults
	candidates := openClawAllowedModels
	if group != nil && group.ModelAllowlist.Enabled && len(group.ModelAllowlist.Models) > 0 {
		source = OpenClawModelsFromGroup
		candidates = group.ModelAllowlist.Models
	}
	seen := make(map[string]struct{}, len(candidates))
	add := func(model string) {
		model = normalizeOpenClawModelID(model)
		if model == "" || len([]rune(model)) > 100 {
			return
		}
		if _, ok := seen[model]; ok {
			return
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	for _, entry := range candidates {
		if strings.Contains(entry, "*") {
			for _, known := range openClawAllowedModels {
				if groupAllowlistPatternMatches(normalizeOpenClawModelID(entry), known) {
					add(known)
				}
			}
			continue
		}
		add(entry)
	}
	if len(models) == 0 && source == OpenClawModelsFromGroup {
		// The allowlist names no concrete model a cell can offer (for example
		// only claude-*). Never block a workspace over it: offer the built-in
		// list, as before the group became the source, and say so.
		source = OpenClawModelsFallback
		models = append([]string(nil), openClawAllowedModels...)
	}
	if group != nil {
		defaultModel = pickOpenClawDefaultModel(models, group.DefaultMappedModel)
	} else {
		defaultModel = pickOpenClawDefaultModel(models, "")
	}
	return models, defaultModel, source
}

func normalizeOpenClawModelID(model string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(model)), "openai/")
}

func pickOpenClawDefaultModel(models []string, preferred ...string) string {
	candidates := append(append([]string(nil), preferred...), openClawPreferredDefaultModel)
	for _, candidate := range candidates {
		candidate = normalizeOpenClawModelID(candidate)
		for _, model := range models {
			if candidate != "" && model == candidate {
				return model
			}
		}
	}
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

// configuredOpenClawModelMapping builds the provider map written into a tenant
// cell. Mapping is enabled by OPENCLAW_MODEL_GATEWAY_BASE_URL; the models come
// from the billing group (see openClawSupportedModels), so a change in Group
// Management reaches every workspace without a code change.
//
// OPENCLAW_MODEL_MAPPING_JSON may still rename models or choose the default.
// Its entries only apply to models the group allows; models it omits are
// offered under their own names. The cell never offers a model the gateway
// would reject for the workspace key.
func configuredOpenClawModelMapping(apiKey string, group *Group) (OpenClawModelMapping, bool, error) {
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
	supported, defaultModel, _ := openClawSupportedModels(group)
	if len(supported) == 0 {
		return OpenClawModelMapping{}, false, errors.New("the OpenClaw billing group allows no model a workspace can offer")
	}
	allowed := make(map[string]struct{}, len(supported))
	for _, model := range supported {
		allowed[model] = struct{}{}
	}
	var payload struct {
		DefaultModel string               `json:"default_model"`
		Models       []OpenClawModelAlias `json:"models"`
	}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return OpenClawModelMapping{}, false, fmt.Errorf("OPENCLAW_MODEL_MAPPING_JSON is invalid: %w", err)
		}
	}
	models := make([]OpenClawModelAlias, 0, len(supported))
	aliases := make(map[string]struct{}, len(supported))
	covered := make(map[string]struct{}, len(supported))
	for _, entry := range payload.Models {
		alias := normalizeOpenClawModelID(entry.Alias)
		target := normalizeOpenClawModelID(entry.Model)
		_, aliasAllowed := allowed[alias]
		_, targetAllowed := allowed[target]
		_, duplicate := aliases[alias]
		if !aliasAllowed || !targetAllowed || duplicate {
			continue
		}
		aliases[alias] = struct{}{}
		covered[target] = struct{}{}
		models = append(models, OpenClawModelAlias{Alias: alias, Model: target})
	}
	for _, model := range supported {
		_, isAlias := aliases[model]
		_, isTarget := covered[model]
		if isAlias || isTarget {
			continue
		}
		aliases[model] = struct{}{}
		models = append(models, OpenClawModelAlias{Alias: model, Model: model})
	}
	names := make([]string, 0, len(models))
	for _, model := range models {
		names = append(names, model.Alias)
	}
	return OpenClawModelMapping{
		GatewayURL:   baseURL,
		APIKey:       apiKey,
		DefaultModel: pickOpenClawDefaultModel(names, payload.DefaultModel, defaultModel),
		Models:       models,
	}, true, nil
}

// openClawMappingGroup is the group whose policy a cell's mapping follows:
// the managed billing group that every workspace key is bound to.
func (s *OpenClawService) openClawMappingGroup(ctx context.Context) (*Group, error) {
	if s.apiKeys == nil {
		return nil, nil
	}
	return s.apiKeys.EnsureOpenClawGroup(ctx)
}

func (s *OpenClawService) workspaceModelMapping(ctx context.Context, apiKey string) (OpenClawModelMapping, bool, error) {
	group, err := s.openClawMappingGroup(ctx)
	if err != nil {
		return OpenClawModelMapping{}, false, err
	}
	return configuredOpenClawModelMapping(apiKey, group)
}

// openClawMappingFingerprint identifies what a cell was given, without the
// secret: the gateway, the billing key, the default and every alias.
func openClawMappingFingerprint(mapping OpenClawModelMapping, apiKeyID int64) string {
	sum := sha256.New()
	_, _ = fmt.Fprintf(sum, "%s\n%d\n%s\n", mapping.GatewayURL, apiKeyID, mapping.DefaultModel)
	for _, model := range mapping.Models {
		_, _ = fmt.Fprintf(sum, "%s=%s\n", model.Alias, model.Model)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// Model sync states reported to the admin fleet.
const (
	OpenClawModelSyncSynced  = "synced"
	OpenClawModelSyncPending = "pending"
	OpenClawModelSyncFailed  = "failed"
)

// openClawModelSyncSetting is the settings row holding, per tenant, the
// fingerprint of the mapping last delivered to its cell. Keeping it in the
// database means a restart or deploy never rewrites a cell: a write would
// also reset the workspace's default agent model.
const openClawModelSyncSetting = "openclaw_cell_model_sync"

type openClawModelSyncRecord struct {
	Fingerprint string    `json:"fingerprint"`
	At          time.Time `json:"at"`
	err         string
}

type openClawModelSync struct {
	mu      sync.Mutex
	records map[string]openClawModelSyncRecord
	loaded  bool
	// persist serialises writes of the settings row so an older snapshot
	// can never overwrite a newer one.
	persist sync.Mutex
}

func (m *openClawModelSync) record(tenant, fingerprint string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records == nil {
		m.records = make(map[string]openClawModelSyncRecord)
	}
	record := openClawModelSyncRecord{Fingerprint: fingerprint, At: time.Now().UTC()}
	if err != nil {
		record.err = err.Error()
	}
	m.records[tenant] = record
}

// adopt installs persisted records for tenants this process has not written yet.
func (m *openClawModelSync) adopt(stored map[string]openClawModelSyncRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records == nil {
		m.records = make(map[string]openClawModelSyncRecord)
	}
	for tenant, record := range stored {
		if _, ok := m.records[tenant]; !ok && record.Fingerprint != "" {
			record.err = ""
			m.records[tenant] = record
		}
	}
	m.loaded = true
}

func (m *openClawModelSync) isLoaded() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loaded
}

// prune forgets tenants that no longer exist and reports whether any did.
func (m *openClawModelSync) prune(keep map[string]struct{}) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := false
	for tenant := range m.records {
		if _, ok := keep[tenant]; !ok {
			delete(m.records, tenant)
			changed = true
		}
	}
	return changed
}

// delivered returns the records of successful writes, the ones worth keeping.
func (m *openClawModelSync) delivered() map[string]openClawModelSyncRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]openClawModelSyncRecord, len(m.records))
	for tenant, record := range m.records {
		if record.err == "" {
			out[tenant] = record
		}
	}
	return out
}

func (m *openClawModelSync) state(tenant, fingerprint string) (string, *time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[tenant]
	if !ok || record.Fingerprint != fingerprint {
		return OpenClawModelSyncPending, nil
	}
	at := record.At
	if record.err != "" {
		return OpenClawModelSyncFailed, &at
	}
	return OpenClawModelSyncSynced, &at
}

// openClawModelSyncRetry spaces out writes to a cell whose last write failed.
const openClawModelSyncRetry = 10 * time.Minute

func (m *openClawModelSync) due(tenant, fingerprint string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[tenant]
	if !ok || record.Fingerprint != fingerprint {
		return true
	}
	return record.err != "" && now.Sub(record.At) >= openClawModelSyncRetry
}

func (s *OpenClawService) loadModelSyncState(ctx context.Context) (map[string]openClawModelSyncRecord, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = $1`, openClawModelSyncSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	stored := map[string]openClawModelSyncRecord{}
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		// An unreadable row is replaced by the next write; nothing is rewritten.
		return map[string]openClawModelSyncRecord{}, true, nil
	}
	return stored, true, nil
}

func (s *OpenClawService) persistModelSync(ctx context.Context) error {
	s.modelSync.persist.Lock()
	defer s.modelSync.persist.Unlock()
	raw, err := json.Marshal(s.modelSync.delivered())
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`,
		openClawModelSyncSetting, string(raw))
	return err
}

// recordModelSync notes what a cell now holds after create, provision or
// Repair wrote it, so the background pass does not write it again.
func (s *OpenClawService) recordModelSync(ctx context.Context, tenant string, mapping OpenClawModelMapping, apiKeyID int64, err error) {
	s.modelSync.record(tenant, openClawMappingFingerprint(mapping, apiKeyID), err)
	// Before the first background pass has loaded the stored record, a write
	// here would replace it with this one tenant; that pass saves it instead.
	if err == nil && s.modelSync.isLoaded() {
		_ = s.persistModelSync(ctx)
	}
}

// findOpenClawGroup reads the managed billing group without creating or
// repairing it; the background pass must not change the group.
func (s *APIKeyService) findOpenClawGroup(ctx context.Context) (*Group, error) {
	if s == nil || s.groupRepo == nil {
		return nil, nil
	}
	if repo, ok := s.groupRepo.(groupByNameRepository); ok {
		group, err := repo.GetByName(ctx, OpenClawBillingGroupName)
		if errors.Is(err, ErrGroupNotFound) {
			return nil, nil
		}
		return group, err
	}
	groups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		if groups[i].Name == OpenClawBillingGroupName {
			return &groups[i], nil
		}
	}
	return nil, nil
}

// SyncModelMappings writes the mapping into a live cell only when what the
// cell should hold changed since it was last written: the administrator
// edited the group's models or default, or the workspace's billing key was
// replaced. A restart, a deploy or an unchanged group writes nothing.
//
// The first pass of a process adopts the persisted record. Without one (the
// first run of this version) it records the current mapping as delivered
// without writing any cell: the cells already hold what the previous version
// gave them, and only later changes are delivered.
func (s *OpenClawService) SyncModelMappings(ctx context.Context) error {
	if s.apiKeys == nil || s.fleet == nil || !s.fleet.Enabled() {
		return nil
	}
	configurator, ok := s.fleet.(OpenClawFleetConfigurator)
	if !ok {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.tenant_slug, link.api_key_id, i.status
		FROM openclaw_instances i
		JOIN openclaw_api_keys link ON link.instance_id = i.id
		JOIN api_keys k ON k.id = link.api_key_id AND k.deleted_at IS NULL
		WHERE i.deletion_scheduled_at IS NULL
		ORDER BY i.id ASC`)
	if err != nil {
		return err
	}
	type cell struct {
		tenant   string
		apiKeyID int64
		live     bool
	}
	var cells []cell
	for rows.Next() {
		var item cell
		var status string
		if err := rows.Scan(&item.tenant, &item.apiKeyID, &status); err != nil {
			_ = rows.Close()
			return err
		}
		item.live = status == "ready" || status == "running"
		cells = append(cells, item)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	group, err := s.apiKeys.findOpenClawGroup(ctx)
	if err != nil || group == nil {
		return err
	}
	// The fingerprint never needs the secret; load a key only for a write.
	desired, enabled, err := configuredOpenClawModelMapping(strings.Repeat("x", 32), group)
	if err != nil || !enabled {
		return err
	}
	changed := false
	if !s.modelSync.isLoaded() {
		// Save after this pass in any case, so writes made by create,
		// provision or Repair before it are kept.
		changed = true
		stored, found, err := s.loadModelSyncState(ctx)
		if err != nil {
			return err
		}
		if !found {
			stored = make(map[string]openClawModelSyncRecord, len(cells))
			for _, item := range cells {
				stored[item.tenant] = openClawModelSyncRecord{Fingerprint: openClawMappingFingerprint(desired, item.apiKeyID), At: time.Now().UTC()}
			}
		}
		s.modelSync.adopt(stored)
	}
	keep := make(map[string]struct{}, len(cells))
	for _, item := range cells {
		keep[item.tenant] = struct{}{}
	}
	if s.modelSync.prune(keep) {
		changed = true
	}
	var failures []error
	for _, item := range cells {
		if !item.live || !s.modelSync.due(item.tenant, openClawMappingFingerprint(desired, item.apiKeyID), time.Now()) {
			continue
		}
		key, err := s.apiKeys.GetByID(ctx, item.apiKeyID)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: load billing key: %w", item.tenant, err))
			continue
		}
		mapping, _, err := configuredOpenClawModelMapping(key.Key, group)
		if err == nil {
			configureCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			err = configurator.Configure(configureCtx, item.tenant, mapping)
			cancel()
		}
		s.modelSync.record(item.tenant, openClawMappingFingerprint(desired, item.apiKeyID), err)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", item.tenant, err))
		} else {
			changed = true
		}
	}
	if changed {
		if err := s.persistModelSync(ctx); err != nil {
			failures = append(failures, fmt.Errorf("record delivered model lists: %w", err))
		}
	}
	return errors.Join(failures...)
}

// StartModelSyncWorker runs SyncModelMappings shortly after startup and then
// on every interval.
func (s *OpenClawService) StartModelSyncWorker(initialDelay, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	go func() {
		timer := time.NewTimer(initialDelay)
		defer timer.Stop()
		for range timer.C {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			_ = s.SyncModelMappings(ctx)
			cancel()
			timer.Reset(interval)
		}
	}()
}
