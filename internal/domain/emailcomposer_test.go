package domain_test

import (
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// An Action Item's link always opens the publication it was raised on, at its
// Action Items, whether or not it is about a Goal: the one place it is listed
// with its Report.
func TestAppLinksActionItemOpensItsPublication(t *testing.T) {
	t.Parallel()

	links := domain.NewAppLinks("https://goals.example.com")
	for _, tc := range []struct {
		name string
		item domain.ActionItem
	}{
		{"about a Goal", domain.ActionItem{DefinitionID: 3, PublicationID: 7, GoalID: 11}},
		{"about the whole Report", domain.ActionItem{DefinitionID: 3, PublicationID: 7}},
	} {
		if got, want := links.ActionItem(tc.item), "https://goals.example.com/reports/3/publications/7#action-items"; got != want {
			t.Errorf("%s: ActionItem = %q, want %q", tc.name, got, want)
		}
	}
}

// A base URL given with a trailing slash still gives links with one slash
// between the base and the path.
func TestAppLinksTrimTheBaseURLsTrailingSlash(t *testing.T) {
	t.Parallel()

	links := domain.NewAppLinks("https://goals.example.com/")
	got := links.ActionItem(domain.ActionItem{DefinitionID: 3, PublicationID: 7, GoalID: 11})
	if want := "https://goals.example.com/reports/3/publications/7#action-items"; got != want {
		t.Errorf("ActionItem = %q, want %q", got, want)
	}
	if strings.Contains(strings.TrimPrefix(got, "https://"), "//") {
		t.Errorf("ActionItem = %q has a doubled slash", got)
	}
}

// With no base URL, links are relative to the web app.
func TestAppLinksWithNoBaseURLAreRelative(t *testing.T) {
	t.Parallel()

	links := domain.NewAppLinks("")
	if got, want := links.Checkin(11, true), "/goals/11/checkin?open=dates"; got != want {
		t.Errorf("Checkin = %q, want %q", got, want)
	}
	if got, want := links.SetPassword("tok"), "/set-password?token=tok"; got != want {
		t.Errorf("SetPassword = %q, want %q", got, want)
	}
}
