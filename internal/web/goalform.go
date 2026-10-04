package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// goalFormView is the New goal page: what was typed and, after a refused
// submit, each problem. A problem names the input it refuses by its form name
// — the name the domain's InputError carries — or none when it is the whole
// submit's.
type goalFormView struct {
	Title    string
	SoWhat   string
	Problems []*domain.InputError
}

// bad is why the named input was refused, or "" when it wasn't.
func (v goalFormView) bad(input string) string {
	for _, p := range v.Problems {
		if p.Input == input {
			return p.Message
		}
	}
	return ""
}

// handleNewGoalForm renders the New goal page, empty.
func (s *Server) handleNewGoalForm(w http.ResponseWriter, r *http.Request, current domain.Account) {
	render(w, r, http.StatusOK, goalFormPage(&current, goalFormView{}))
}

// handleCreateDefinedGoal submits the New goal page: the Goal is created as
// defined and the post lands on its page, which is the confirmation, so there
// is no toast. A refused submit comes back as the form, 422, as typed, with
// every problem listed at the top and marked on its input. A value the
// handler can't parse is refused the same way, under its input's name, in the
// same 422 as the domain's problems.
func (s *Server) handleCreateDefinedGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	v := goalFormView{Title: r.FormValue("title"), SoWhat: r.FormValue("so_what")}
	g, err := s.svc.CreateDefinedGoal(r.Context(), domain.DefinedGoalInput{
		Title:   v.Title,
		SoWhat:  v.SoWhat,
		OwnerID: current.ID,
	})
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			v.Problems = domain.InputErrors(err)
			if len(v.Problems) == 0 {
				v.Problems = []*domain.InputError{{Message: strings.TrimPrefix(err.Error(), domain.ErrValidation.Error()+": ")}}
			}
			render(w, r, http.StatusUnprocessableEntity, goalFormPage(&current, v))
			return
		}
		http.Error(w, "could not create goal", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/goals/%d", g.ID), http.StatusSeeOther)
}
