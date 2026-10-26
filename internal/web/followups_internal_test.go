package web

import (
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// TestFollowUpChipsTakeTheirSignalsNameFromTheDefinitions checks that a chip
// showing its signal's plain name leads with the name followUpDefinitions
// gives it, so renaming a signal there renames its table chip too (#387).
func TestFollowUpChipsTakeTheirSignalsNameFromTheDefinitions(t *testing.T) {
	t.Parallel()
	today := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	g := domain.Goal{CadenceDays: 7}
	for _, kind := range []string{"asks-for-help", "checkin-overdue", "ownerless", "unlinked"} {
		name := followUpName(kind)
		if name == "" {
			t.Fatalf("followUpDefinitions has no %q", kind)
		}
		var b strings.Builder
		if err := followUpChip(today, g, domain.FollowUpSignal{Kind: kind, Days: 3}).Render(t.Context(), &b); err != nil {
			t.Fatalf("render %s chip: %v", kind, err)
		}
		label := b.String()
		open := strings.Index(label, "<button")
		if open < 0 {
			t.Fatalf("%s chip has no button:\n%s", kind, label)
		}
		text := label[strings.Index(label[open:], ">")+open+1:]
		if !strings.HasPrefix(text, name) {
			t.Errorf("%s chip's button reads %q, want it to start with its definition's name %q", kind, text[:min(len(text), 60)], name)
		}
	}
	if followUpName("no-such-signal") != "" {
		t.Error(`followUpName("no-such-signal") isn't empty`)
	}
}
