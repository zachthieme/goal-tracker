package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

func (s *Server) handleGoals(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goals, err := s.svc.ListGoals(r.Context())
	if err != nil {
		http.Error(w, "could not list goals", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, goalsPage(&current, goals))
}

func (s *Server) handleCreateGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	_, err := s.svc.CreateGoal(r.Context(), domain.CreateGoalInput{
		Title:   r.FormValue("title"),
		SoWhat:  r.FormValue("so_what"),
		OwnerID: current.ID,
	})
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		http.Error(w, "could not create goal", http.StatusInternalServerError)
		return
	}

	// htmx swaps the Goal list in place; a plain form post reloads the page.
	if r.Header.Get("HX-Request") == "true" {
		goals, err := s.svc.ListGoals(r.Context())
		if err != nil {
			http.Error(w, "could not list goals", http.StatusInternalServerError)
			return
		}
		render(w, r, http.StatusOK, goalList(goals))
		return
	}
	http.Redirect(w, r, "/goals", http.StatusSeeOther)
}

func (s *Server) handleViewGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	g, err := s.svc.ViewGoal(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load goal", http.StatusInternalServerError)
		return
	}

	parents, err := s.svc.ParentLinks(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load parents", http.StatusInternalServerError)
		return
	}
	children, err := s.svc.ChildLinks(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load children", http.StatusInternalServerError)
		return
	}
	// Candidate parents to contribute to: every other Goal. The domain rejects
	// self-links, duplicates, and cycles when the request is actually made.
	all, err := s.svc.ListGoals(r.Context())
	if err != nil {
		http.Error(w, "could not load goals", http.StatusInternalServerError)
		return
	}
	candidates := make([]domain.Goal, 0, len(all))
	for _, c := range all {
		if c.ID != g.ID {
			candidates = append(candidates, c)
		}
	}

	render(w, r, http.StatusOK, goalPage(&current, g, parents, children, candidates))
}
