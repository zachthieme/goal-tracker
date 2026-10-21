package web_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The light theme takes the flat frame's tokens (DESIGN.md § Tokens): one white
// canvas, a grey sidebar with its hover and current fills, and neutral inks
// and hairlines, so teal marks only what a person can act on. The --nav-*
// tokens of the teal nav bar the sidebar replaced are gone, in every theme.
func TestLightTokensAreTheFlatFrames(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	light := tokens(tokenBlock(t, css, ":root"))

	for token, want := range map[string]string{
		"--color-canvas":        "#FFFFFF",
		"--color-sidebar":       "#F7F7F8",
		"--color-sidebar-hover": "#EEEEF0",
		"--color-sidebar-on":    "#E8E8EB",
		"--color-row-hover":     "#F6F6F7",
		"--color-group":         "#FAFAFB",
		"--color-border":        "#EDEDEF",
		"--color-border-strong": "#8A8D93",
		"--color-ink":           "#1B1D22",
		"--color-ink-2":         "#33363C",
		"--color-ink-muted":     "#5F636B",
		"--color-chip-bg":       "#F1F1F3",
		"--color-chip-ink":      "#4A4D54",
		"--health-r-mark":       "#C62828",
		"--health-y-mark":       "#B27B00",
		"--health-g-mark":       "#2E7D4F",
		"--radius-md":           "6px",
		"--radius-lg":           "10px",
	} {
		if got := resolve(t, light, "var("+token+")"); got != want {
			t.Errorf("light %s is %s, want %s", token, got, want)
		}
	}
	themes := darkThemes(t, css)
	themes["light"] = light
	for name, theme := range themes {
		for token := range theme {
			if strings.HasPrefix(token, "--nav-") || token == "--font-display" || token == "--font-mono" {
				t.Errorf("%s still sets %s", name, token)
			}
		}
	}
}

// frameSurfaces are the surfaces text and control edges sit on: the canvas,
// the sidebar, a hovered row and a group header, then the surfaces pages that
// haven't moved to the flat frame still use — cards, a hovered card, panels
// and table heads.
var frameSurfaces = []string{
	"--color-canvas", "--color-sidebar", "--color-row-hover", "--color-group",
	"--color-surface", "--color-surface-hover", "--color-surface-alt",
}

// In every theme the three inks are neutral, with no teal tint, and each reaches
// WCAG AA's 4.5:1 on every surface text sits on, the selected-row fill
// included.
func TestInkIsNeutralAndReachesAAOnEverySurface(t *testing.T) {
	t.Parallel()

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
			for _, surface := range append(frameSurfaces, "--color-primary-light") {
				bg := resolve(t, theme, "var("+surface+")")
				if ratio := contrast(t, fg, bg); ratio < 4.5 {
					t.Errorf("%s: %s %s on %s %s is %.2f:1, under 4.5:1", name, ink, fg, surface, bg, ratio)
				}
			}
		}
	}
}

// In every theme the canvas and the surfaces beside it are neutral greys, so
// Health's colours are the only saturated hues on the page. In light the
// canvas is white, and the sidebar, a hovered row and a group header each sit
// a step off it.
func TestCanvasAndItsSurfacesAreNeutral(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	themes := darkThemes(t, css)
	light := tokens(tokenBlock(t, css, ":root"))
	themes["light"] = light
	for name, theme := range themes {
		for _, token := range append(frameSurfaces, "--color-sidebar-hover", "--color-sidebar-on", "--color-border") {
			hex := resolve(t, theme, "var("+token+")")
			if spread := channelSpread(t, hex); spread > 8 {
				t.Errorf("%s %s %s is tinted (its channels differ by %d), want a neutral grey", name, token, hex, spread)
			}
		}
	}
	canvas := resolve(t, light, "var(--color-canvas)")
	for _, token := range []string{"--color-sidebar", "--color-row-hover", "--color-group"} {
		if hex := resolve(t, light, "var("+token+")"); luminance(t, hex) >= luminance(t, canvas) {
			t.Errorf("light %s %s does not sit a step off the canvas %s", token, hex, canvas)
		}
	}
}

// In every theme, on the canvas and every surface beside it, link and
// destructive text reach WCAG AA's 4.5:1, and an input's edge, the focus ring
// and each Health mark reach the 3:1 that marks a control or a shape.
func TestTextEdgesAndMarksHoldOnEverySurface(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	themes := darkThemes(t, css)
	themes["light"] = tokens(tokenBlock(t, css, ":root"))
	for name, theme := range themes {
		for _, mark := range []struct {
			token string
			want  float64
		}{
			{"--color-primary-strong", 4.5},
			{"--color-danger-ink", 4.5},
			{"--color-border-strong", 3},
			{"--color-focus", 3},
			{"--health-r-mark", 3},
			{"--health-y-mark", 3},
			{"--health-g-mark", 3},
		} {
			fg := resolve(t, theme, "var("+mark.token+")")
			for _, surface := range frameSurfaces {
				bg := resolve(t, theme, "var("+surface+")")
				if ratio := contrast(t, fg, bg); ratio < mark.want {
					t.Errorf("%s: %s %s on %s %s is %.2f:1, under %.1f:1", name, mark.token, fg, surface, bg, ratio, mark.want)
				}
			}
		}
	}
}

// In every theme a nav count is muted tabular figures that read on the
// sidebar, hovered or current, and Home's count, because it means something
// needs the person, is a filled pill whose number reads on it and whose fill
// stands out from the sidebar.
func TestNavCountsReadOnTheSidebar(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	themes := darkThemes(t, css)
	themes["light"] = tokens(tokenBlock(t, css, ":root"))
	for name, theme := range themes {
		plain := computed(t, css, theme, ".count")
		needs := computed(t, css, theme, ".count", ".count.needs-you")
		if plain["font-variant-numeric"] != "tabular-nums" {
			t.Errorf("%s: a nav count isn't in tabular figures: %v", name, plain)
		}
		if _, filled := plain["background"]; filled {
			t.Errorf("%s: a plain nav count is a pill: %v", name, plain)
		}
		if ratio := contrast(t, needs["color"], needs["background"]); ratio < 4.5 {
			t.Errorf("%s: Home's count %s on its pill %s is %.2f:1, under 4.5:1", name, needs["color"], needs["background"], ratio)
		}
		for _, surface := range []string{"--color-sidebar", "--color-sidebar-hover", "--color-sidebar-on"} {
			bg := resolve(t, theme, "var("+surface+")")
			if ratio := contrast(t, plain["color"], bg); ratio < 4.5 {
				t.Errorf("%s: a nav count %s on %s %s is %.2f:1, under 4.5:1", name, plain["color"], surface, bg, ratio)
			}
			if ratio := contrast(t, needs["background"], bg); ratio < 3 {
				t.Errorf("%s: Home's count pill %s on %s %s is %.2f:1, under 3:1", name, needs["background"], surface, bg, ratio)
			}
		}
	}
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
