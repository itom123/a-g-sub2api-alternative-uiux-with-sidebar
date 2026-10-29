package openclawworker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateExistingCellReconcilesInheritedFeatures(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "bootstrap-fails"}[fail], func(t *testing.T) {
			dir := t.TempDir()
			calls := filepath.Join(dir, "calls")
			binary := filepath.Join(dir, "fleet")
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$CLAW_BASELINE_CALLS"
if [ "$2" = list ]; then
  printf '%s\n' '{"cells":[{"tenant":"u1-123456789abc","state":"running","port":19100}]}'
elif [ "$2" = start ]; then
  exit "$CLAW_BASELINE_EXIT"
else
  exit 9
fi
`
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CLAW_BASELINE_CALLS", calls)
			t.Setenv("CLAW_BASELINE_EXIT", map[bool]string{false: "0", true: "7"}[fail])
			worker := &Worker{Binary: binary, MaxCells: 1}
			result, err := worker.execute(context.Background(), "u1-123456789abc", "create")
			if fail && err == nil {
				t.Fatal("incomplete bootstrap was reported as successful creation")
			}
			if !fail && (err != nil || result.State != "running" || result.Port != 19100) {
				t.Fatalf("unexpected existing-cell result: %+v, %v", result, err)
			}
			data, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "fleet start u1-123456789abc") || strings.Contains(string(data), "fleet create") {
				t.Fatalf("existing cell must reconcile, without duplicate allocation: %s", data)
			}
		})
	}
}
