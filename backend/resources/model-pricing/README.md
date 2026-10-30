# Model Pricing Data

This directory contains a local copy of the mirrored model pricing data as a fallback mechanism.

## Source
The original file is maintained by the LiteLLM project and mirrored into the `price-mirror` branch of this repository via GitHub Actions:
- Mirror branch (configurable via `PRICE_MIRROR_REPO`): https://raw.githubusercontent.com/<your-repo>/price-mirror/model_prices_and_context_window.json
- Upstream source: https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json

## Purpose
This local copy serves as a fallback when the remote file cannot be downloaded due to:
- Network restrictions
- Firewall rules
- DNS resolution issues
- GitHub being blocked in certain regions
- Docker container network limitations

## Update Process
The pricingService will:
1. First attempt to download the latest version from GitHub
2. If download fails, use this local copy as fallback
3. Log a warning when using the fallback file

Catalog loads also apply the official OpenAI/Claude rates verified on 2026-10-27
in `internal/service/pricing_official.go`. Correct synced fields retain their
values; stale published fields are corrected and new public model cards are
filled in. `pricing.override_file` remains the highest-priority file layer, and
group/channel custom prices continue to take precedence during billing.

The verification sources are [OpenAI API pricing](https://developers.openai.com/api/docs/pricing),
[OpenAI prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching),
and [Claude pricing](https://platform.claude.com/docs/en/about-claude/pricing).
OpenAI 30-minute and 12-hour cache writes are billed separately from ordinary
input and cache reads. Provider-reported token counts determine charges.

## Manual Update
To manually update this file with the latest pricing data (if automation is unavailable):
```bash
curl -s https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json -o model_prices_and_context_window.json
```

## File Format
The file contains JSON data with model pricing information including:
- Model names and identifiers
- Input/output token costs
- Context window sizes
- Model capabilities

Pricing verification last updated: 2026-10-27 (the mirrored JSON is retained as the raw fallback catalog).
