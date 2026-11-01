package web_test

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Import page's file input error sits on its own line in the danger ink,
// as the Goal form's input errors do (#676).
func TestImportFileErrorIsInTheDangerInkStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "admin@example.com")
	page := getBody(t, admin, ts.URL+"/imports")

	rule := cssRule(t, page, ".im-input-error")
	for _, want := range []string{"display:block", "font-size:var(--text-secondary)", "color:var(--color-danger-ink)"} {
		if !strings.Contains(rule, want) {
			t.Errorf(".im-input-error{%s} lacks %s", rule, want)
		}
	}
}

// On the Dimensions page, why Add value or a rename was refused sits on its
// own line under the input and its button, the row wrapping to hold it, so it
// doesn't squeeze the input (#702).
func TestDimensionsRowRefusalSitsUnderItsInputStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	postRefusedBack(t, client, fmt.Sprintf("%s/dimensions/%d/values", ts.URL, pillar.ID), url.Values{"value": {"Q1; Q2"}}, fmt.Sprintf("/dimensions#dimension-%d", pillar.ID))
	body := getBody(t, client, ts.URL+"/dimensions")
	form := findByTestID(t, parsePage(t, body), "add-dimension-value")
	reason := findByTestID(t, form, "form-error")
	if reason.Parent != form || !slices.Contains(strings.Fields(attrOf(reason, "class")), "alert") || !slices.Contains(strings.Fields(attrOf(form, "class")), "dm-row") {
		t.Fatalf("the reason isn't an .alert straight inside its .dm-row form:\n%s", renderNode(form))
	}
	if rule := cssRule(t, body, ".dm-row"); !strings.Contains(rule, "flex-wrap:wrap") {
		t.Errorf(".dm-row is %q, want it wrapping", rule)
	}
	if rule := cssRule(t, body, ".dm-row>.alert"); !strings.Contains(rule, "flex-basis:100%") {
		t.Errorf(".dm-row>.alert is %q, want it a line of its own", rule)
	}
}
