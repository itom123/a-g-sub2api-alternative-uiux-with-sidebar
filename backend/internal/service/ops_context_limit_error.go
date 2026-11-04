package service

import "strings"

// IsOpsContextLimitErrorMessage reports whether an error message says the
// request did not fit the model's context window (OpenAI "exceeds the context
// window", Anthropic "prompt is too long: N tokens > M maximum"). Such requests
// fail because of their own size: retrying them can never succeed, so ops
// attributes them to the client and keeps them out of SLA and provider error
// rates.
func IsOpsContextLimitErrorMessage(message string) bool {
	if isOpenAIContextWindowError(message, nil) {
		return true
	}
	return strings.Contains(strings.ToLower(message), "prompt is too long")
}
