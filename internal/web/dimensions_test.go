package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// An Admin defines a Dimension, adds a value, renames one, and retires one from
// the Dimensions page; a retired value stays listed, flagged retired.
func TestAdminManagesDimensionsOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")

	// Create a Dimension with two values.
	resp := postForm(t, boss, ts.URL+"/dimensions", url.Values{
		"name":   {"Pillar"},
		"values": {"Growth, Reliability"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create dimension: status %d", resp.StatusCode)
	}
	page := readBody(t, resp)
	if !strings.Contains(page, "Pillar") || !strings.Contains(page, "Growth") || !strings.Contains(page, "Reliability") {
		t.Fatalf("Dimensions page missing the new Dimension; body:\n%s", page)
	}

	dims, err := h.Service.ListDimensions(context.Background())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	dim := dims[0]
	var growth, reliability int64
	for _, v := range dim.Values {
		switch v.Value {
		case "Growth":
			growth = v.ID
		case "Reliability":
			reliability = v.ID
		}
	}

	// Add a value.
	postForm(t, boss, fmt.Sprintf("%s/dimensions/%d/values", ts.URL, dim.ID), url.Values{"value": {"Efficiency"}})
	// Rename a value.
	postForm(t, boss, fmt.Sprintf("%s/dimension-values/%d/rename", ts.URL, growth), url.Values{"value": {"Expansion"}})
	// Retire a value.
	postForm(t, boss, fmt.Sprintf("%s/dimension-values/%d/retire", ts.URL, reliability), url.Values{})

	page = getBody(t, boss, ts.URL+"/dimensions")
	for _, want := range []string{"Efficiency", "Expansion"} {
		if !strings.Contains(page, want) {
			t.Errorf("Dimensions page missing %q after edits; body:\n%s", want, page)
		}
	}
	if strings.Contains(page, "Growth") {
		t.Errorf("renamed-away value Growth still shown; body:\n%s", page)
	}
	if !strings.Contains(page, `data-testid="retired"`) {
		t.Errorf("retired value not flagged; body:\n%s", page)
	}
}

// A non-Admin can see Dimensions but gets no management controls, and the API
// refuses their attempt to create one.
func TestNonAdminCannotManageDimensions(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.CreateDimension(boss, "Pillar", "Growth")
	ts := newServer(t, h)

	sam := signInClient(t, ts.URL, "sam@example.com")
	page := getBody(t, sam, ts.URL+"/dimensions")
	if !strings.Contains(page, "Pillar") {
		t.Errorf("non-Admin cannot see Dimensions; body:\n%s", page)
	}
	if strings.Contains(page, `data-testid="create-dimension"`) {
		t.Errorf("non-Admin should not see the create-Dimension form; body:\n%s", page)
	}
	resp := postForm(t, sam, ts.URL+"/dimensions", url.Values{"name": {"Quarter"}, "values": {"Q1"}})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin create dimension: status %d, want 403", resp.StatusCode)
	}
}
