package geminicli

import (
	"os"
	"testing"
)

// The built-in Gemini CLI client is not embedded in this repository; tests use
// a dummy client ID to simulate operator-provided configuration.
func TestMain(m *testing.M) {
	if GeminiCLIOAuthClientID == "" {
		GeminiCLIOAuthClientID = "test-built-in-client-id.apps.googleusercontent.com"
	}
	os.Exit(m.Run())
}
