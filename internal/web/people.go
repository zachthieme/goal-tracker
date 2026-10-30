package web

import (
	"cmp"
	"net/http"
	"slices"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// personSearchLimit is the most matches the person picker's search answers.
const personSearchLimit = 10

// handleSearchPeople answers the person picker's search: the people whose
// Name or email holds q, ignoring case, by Label, then email, at most
// personSearchLimit of them. A Departed person is never offered, since they
// can't act (CONTEXT.md: Departed), and a blank q answers nothing.
func (s *Server) handleSearchPeople(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if q == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	accounts, err := s.svc.Accounts(r.Context())
	if err != nil {
		s.serverError(w, r, "could not search people", err)
		return
	}
	var found []domain.Account
	for _, a := range accounts {
		if !a.Departed && (strings.Contains(strings.ToLower(a.Name), q) || strings.Contains(strings.ToLower(a.Email), q)) {
			found = append(found, a)
		}
	}
	slices.SortFunc(found, func(a, b domain.Account) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Label()), strings.ToLower(b.Label())), cmp.Compare(a.Email, b.Email))
	})
	render(w, r, http.StatusOK, personSearchResults(found[:min(len(found), personSearchLimit)]))
}
