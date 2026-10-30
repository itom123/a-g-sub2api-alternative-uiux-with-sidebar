package service

import (
	"encoding/json"
	"strings"
)

// Verified 2026-10-27 against the providers' pricing pages. Patch only these
// published fields, retaining catalog metadata and the operator override layer.
// https://developers.openai.com/api/docs/pricing
// https://platform.claude.com/docs/en/about-claude/pricing
const officialPricingCorrectionsJSON = `{
  "gpt-6-astra": {"input_cost_per_token": 0.00001, "output_cost_per_token": 0.00005, "cache_read_input_token_cost": 0.000001, "cache_creation_input_token_cost": 0.0000125, "cache_creation_input_token_cost_above_12hr": 0.000025, "long_context_input_token_threshold": 272000, "long_context_input_cost_multiplier": 2, "long_context_output_cost_multiplier": 1.5, "input_cost_per_token_priority": 0.00002, "output_cost_per_token_priority": 0.0001, "cache_read_input_token_cost_priority": 0.000002, "cache_creation_input_token_cost_priority": 0.000025, "supports_service_tier": true},
  "gpt-6.1-sol": {"input_cost_per_token": 0.000002, "output_cost_per_token": 0.00001, "cache_read_input_token_cost": 0.0000001, "cache_creation_input_token_cost": 0.0000025, "cache_creation_input_token_cost_above_12hr": 0.000005, "long_context_input_token_threshold": 272000, "long_context_input_cost_multiplier": 2, "long_context_output_cost_multiplier": 1.5, "input_cost_per_token_priority": 0.000004, "output_cost_per_token_priority": 0.00002, "cache_read_input_token_cost_priority": 2e-7, "cache_creation_input_token_cost_priority": 0.000005, "supports_service_tier": true},
  "gpt-6.1-terra": {"input_cost_per_token": 0.000001, "output_cost_per_token": 0.000005, "cache_read_input_token_cost": 0.00000005, "cache_creation_input_token_cost": 0.00000125, "cache_creation_input_token_cost_above_12hr": 0.0000025, "long_context_input_token_threshold": 272000, "long_context_input_cost_multiplier": 2, "long_context_output_cost_multiplier": 1.5, "input_cost_per_token_priority": 0.000002, "output_cost_per_token_priority": 0.00001, "cache_read_input_token_cost_priority": 1e-7, "cache_creation_input_token_cost_priority": 0.0000025, "supports_service_tier": true},
  "gpt-6-sol": {"input_cost_per_token": 0.000002, "output_cost_per_token": 0.00001, "cache_read_input_token_cost": 0.0000002, "cache_creation_input_token_cost": 0.0000025, "cache_creation_input_token_cost_above_12hr": 0.000005, "long_context_input_token_threshold": 272000, "long_context_input_cost_multiplier": 2, "long_context_output_cost_multiplier": 1.5, "input_cost_per_token_priority": 0.000004, "output_cost_per_token_priority": 0.00002, "cache_read_input_token_cost_priority": 4e-7, "cache_creation_input_token_cost_priority": 0.000005, "supports_service_tier": true},
  "gpt-6-luna": {"input_cost_per_token": 0.0000001, "output_cost_per_token": 0.0000005, "cache_read_input_token_cost": 0.00000001, "cache_creation_input_token_cost": 0.000000125, "cache_creation_input_token_cost_above_12hr": 0.00000025, "long_context_input_token_threshold": 272000, "long_context_input_cost_multiplier": 2, "long_context_output_cost_multiplier": 1.5, "input_cost_per_token_priority": 2e-7, "output_cost_per_token_priority": 0.000001, "cache_read_input_token_cost_priority": 2e-8, "cache_creation_input_token_cost_priority": 2.5e-7, "supports_service_tier": true},
  "gpt-5.6-sol": {"input_cost_per_token": 0.000004, "output_cost_per_token": 0.00002, "cache_read_input_token_cost": 0.0000004, "cache_creation_input_token_cost": 0.000005, "cache_creation_input_token_cost_above_12hr": 0.00001, "long_context_input_token_threshold": 272000, "long_context_input_cost_multiplier": 2, "long_context_output_cost_multiplier": 1.5, "input_cost_per_token_priority": 0.000008, "output_cost_per_token_priority": 0.00004, "cache_read_input_token_cost_priority": 8e-7, "cache_creation_input_token_cost_priority": 0.00001, "supports_service_tier": true},
  "gpt-5.6-terra": {"input_cost_per_token": 0.000002, "output_cost_per_token": 0.000012, "cache_read_input_token_cost": 0.0000002, "cache_creation_input_token_cost": 0.0000025, "cache_creation_input_token_cost_above_12hr": 0.000005, "long_context_input_token_threshold": 272000, "long_context_input_cost_multiplier": 2, "long_context_output_cost_multiplier": 1.5, "input_cost_per_token_priority": 0.000004, "output_cost_per_token_priority": 0.000024, "cache_read_input_token_cost_priority": 4e-7, "cache_creation_input_token_cost_priority": 0.000005, "supports_service_tier": true},
  "gpt-5.6-luna": {"input_cost_per_token": 0.0000002, "output_cost_per_token": 0.0000012, "cache_read_input_token_cost": 0.00000002, "cache_creation_input_token_cost": 0.00000025, "cache_creation_input_token_cost_above_12hr": 0.0000005, "long_context_input_token_threshold": 272000, "long_context_input_cost_multiplier": 2, "long_context_output_cost_multiplier": 1.5, "input_cost_per_token_priority": 4e-7, "output_cost_per_token_priority": 0.0000024, "cache_read_input_token_cost_priority": 4e-8, "cache_creation_input_token_cost_priority": 5e-7, "supports_service_tier": true},
  "claude-opus-5-5": {"input_cost_per_token": 0.000004, "output_cost_per_token": 0.00002, "cache_creation_input_token_cost": 0.000005, "cache_creation_input_token_cost_above_1hr": 0.000008, "cache_read_input_token_cost": 0.0000002, "long_context_input_token_threshold": 0, "input_cost_per_token_priority": 0.000008, "output_cost_per_token_priority": 0.00004, "cache_read_input_token_cost_priority": 4e-7, "cache_creation_input_token_cost_priority": 0.00001, "supports_service_tier": true},
  "claude-sonnet-5-5": {"input_cost_per_token": 0.000002, "output_cost_per_token": 0.00001, "cache_creation_input_token_cost": 0.0000025, "cache_creation_input_token_cost_above_1hr": 0.000004, "cache_read_input_token_cost": 0.0000001, "long_context_input_token_threshold": 0},
  "claude-haiku-5-5": {"input_cost_per_token": 0.0000001, "output_cost_per_token": 0.0000005, "cache_creation_input_token_cost": 0.000000125, "cache_creation_input_token_cost_above_1hr": 0.0000002, "cache_read_input_token_cost": 0.00000001, "long_context_input_token_threshold": 100000, "long_context_input_cost_multiplier": 5, "long_context_output_cost_multiplier": 5},
  "claude-fable-5-1": {"input_cost_per_token": 0.00001, "output_cost_per_token": 0.00005, "cache_creation_input_token_cost": 0.0000125, "cache_creation_input_token_cost_above_1hr": 0.00002, "cache_read_input_token_cost": 0.00000025, "long_context_input_token_threshold": 0},
  "claude-mythos-5-1": {"input_cost_per_token": 0.00001, "output_cost_per_token": 0.00005, "cache_creation_input_token_cost": 0.0000125, "cache_creation_input_token_cost_above_1hr": 0.00002, "cache_read_input_token_cost": 0.00000025, "long_context_input_token_threshold": 0}
}`

