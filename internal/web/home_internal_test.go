package web

import (
	"encoding/json"
	"html"
	"strings"
	"testing"
)

// TestConfirmScriptAsksAnyMessageWordForWord: whatever the message holds —
// apostrophes, double quotes, a backslash, markup — the onsubmit script stays
// inside its attribute and, once the browser decodes it, confirms exactly that
// message.
func TestConfirmScriptAsksAnyMessageWordForWord(t *testing.T) {
	t.Parallel()

	if !strings.Contains(confirmMerge, "can't") {
		t.Fatalf("confirmMerge no longer has its straight apostrophe, so it no longer covers the regression: %q", confirmMerge)
	}
	for _, msg := range []string{
		confirmMerge,
		confirmRemoveAdmin,
		"it's",
		`say "hi"`,
		`a \ backslash`,
		"</script><script>alert(1)</script>",
		"Goals & Metrics",
		"this person’s role",
	} {
		call := confirmScript(msg).Call
		if strings.ContainsAny(call, `"'<>`) {
			t.Errorf("confirmScript(%q).Call could end its attribute or tag: %s", msg, call)
		}
		script := html.UnescapeString(call)
		lit, ok := strings.CutPrefix(script, "return confirm(")
		if ok {
			lit, ok = strings.CutSuffix(lit, ")")
		}
		if !ok {
			t.Errorf("confirmScript(%q) decodes to %s, not a call of confirm", msg, script)
			continue
		}
		var got string
		if err := json.Unmarshal([]byte(lit), &got); err != nil {
			t.Errorf("confirmScript(%q) passes confirm %s, not a string literal: %v", msg, lit, err)
			continue
		}
		if got != msg {
			t.Errorf("confirmScript(%q) confirms %q", msg, got)
		}
	}
}
