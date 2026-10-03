package web_test

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// DESIGN.md § System: everything on screen aligns to a 4px grid. The shared
// stylesheet's spacing — every gap, padding and margin — is a multiple of 4px,
// bar the 1px optical padding on badges.
func TestStylesheetSpacingSitsOnTheGrid(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	for _, decl := range offGridSpacing(css) {
		t.Errorf("app.css spacing is off the 4px grid: %s", decl)
	}
}

// Each page's own style block keeps to the same grid. The Report Print view is
// a paper document that DESIGN.md § Print variant exempts, so its block is
// left out.
func TestPageStyleSpacingSitsOnTheGrid(t *testing.T) {
	t.Parallel()

	sources, err := filepath.Glob("*.templ")
	if err != nil || len(sources) == 0 {
		t.Fatalf("no templates found: %v", err)
	}
	for _, source := range sources {
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		for _, block := range screenStyleBlocks(string(raw)) {
			for _, decl := range offGridSpacing(block) {
				t.Errorf("%s spacing is off the 4px grid: %s", source, decl)
			}
		}
	}
}

// screenStyleBlocks returns the body of every style block in a template
// source, bar the one inside the Report Print view.
func screenStyleBlocks(source string) []string {
	var blocks []string
	for _, component := range strings.Split(source, "\ntempl ") {
		if strings.HasPrefix(component, "publicationPrintPage(") {
			continue
		}
		for {
			_, after, ok := strings.Cut(component, "<style>")
			if !ok {
				break
			}
			block, rest, _ := strings.Cut(after, "</style>")
			blocks = append(blocks, block)
			component = rest
		}
	}
	return blocks
}

var (
	spacingDecl = regexp.MustCompile(`(?:^|[;{\s])((?:row-|column-)?(?:gap|padding|margin)(?:-[a-z-]+)?)\s*:\s*([^;}]*)`)
	length      = regexp.MustCompile(`(-?\d+(?:\.\d+)?)([a-z%]+)`)
)

// offGridSpacing returns every gap, padding or margin declaration in css whose
// lengths are not whole multiples of 4px, each with the selector it sits under.
// A badge's 1px vertical padding is an optical adjustment and passes.
func offGridSpacing(css string) []string {
	var off []string
	for _, rule := range strings.Split(css, "}") {
		// A media block's query opens a brace before its first rule's, so
		// the declarations follow the last opening brace and the selector
		// sits between it and the one before.
		open := strings.LastIndex(rule, "{")
		if open < 0 {
			continue
		}
		selectors, decls := rule[:open], rule[open+1:]
		if at := strings.LastIndex(selectors, "{"); at >= 0 {
			selectors = selectors[at+1:]
		}
		selectors = strings.TrimSpace(selectors)
		for _, m := range spacingDecl.FindAllStringSubmatch(decls, -1) {
			prop, value := m[1], strings.TrimSpace(m[2])
			if prop == "padding" && strings.Contains(selectors, "badge") && strings.HasPrefix(value, "1px ") {
				value = strings.TrimPrefix(value, "1px ")
			}
			for _, l := range length.FindAllStringSubmatch(value, -1) {
				n, err := strconv.ParseFloat(l[1], 64)
				if err != nil || l[2] != "px" || int(n)%4 != 0 || n != float64(int(n)) {
					off = append(off, selectors+"{"+prop+":"+value+"}")
					break
				}
			}
		}
	}
	return off
}
