package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOfficialPricingCorrectsStaleCatalogAndKeepsOperatorOverrides(t *testing.T) {
	const catalog = `{
		"gpt-5.6-sol": {"input_cost_per_token": 0.000005, "output_cost_per_token": 0.00003, "max_input_tokens": 922000},
		"claude-sonnet-5-5": {"input_cost_per_token": 0.000002, "output_cost_per_token": 0.00001, "cache_read_input_token_cost": 0.0000002},
		"bedrock/claude-sonnet-5-5": {"input_cost_per_token": 0.000009, "cache_read_input_token_cost": 0.0000009},
		"custom-model": {"input_cost_per_token": 0.000007}
	}`
	svc := &PricingService{}
	data, err := svc.parseCatalogPricingData([]byte(catalog))
	require.NoError(t, err)
	require.InDelta(t, 4e-6, data["gpt-5.6-sol"].InputCostPerToken, 1e-15)
	require.InDelta(t, 20e-6, data["gpt-5.6-sol"].OutputCostPerToken, 1e-15)
	require.InDelta(t, 0.1e-6, data["claude-sonnet-5-5"].CacheReadInputTokenCost, 1e-15)
	require.InDelta(t, 0.9e-6, data["bedrock/claude-sonnet-5-5"].CacheReadInputTokenCost, 1e-15)
	require.InDelta(t, 7e-6, data["custom-model"].InputCostPerToken, 1e-15)
	require.InDelta(t, 1e-6, data["gpt-6.1-terra"].InputCostPerToken, 1e-15)

	// Correctly synced fields survive, while explicit local overrides win.
	svc = newPricingServiceWithOverride(t, `{"claude-sonnet-5-5":{"cache_read_input_token_cost":0.0000003},"gpt-6.1-terra":{"cache_creation_input_token_cost":0,"cache_creation_input_token_cost_above_12hr":0}}`)
	data, err = svc.parseCatalogPricingData([]byte(catalog))
	require.NoError(t, err)
	require.InDelta(t, 2e-6, data["claude-sonnet-5-5"].InputCostPerToken, 1e-15)
	require.InDelta(t, 0.3e-6, data["claude-sonnet-5-5"].CacheReadInputTokenCost, 1e-15)
	require.Zero(t, data["gpt-6.1-terra"].CacheCreationInputTokenCostAbove12hr)
	billing := NewBillingService(&config.Config{}, &PricingService{pricingData: data})
	cost, err := billing.CalculateCostWithServiceTier("gpt-6.1-terra", UsageTokens{CacheCreationTokens: 100, CacheCreation12hTokens: 50}, 1, "fast")
	require.NoError(t, err)
	require.Zero(t, cost.CacheCreationCost)
}

func TestOpenAI12hCacheBillingUsesSeparateWritesAcrossTiers(t *testing.T) {
	data, err := (&PricingService{}).parseCatalogPricingData([]byte(`{"sentinel":{"input_cost_per_token":1}}`))
	require.NoError(t, err)
	for _, dynamic := range []bool{false, true} {
		var pricing *PricingService
		if dynamic {
			pricing = &PricingService{pricingData: data}
		}
		billing := NewBillingService(&config.Config{}, pricing)
		for _, tier := range []struct {
			name       string
			multiplier float64
		}{{"default", 1}, {"fast", 2}, {"flex", 0.5}, {"ultrafast", 6}} {
			usage, ok := openAIUsageFromGJSON(gjson.Parse(`{"input_tokens":12000,"output_tokens":100,"input_tokens_details":{"cached_tokens":4000,"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}}}`))
			require.True(t, ok)
			require.Equal(t, 4000, usage.CacheCreation12hInputTokens)
			cost, err := billing.CalculateCostWithServiceTier("gpt-6.1-sol", UsageTokens{
				InputTokens:  usage.InputTokens - usage.CacheReadInputTokens - usage.CacheCreationInputTokens,
				OutputTokens: usage.OutputTokens, CacheReadTokens: usage.CacheReadInputTokens,
				CacheCreationTokens: usage.CacheCreationInputTokens, CacheCreation12hTokens: usage.CacheCreation12hInputTokens,
			}, 1.1, tier.name)
			require.NoError(t, err)
			require.InDelta(t, 1000*2e-6*tier.multiplier, cost.InputCost, 1e-12)
			require.InDelta(t, (3000*2.5e-6+4000*5e-6)*tier.multiplier, cost.CacheCreationCost, 1e-12)
			require.InDelta(t, 4000*0.1e-6*tier.multiplier, cost.CacheReadCost, 1e-12)
			require.InDelta(t, cost.TotalCost*1.1, cost.ActualCost, 1e-12)
		}
	}
}

func TestOpenAI12hCacheLongContextAndCustomPrices(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	tokens := UsageTokens{InputTokens: 261000, CacheReadTokens: 4000, CacheCreationTokens: 7000, CacheCreation12hTokens: 4000, OutputTokens: 100}
	base, err := billing.CalculateCost("gpt-6.1-terra", tokens, 1)
	require.NoError(t, err)
	require.False(t, base.LongContextBillingApplied)
	tokens.InputTokens++
	long, err := billing.CalculateCost("gpt-6.1-terra", tokens, 1)
	require.NoError(t, err)
	require.True(t, long.LongContextBillingApplied)
	require.InDelta(t, base.CacheCreationCost*2, long.CacheCreationCost, 1e-12)
	require.InDelta(t, base.OutputCost*1.5, long.OutputCost, 1e-12)

	price := 3e-6
	custom, err := billing.GetModelPricingWithChannel("gpt-6.1-terra", &ChannelModelPricing{CacheWritePrice: &price})
	require.NoError(t, err)
	charged := billing.computeTokenBreakdown(custom, tokens, 1, "default", false)
	require.InDelta(t, 7000*price, charged.CacheCreationCost, 1e-12)
}

func TestOfficialClaudeFastCachePricesAndHaikuBoundary(t *testing.T) {
	data, err := (&PricingService{}).parseCatalogPricingData([]byte(`{"claude-opus-5-5":{"input_cost_per_token":0.000004,"input_cost_per_token_priority":0.000008,"cache_creation_input_token_cost_priority":0.00001}}`))
	require.NoError(t, err)
	billing := NewBillingService(&config.Config{}, &PricingService{pricingData: data})
	fast, err := billing.CalculateCostWithServiceTier("claude-opus-5-5", UsageTokens{CacheCreationTokens: 1000, CacheCreation5mTokens: 400, CacheCreation1hTokens: 600}, 1, "fast")
	require.NoError(t, err)
	require.InDelta(t, (400*5e-6+600*8e-6)*2, fast.CacheCreationCost, 1e-12)
	tokens := UsageTokens{InputTokens: 99000, CacheReadTokens: 500, CacheCreationTokens: 500, OutputTokens: 10}
	base, err := billing.CalculateCost("claude-haiku-5-5", tokens, 1)
	require.NoError(t, err)
	require.False(t, base.LongContextBillingApplied)
	tokens.InputTokens++
	long, err := billing.CalculateCost("claude-haiku-5-5", tokens, 1)
	require.NoError(t, err)
	require.True(t, long.LongContextBillingApplied)
	require.InDelta(t, base.CacheReadCost*5, long.CacheReadCost, 1e-12)
	require.InDelta(t, base.CacheCreationCost*5, long.CacheCreationCost, 1e-12)
}
