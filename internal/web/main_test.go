package web_test

import (
	"log/slog"
	"os"
	"testing"
)

// TestMain discards what a Server built without WithLogger logs, so the
// access line every test request gets doesn't flood the test output. A test
// that reads the log gives its Server a logger with WithLogger, or swaps the
// default for its own run.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.DiscardHandler))
	os.Exit(m.Run())
}
