package web_test

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Check-in page carries the Highlight editor for script to upgrade its
// Highlight rows with: the editor's and an item's templates, a + button per
// Highlight kind that posts nothing, and the rows' container marked for the
// editor. The editor's Kind select, which Edit shows, offers every kind and
// None.
func TestCheckinPageCarriesTheHighlightEditor(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))

	editor := templateByID(t, page, "highlight-editor")
	var options []string
	for _, m := range regexp.MustCompile(`<option[^>]*>([^<]*)</option>`).FindAllStringSubmatch(editor, -1) {
		options = append(options, m[1])
	}
	if want := []string{"None", "Insight", "Accomplishment", "Miss"}; !slices.Equal(options, want) {
		t.Errorf("the editor's Kind select offers %q, want %q", options, want)
	}
	templateByID(t, page, "highlight-item")

	var kinds []string
	for _, tag := range regexp.MustCompile(`<button[^>]*data-testid="highlight-add"[^>]*>`).FindAllString(page, -1) {
		if attr(tag, "type") != "button" {
			t.Errorf("an add button would submit the form: %s", tag)
		}
		kinds = append(kinds, attr(tag, "data-kind"))
	}
	if want := []string{"Insight", "Accomplishment", "Miss"}; !slices.Equal(kinds, want) {
		t.Errorf("add buttons' kinds = %q, want %q", kinds, want)
	}

	highlights := tagAround(t, page, `data-testid="checkin-highlights"`)
	if !strings.Contains(highlights, " data-highlight-editor") {
		t.Errorf("the Highlight rows' container isn't marked for the editor: %s", highlights)
	}
}

// A row offered from a Draft Highlight carries who logged it, by name, for
// the editor's "Draft · logged by" line, and a form refused and rendered again
// still does.
func TestCheckinDraftHighlightRowCarriesWhoLoggedIt(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	draft := h.LogDraftHighlight(sam, goal.ID, domain.HighlightInsight, "Drills find gaps.")
	id := strconv.FormatInt(draft.ID, 10)
	field := `name="draft_highlight_logged_by_` + id + `"`
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	if by := tagAround(t, page, field); attr(by, "type") != "hidden" || attr(by, "value") != sam.Label() {
		t.Errorf("the Draft's row carries %s, want a hidden %q", by, sam.Label())
	}

	body, _ := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":                          {domain.HealthGreen},
		"draft_highlight_id":              {id},
		"draft_highlight_keep":            {id},
		"draft_highlight_kind_" + id:      {domain.HighlightInsight},
		"draft_highlight_note_" + id:      {"Drills find gaps."},
		"draft_highlight_logged_by_" + id: {sam.Label()},
	})
	if !strings.Contains(body, `data-testid="checkin-error"`) {
		t.Fatalf("a Check-in with no status wasn't refused; body:\n%s", body)
	}
	if by := tagAround(t, body, field); attr(by, "value") != sam.Label() {
		t.Errorf("the refused form's Draft row carries %s, want %q", by, sam.Label())
	}
}

// templateByID returns the <template> element with the id, up to its closing
// tag.
func templateByID(t *testing.T, page, id string) string {
	t.Helper()
	start := strings.Index(page, `<template id="`+id+`"`)
	if start < 0 {
		t.Fatalf("page has no <template id=%q>", id)
	}
	end := strings.Index(page[start:], "</template>")
	if end < 0 {
		t.Fatalf("<template id=%q> is not closed", id)
	}
	return page[start : start+end]
}
