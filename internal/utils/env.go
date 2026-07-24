package utils

import "os"

// IsTestEnv reports whether the process runs against the test environment.
// Test-only shortcuts (auto-verified emails, skipped outgoing mail) are gated
// on this and must never trigger in development or production.
func IsTestEnv() bool {
	return os.Getenv("ENV") == "test"
}
