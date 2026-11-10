package openclawworker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigureSpecifiesResponsesProtocol(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "patch.json")
	// Capture the exact patch sent to the cell, without contacting a gateway.
	if err := os.WriteFile(filepath.Join(dir, "podman"), []byte("#!/bin/sh\ncat > \"$CLAW_TEST_PATCH\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLAW_TEST_PATCH", capture)
	w := &Worker{}
	mapping := modelMapping{
		GatewayURL: "https://gateway.example/v1", APIKey: strings.Repeat("x", 32),
		DefaultModel: "test", Models: []modelAlias{{Alias: "test", Model: "test"}, {Alias: "second", Model: "second-model"}},
	}
	for range 2 {
		if err := w.configure(context.Background(), "u1-123456789abc", mapping); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(capture)
		if err != nil {
			t.Fatal(err)
		}
		var patch struct {
			Models struct {
				Providers map[string]struct {
					API    string `json:"api"`
					Models []struct {
						ID     string `json:"id"`
						Name   string `json:"name"`
						Compat struct {
							SupportsPromptCacheKey *bool `json:"supportsPromptCacheKey"`
						} `json:"compat"`
					} `json:"models"`
				} `json:"providers"`
			} `json:"models"`
		}
		if err := json.Unmarshal(data, &patch); err != nil {
			t.Fatal(err)
		}
		provider := patch.Models.Providers["sub2api"]
		if got := provider.API; got != "openai-responses" {
			t.Fatalf("provider protocol = %q, want openai-responses", got)
		}
		if len(provider.Models) != len(mapping.Models) {
			t.Fatalf("models = %d, want %d", len(provider.Models), len(mapping.Models))
		}
		for i, model := range provider.Models {
			if model.ID != mapping.Models[i].Model || model.Name != mapping.Models[i].Alias {
				t.Fatalf("model %d = %q/%q, want %q/%q", i, model.ID, model.Name, mapping.Models[i].Model, mapping.Models[i].Alias)
			}
			// Without this flag OpenClaw omits prompt_cache_key for non-OpenAI base URLs.
			if flag := model.Compat.SupportsPromptCacheKey; flag == nil || !*flag {
				t.Fatalf("model %q missing compat.supportsPromptCacheKey=true", model.ID)
			}
		}
	}
}
