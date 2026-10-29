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
		DefaultModel: "test", Models: []modelAlias{{Alias: "test", Model: "test"}},
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
					API string `json:"api"`
				} `json:"providers"`
			} `json:"models"`
		}
		if err := json.Unmarshal(data, &patch); err != nil {
			t.Fatal(err)
		}
		if got := patch.Models.Providers["sub2api"].API; got != "openai-responses" {
			t.Fatalf("provider protocol = %q, want openai-responses", got)
		}
	}
}
