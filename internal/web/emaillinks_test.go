package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// Every link an email carries (domain.AppLinks) opens a page the web app
// serves: its path, without the base URL, query or fragment, matches a route
// registered for GET rather than falling through to the index or the
// catch-all. The table names every AppLinks method, so a new link fails here
// until it is listed and checked.
func TestEveryEmailLinkReachesARegisteredRoute(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	srv := web.NewServer(h.Service, web.WithPasswordSignIn())
	links := domain.NewAppLinks("https://goals.example.com")
	item := domain.ActionItem{DefinitionID: 3, PublicationID: 7, GoalID: 11}

	cases := map[string]string{
		"Goal":            links.Goal(11),
		"GoalComments":    links.GoalComments(11),
		"Checkin":         links.Checkin(11, true),
		"PublicationGoal": links.PublicationGoal(3, 7, 11),
		"ActionItem":      links.ActionItem(item),
		"PendingLinks":    links.PendingLinks(),
		"SetPassword":     links.SetPassword("tok"),
	}

	typ := reflect.TypeOf(domain.AppLinks{})
	var methods []string
	for i := range typ.NumMethod() {
		methods = append(methods, typ.Method(i).Name)
	}
	for _, m := range methods {
		if _, ok := cases[m]; !ok {
			t.Errorf("AppLinks.%s is not checked: add it to this test's table", m)
		}
	}
	for name := range cases {
		if !slices.Contains(methods, name) {
			t.Errorf("the table checks %s, which is not an AppLinks method", name)
		}
	}

	for name, link := range cases {
		u, err := url.Parse(link)
		if err != nil {
			t.Errorf("%s: %q does not parse: %v", name, link, err)
			continue
		}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, u.Path, nil)
		switch pattern := srv.RoutePattern(req); pattern {
		case "", "/", "GET /":
			t.Errorf("%s: GET %s (from %s) matches %q, not a page's route", name, u.Path, link, pattern)
		}
	}
}
