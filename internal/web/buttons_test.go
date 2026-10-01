package web_test

import (
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// In the dark theme a hovered primary button's fill goes lighter: dark themes
// show elevation by lightness. That holds whether the theme comes from the OS
// or from data-theme="dark".
func TestDarkPrimaryButtonHoverLightens(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	hover := cssRule(t, css, "\n.btn.primary:hover")

	for name, theme := range darkThemes(t, css) {
		if fill := resolve(t, theme, declValue(t, hover, "background")); fill != "#2DD4BF" {
			t.Errorf("%s: a hovered primary button's fill is %s, want the lighter --color-primary-hover #2DD4BF", name, fill)
		}
	}
}

// A hovered primary button's text reaches WCAG AA's 4.5:1 on its fill in
// every theme.
func TestPrimaryButtonHoverTextReachesAA(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	hover := cssRule(t, css, "\n.btn.primary:hover")

	themes := darkThemes(t, css)
	themes["light"] = tokens(tokenBlock(t, css, ":root"))
	for name, theme := range themes {
		ink := resolve(t, theme, declValue(t, hover, "color"))
		fill := resolve(t, theme, declValue(t, hover, "background"))
		if ratio := contrast(t, ink, fill); ratio < 4.5 {
			t.Errorf("%s: hovered primary button text %s on %s is %.2f:1, under 4.5:1", name, ink, fill, ratio)
		}
	}
}

// A hovered primary button shows it by its fill alone, in every theme: no lift,
// no halo, and the border keeps matching the fill so it reads as one surface.
func TestPrimaryButtonHoverChangesFillOnly(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	themes := darkThemes(t, css)
	themes["light"] = tokens(tokenBlock(t, css, ":root"))
	for name, theme := range themes {
		rest := computed(t, css, theme, ".btn", ".btn.primary")
		hover := computed(t, css, theme, ".btn", ".btn.primary", ".btn:hover", ".btn.primary:hover")
		if got := changed(rest, hover); fmt.Sprint(got) != "[background border-color]" {
			t.Errorf("%s: a hovered primary button changes %v, want only its fill", name, got)
		}
		for state, style := range map[string]map[string]string{"resting": rest, "hovered": hover} {
			if style["border-color"] != style["background"] {
				t.Errorf("%s: a %s primary button's border %s doesn't match its fill %s", name, state, style["border-color"], style["background"])
			}
		}
	}
	if fill := resolve(t, themes["light"], "var(--btn-primary-hover)"); fill != "#115E59" {
		t.Errorf("light: a hovered primary button's fill is %s, want the darker Teal 800 #115E59", fill)
	}
}

// A hovered clickable card shows it by a deeper shadow alone, in every theme.
func TestClickableCardHoverChangesShadowOnly(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	themes := darkThemes(t, css)
	themes["light"] = tokens(tokenBlock(t, css, ":root"))
	for name, theme := range themes {
		rest := computed(t, css, theme, ".card", "a.card")
		hover := computed(t, css, theme, ".card", "a.card", "a.card:hover")
		if got := changed(rest, hover); fmt.Sprint(got) != "[box-shadow]" {
			t.Errorf("%s: a hovered clickable card changes %v, want only its shadow", name, got)
		}
		if want := resolve(t, theme, "var(--shadow-card-hover)"); hover["box-shadow"] != want {
			t.Errorf("%s: a hovered clickable card's shadow is %s, want --shadow-card-hover %s", name, hover["box-shadow"], want)
		}
	}
}

// computed cascades the top-level rules for selectors, in order, and resolves
// each value against theme. Properties none of them set are absent.
func computed(t *testing.T, css string, theme map[string]string, selectors ...string) map[string]string {
	t.Helper()
	style := map[string]string{}
	for _, sel := range selectors {
		for _, d := range strings.Split(cssRule(t, css, "\n"+sel), ";") {
			if p, v, ok := strings.Cut(d, ":"); ok {
				style[strings.TrimSpace(p)] = resolve(t, theme, strings.TrimSpace(v))
			}
		}
	}
	return style
}

// changed lists, sorted, the properties whose value differs between before and
// after. A property one side leaves unset counts as its initial value.
func changed(before, after map[string]string) []string {
	initial := map[string]string{"transform": "none", "box-shadow": "none", "text-decoration": "none"}
	value := func(style map[string]string, p string) string {
		if v, ok := style[p]; ok {
			return v
		}
		return initial[p]
	}
	var diff []string
	for p := range after {
		if value(before, p) != value(after, p) {
			diff = append(diff, p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok && value(before, p) != value(after, p) {
			diff = append(diff, p)
		}
	}
	sort.Strings(diff)
	return diff
}

// The OS dark block and the data-theme="dark" block set the same tokens to the
// same values, so the dark theme looks the same however it is chosen.
func TestDarkThemeBlocksAreIdentical(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	media, ok := ruleFor(mediaBlock(t, css, "@media (prefers-color-scheme: dark) "), `:root:not([data-theme="light"])`)
	if !ok {
		t.Fatalf("the OS dark block sets no tokens on :root")
	}
	fromOS, pinned := tokens(media), tokens(tokenBlock(t, css, `:root[data-theme="dark"]`))
	for k, v := range fromOS {
		if pinned[k] != v {
			t.Errorf("%s is %q in the OS dark block but %q under data-theme=\"dark\"", k, v, pinned[k])
		}
	}
	for k, v := range pinned {
		if _, ok := fromOS[k]; !ok {
			t.Errorf("%s is %q under data-theme=\"dark\" but missing from the OS dark block", k, v)
		}
	}
}

// contrast returns the WCAG contrast ratio of two #RRGGBB colours.
func contrast(t *testing.T, a, b string) float64 {
	t.Helper()
	la, lb := luminance(t, a), luminance(t, b)
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}

// luminance returns the WCAG relative luminance of a #RRGGBB colour.
func luminance(t *testing.T, hex string) float64 {
	t.Helper()
	var r, g, b int
	if n, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); n != 3 || err != nil {
		t.Fatalf("%q is not a #RRGGBB colour", hex)
	}
	channel := func(c int) float64 {
		s := float64(c) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}

// darkThemes returns the token values in force under each way of getting the
// dark theme: the light :root tokens overlaid with each dark block.
func darkThemes(t *testing.T, css string) map[string]map[string]string {
	t.Helper()
	light := tokens(tokenBlock(t, css, ":root"))
	media, ok := ruleFor(mediaBlock(t, css, "@media (prefers-color-scheme: dark) "), `:root:not([data-theme="light"])`)
	if !ok {
		t.Fatalf("the OS dark block sets no tokens on :root")
	}
	themes := map[string]map[string]string{}
	for name, block := range map[string]string{
		"OS dark":           media,
		`data-theme="dark"`: tokenBlock(t, css, `:root[data-theme="dark"]`),
	} {
		theme := map[string]string{}
		for k, v := range light {
			theme[k] = v
		}
		for k, v := range tokens(block) {
			theme[k] = v
		}
		themes[name] = theme
	}
	return themes
}

// tokenBlock returns the declarations of the top-level rule for selector.
func tokenBlock(t *testing.T, css, selector string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(selector) + `\s*\{([^}]*)\}`).FindStringSubmatch(css)
	if m == nil {
		t.Fatalf("stylesheet has no top-level %s rule", selector)
	}
	return m[1]
}

// tokens returns the custom properties a rule's declarations set.
func tokens(decls string) map[string]string {
	m := map[string]string{}
	for _, d := range regexp.MustCompile(`(--[\w-]+)\s*:\s*([^;]+);`).FindAllStringSubmatch(stripComments(decls), -1) {
		m[d[1]] = strings.TrimSpace(d[2])
	}
	return m
}

func stripComments(css string) string {
	return regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
}

// declValue returns the value decls gives property.
func declValue(t *testing.T, decls, property string) string {
	t.Helper()
	for _, d := range strings.Split(decls, ";") {
		if p, v, ok := strings.Cut(d, ":"); ok && strings.TrimSpace(p) == property {
			return strings.TrimSpace(v)
		}
	}
	t.Fatalf("no %s in %s", property, decls)
	return ""
}

// resolve follows value through var() references in theme to a literal.
func resolve(t *testing.T, theme map[string]string, value string) string {
	t.Helper()
	ref := regexp.MustCompile(`^var\((--[\w-]+)\)$`)
	for range 10 {
		m := ref.FindStringSubmatch(value)
		if m == nil {
			return value
		}
		next, ok := theme[m[1]]
		if !ok {
			t.Fatalf("token %s is not set", m[1])
		}
		value = next
	}
	t.Fatalf("token chain for %s does not end", value)
	return ""
}
