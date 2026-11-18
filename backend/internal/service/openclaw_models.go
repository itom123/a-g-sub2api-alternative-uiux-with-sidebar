package service

import (
	"context"
	"crypto/sha256"
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

type openClawModelSyncRecord struct {
	fingerprint string
	err         string
	at          time.Time
}

// openClawModelSync remembers the mapping last written to each cell. It is
// process memory: after a restart the first pass writes every live cell once,
// which is the same idempotent config patch as Repair OpenClaw binding.
type openClawModelSync struct {
	mu      sync.Mutex
	records map[string]openClawModelSyncRecord
}

func (m *openClawModelSync) record(tenant, fingerprint string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records == nil {
		m.records = make(map[string]openClawModelSyncRecord)
	}
	record := openClawModelSyncRecord{fingerprint: fingerprint, at: time.Now()}
	if err != nil {
		record.err = err.Error()
	}
	m.records[tenant] = record
}

func (m *openClawModelSync) state(tenant, fingerprint string) (string, *time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[tenant]
	if !ok || record.fingerprint != fingerprint {
		return OpenClawModelSyncPending, nil
	}
	at := record.at
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
	if !ok || record.fingerprint != fingerprint {
		return true
	}
	return record.err != "" && now.Sub(record.at) >= openClawModelSyncRetry
}

func (s *OpenClawService) recordModelSync(tenant string, mapping OpenClawModelMapping, apiKeyID int64, err error) {
	s.modelSync.record(tenant, openClawMappingFingerprint(mapping, apiKeyID), err)
}

// SyncModelMappings writes the current mapping into every live cell whose last
// write differs, so a model added to or removed from the billing group reaches
// existing workspaces without anyone pressing Repair.
func (s *OpenClawService) SyncModelMappings(ctx context.Context) error {
	if s.apiKeys == nil || s.fleet == nil || !s.fleet.Enabled() {
		return nil
	}
	configurator, ok := s.fleet.(OpenClawFleetConfigurator)
	if !ok {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.tenant_slug, link.api_key_id
		FROM openclaw_instances i
		JOIN openclaw_api_keys link ON link.instance_id = i.id
		JOIN api_keys k ON k.id = link.api_key_id AND k.deleted_at IS NULL
		WHERE i.status IN ('ready', 'running') AND i.deletion_scheduled_at IS NULL
		ORDER BY i.id ASC`)
	if err != nil {
		return err
	}
	type liveCell struct {
		tenant   string
		apiKeyID int64
	}
	var cells []liveCell
	for rows.Next() {
		var cell liveCell
		if err := rows.Scan(&cell.tenant, &cell.apiKeyID); err != nil {
			_ = rows.Close()
			return err
		}
		cells = append(cells, cell)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(cells) == 0 {
		return nil
	}
	group, err := s.openClawMappingGroup(ctx)
	if err != nil {
		return err
	}
	// The fingerprint never needs the secret; load a key only for a write.
	desired, enabled, err := configuredOpenClawModelMapping(strings.Repeat("x", 32), group)
	if err != nil || !enabled {
		return err
	}
	var failures []error
	for _, cell := range cells {
		if !s.modelSync.due(cell.tenant, openClawMappingFingerprint(desired, cell.apiKeyID), time.Now()) {
			continue
		}
		key, err := s.apiKeys.GetByID(ctx, cell.apiKeyID)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: load billing key: %w", cell.tenant, err))
			continue
		}
		mapping, _, err := configuredOpenClawModelMapping(key.Key, group)
		if err == nil {
			configureCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			err = configurator.Configure(configureCtx, cell.tenant, mapping)
			cancel()
		}
		s.recordModelSync(cell.tenant, desired, cell.apiKeyID, err)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", cell.tenant, err))
		}
	}
	return errors.Join(failures...)
}

// openClawModelSyncEnabled reports whether the background pass may write to
// live cells. It is off unless OPENCLAW_MODEL_SYNC_ENABLED=1. The fleet
// worker's configure patch replaces `agents.defaults.model` with a single
// model (dropping fallbacks), replaces the model list (dropping per-model
// settings such as image input) and re-sends the gateway section, which can
// restart a cell's gateway. Until that patch preserves cell settings, the
// mapping reaches cells only on create, provision and Repair, as before.
func openClawModelSyncEnabled() bool {
	return strings.TrimSpace(os.Getenv("OPENCLAW_MODEL_SYNC_ENABLED")) == "1"
}

// StartModelSyncWorker runs SyncModelMappings shortly after startup and then
// on every interval, when openClawModelSyncEnabled allows it.
func (s *OpenClawService) StartModelSyncWorker(initialDelay, interval time.Duration) {
	if !openClawModelSyncEnabled() {
		return
	}
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
