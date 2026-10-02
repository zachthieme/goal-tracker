package web_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The toast is fixed to the bottom of the window, above the page content, and
// its message reads at WCAG AA's 4.5:1 on its fill in every theme (#83).
func TestToastSitsAtTheBottomAboveThePageInEveryTheme(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	toast := cssRule(t, css, "\n.toast")

	for property, want := range map[string]string{"position": "fixed", "bottom": "24px", "z-index": "30"} {
		if got := declValue(t, toast, property); got != want {
			t.Errorf(".toast %s = %q, want %q", property, got, want)
		}
	}
	themes := darkThemes(t, css)
	themes["light"] = tokens(tokenBlock(t, css, ":root"))
	for name, theme := range themes {
		ink := resolve(t, theme, declValue(t, toast, "color"))
		fill := resolve(t, theme, declValue(t, toast, "background"))
		if ratio := contrast(t, ink, fill); ratio < 4.5 {
			t.Errorf("%s: toast text %s on %s is %.2f:1, under 4.5:1", name, ink, fill, ratio)
		}
	}
}

// At phone width the toast spans the window less a 16px margin each side, so
// its message wraps rather than running off the screen.
func TestToastFitsAPhoneWidth(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	m := regexp.MustCompile(`@media \(max-width:600px\)\{\.toast\{([^}]*)\}`).FindStringSubmatch(css)
	if m == nil {
		t.Fatal("no phone-width .toast rule")
	}
	for _, want := range []string{"left:16px", "right:16px", "transform:none"} {
		if !strings.Contains(m[1], want) {
			t.Errorf("phone-width .toast rule %q lacks %s", m[1], want)
		}
	}
}
