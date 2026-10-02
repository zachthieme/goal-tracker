package web_test

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// Text is neutral ink so that teal marks only what a person can act on. The
// light theme's headings, body and secondary text take the UX Review's values,
// and the top bar keeps its deep teal though --color-ink no longer is (#85).
func TestLightInkIsNeutralAndTheTopBarStaysTeal(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	light := tokens(tokenBlock(t, css, ":root"))

	for token, want := range map[string]string{
		"--color-ink":       "#18211F",
		"--color-ink-2":     "#3E4946",
		"--color-ink-muted": "#66706D",
		"--nav-bg":          "#134E4A",
	} {
		if got := resolve(t, light, "var("+token+")"); got != want {
			t.Errorf("light %s is %s, want %s", token, got, want)
		}
	}
}

// In every theme the three inks are neutral, with no teal tint, and each reaches
// WCAG AA's 4.5:1 on every surface text sits on: the canvas, cards, a hovered
// card, panels and table heads, and the selected-row fill.
func TestInkIsNeutralAndReachesAAOnEverySurface(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	themes := darkThemes(t, css)
	themes["light"] = tokens(tokenBlock(t, css, ":root"))
	for name, theme := range themes {
		for _, ink := range []string{"--color-ink", "--color-ink-2", "--color-ink-muted"} {
			fg := resolve(t, theme, "var("+ink+")")
			if spread := channelSpread(t, fg); spread > 12 {
				t.Errorf("%s: %s %s is tinted (its channels differ by %d), want a neutral grey", name, ink, fg, spread)
			}
			for _, surface := range []string{"--color-canvas", "--color-surface", "--color-surface-hover", "--color-surface-alt", "--color-primary-light"} {
				bg := resolve(t, theme, "var("+surface+")")
				if ratio := contrast(t, fg, bg); ratio < 4.5 {
					t.Errorf("%s: %s %s on %s %s is %.2f:1, under 4.5:1", name, ink, fg, surface, bg, ratio)
				}
			}
		}
	}
}

// In every theme the top bar's counts come in two pills: Home's is dark,
// because it means something needs the person, and every other count is a
// neutral grey. Each number reaches 4.5:1 on its pill, and each pill's edge —
// its fill, or its ring where it has one — reaches WCAG's 3:1 for a visible
// shape against the top bar.
func TestNavCountPillsReadAndStandOutOnTheTopBar(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	themes := darkThemes(t, css)
	themes["light"] = tokens(tokenBlock(t, css, ":root"))
	for name, theme := range themes {
		bar := resolve(t, theme, "var(--nav-bg)")
		grey := computed(t, css, theme, ".count")
		needs := computed(t, css, theme, ".count", ".count.needs-you")
		if spread := channelSpread(t, grey["background"]); spread > 12 {
			t.Errorf("%s: a nav count's pill %s is tinted (its channels differ by %d), want a neutral grey", name, grey["background"], spread)
		}
		if luminance(t, needs["background"]) >= luminance(t, grey["background"]) {
			t.Errorf("%s: Home's count pill %s is not darker than the other counts' %s", name, needs["background"], grey["background"])
		}
		for pill, style := range map[string]map[string]string{"a nav count": grey, "Home's count": needs} {
			if ratio := contrast(t, style["color"], style["background"]); ratio < 4.5 {
				t.Errorf("%s: %s's number %s on its pill %s is %.2f:1, under 4.5:1", name, pill, style["color"], style["background"], ratio)
			}
			edge := style["background"]
			if ring := ringColour(t, theme, style["box-shadow"]); ring != "" {
				edge = ring
			}
			if ratio := contrast(t, edge, bar); ratio < 3 {
				t.Errorf("%s: %s's pill edge %s on the top bar %s is %.2f:1, under 3:1", name, pill, edge, bar, ratio)
			}
		}
	}
}

// ringColour resolves the colour of a "0 0 0 <width> <colour>" ring shadow, ""
// when there is none.
func ringColour(t *testing.T, theme map[string]string, shadow string) string {
	t.Helper()
	if shadow == "" || shadow == "none" {
		return ""
	}
	m := regexp.MustCompile(`^0 0 0 \d+px (\S+)$`).FindStringSubmatch(shadow)
	if m == nil {
		t.Fatalf("box-shadow %q is not a ring", shadow)
	}
	return resolve(t, theme, m[1])
}

// channelSpread is how far apart a #RRGGBB colour's channels are: 0 for a pure
// grey, larger the more it leans toward a hue.
func channelSpread(t *testing.T, hex string) int {
	t.Helper()
	var r, g, b int
	if n, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); n != 3 || err != nil {
		t.Fatalf("%q is not a #RRGGBB colour", hex)
	}
	return max(r, g, b) - min(r, g, b)
}
