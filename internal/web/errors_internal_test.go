package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A context.Canceled failure on a request whose own context is still live
// isn't a client abort; something the server started was cancelled. It is
// logged at Error with its cause, like any other server error.
func TestACancelledFailureOnALiveRequestIsLoggedAtError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	s := &Server{log: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	r := httptest.NewRequest(http.MethodPost, "/goals/new/checklist", nil)
	s.logServerError(r, fmt.Errorf("load fields: %w", context.Canceled))

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("want one log record, got %q: %v", buf.String(), err)
	}
	if rec["level"] != "ERROR" || rec["msg"] != "server error" || rec["method"] != http.MethodPost || rec["path"] != "/goals/new/checklist" {
		t.Errorf("record %v, want a server error for POST /goals/new/checklist", rec)
	}
	if cause, _ := rec["err"].(string); !strings.Contains(cause, "load fields: context canceled") {
		t.Errorf("record err %q, want the cancelled load's error", cause)
	}
}