var officialFallbackPrices = func() map[string]*LiteLLMModelPricing {
	var prices map[string]*LiteLLMModelPricing
	if err := json.Unmarshal([]byte(officialPricingCorrectionsJSON), &prices); err != nil {
		panic(err)
	}
	for name, price := range prices {
		price.LiteLLMProvider = "openai"
		if strings.HasPrefix(name, "claude-") {
			price.LiteLLMProvider = "anthropic"
		}
		price.Mode = "chat"
		price.SupportsPromptCaching = true
	}
	return prices
}()

func (s *PricingService) applyOfficialPricingCorrections(data map[string]json.RawMessage) {
	var corrections map[string]json.RawMessage
	if json.Unmarshal([]byte(officialPricingCorrectionsJSON), &corrections) != nil {
		return
	}
	for name, entry := range data {
		// Partner-operated cloud platforms have their own prices.
		if strings.Contains(name, "/") && !strings.HasPrefix(name, "openai/") && !strings.HasPrefix(name, "anthropic/") {
			continue
		}
		canonical := s.extractBaseName(normalizeModelNameForPricing(name))
		patch, ok := corrections[canonical]
		if !ok {
			continue
		}
		if merged, valid := mergePricingOverrideEntry(entry, patch); valid {
			data[name] = merged
		}
	}
	// New public models remain available while the mirror catches up.
	for name, patch := range corrections {
		if _, exists := data[name]; exists {
			continue
		}
		provider := "openai"
		if strings.HasPrefix(name, "claude-") {
			provider = "anthropic"
		}
		entry, _ := json.Marshal(map[string]any{"litellm_provider": provider, "mode": "chat", "supports_prompt_caching": true})
		data[name], _ = mergePricingOverrideEntry(entry, patch)
	}
}

func (s *PricingService) parseCatalogPricingData(body []byte) (map[string]*LiteLLMModelPricing, error) {
	var data map[string]json.RawMessage
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	if data == nil {
		data = make(map[string]json.RawMessage)
	}
	s.applyOfficialPricingCorrections(data)
	corrected, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return s.parsePricingData(corrected)
}

func (s *BillingService) initOfficialFallbackPricing() {
	for name, rate := range officialFallbackPrices {
		s.fallbackPrices[name] = &ModelPricing{
			InputPricePerToken:                 rate.InputCostPerToken,
			InputPricePerTokenPriority:         rate.InputCostPerTokenPriority,
			OutputPricePerToken:                rate.OutputCostPerToken,
			OutputPricePerTokenPriority:        rate.OutputCostPerTokenPriority,
			CacheReadPricePerToken:             rate.CacheReadInputTokenCost,
			CacheReadPricePerTokenPriority:     rate.CacheReadInputTokenCostPriority,
			CacheCreationPricePerToken:         rate.CacheCreationInputTokenCost,
			CacheCreationPricePerTokenPriority: rate.CacheCreationInputTokenCostPriority,
			CacheCreation5mPrice:               rate.CacheCreationInputTokenCost,
			CacheCreation1hPrice:               rate.CacheCreationInputTokenCostAbove1hr,
			CacheCreation12hPrice:              rate.CacheCreationInputTokenCostAbove12hr,
			SupportsCacheBreakdown:             rate.CacheCreationInputTokenCostAbove1hr > 0,
			LongContextInputThreshold:          rate.LongContextInputTokenThreshold,
			LongContextInputMultiplier:         rate.LongContextInputCostMultiplier,
			LongContextOutputMultiplier:        rate.LongContextOutputCostMultiplier,
		}
	}
}
