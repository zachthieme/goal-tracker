package email_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/email"
)

// The development sender's log line names the recipient by Account ID and
// the subject, never by their address (ticket #498).
func TestLogSenderNamesTheRecipientsAccountNotTheirAddress(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer
	sender := email.LogSender{Logger: slog.New(slog.NewTextHandler(&log, nil))}

	err := sender.Send(context.Background(), email.Message{
		To: "owner@example.com", AccountID: 42, Subject: "Comment on Launch in EU", Body: "Why?",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	line := strings.TrimSpace(log.String())
	for _, want := range []string{"account_id=42", `subject="Comment on Launch in EU"`} {
		if !strings.Contains(line, want) {
			t.Errorf("log %q does not mention %q", line, want)
		}
	}
	if strings.Contains(line, "@") {
		t.Errorf("log %q carries an email address", line)
	}
}
