// Package web is the HTTP layer: stdlib net/http routing, templ pages, and
// htmx. Handlers call only the domain service (domain.Service); they never
// touch SQL, the clock, or the email sender directly.
package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

const sessionCookie = "gt_session"

// Server wires the routes over the domain service.
type Server struct {
	svc *domain.Service
	mux *http.ServeMux
}

// NewServer builds a Server whose routes call svc.
func NewServer(svc *domain.Service) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux()}
	s.routes()
	return s
}

// ServeHTTP makes Server an http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("GET /signin", s.handleSignInForm)
	s.mux.HandleFunc("POST /signin", s.handleSignIn)
	s.mux.HandleFunc("POST /signout", s.handleSignOut)
	s.mux.HandleFunc("GET /goals", s.requireAuth(s.handleGoals))
	s.mux.HandleFunc("POST /goals", s.requireAuth(s.handleCreateGoal))
	s.mux.HandleFunc("GET /goals/{id}", s.requireAuth(s.handleViewGoal))
}

// currentAccount resolves the signed-in Account from the session cookie, or nil
// if there is none.
func (s *Server) currentAccount(r *http.Request) *domain.Account {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	id, err := strconv.ParseInt(c.Value, 10, 64)
	if err != nil {
		return nil
	}
	acc, err := s.svc.Account(r.Context(), id)
	if err != nil {
		return nil
	}
	return &acc
}

// requireAuth wraps a handler so it runs only for a signed-in Account, which it
// passes along via the request context.
func (s *Server) requireAuth(h func(http.ResponseWriter, *http.Request, domain.Account)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acc := s.currentAccount(r)
		if acc == nil {
			http.Redirect(w, r, "/signin", http.StatusSeeOther)
			return
		}
		h(w, r, *acc)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if s.currentAccount(r) != nil {
		http.Redirect(w, r, "/goals", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/signin", http.StatusSeeOther)
}

func (s *Server) handleSignInForm(w http.ResponseWriter, r *http.Request) {
	if s.currentAccount(r) != nil {
		http.Redirect(w, r, "/goals", http.StatusSeeOther)
		return
	}
	render(w, r, http.StatusOK, signInPage())
}

func (s *Server) handleSignIn(w http.ResponseWriter, r *http.Request) {
	emailAddr := r.FormValue("email")
	if emailAddr == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}
	acc, err := s.svc.SignIn(r.Context(), emailAddr)
	if err != nil {
		http.Error(w, "sign-in failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    strconv.FormatInt(acc.ID, 10),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/goals", http.StatusSeeOther)
}

func (s *Server) handleSignOut(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	http.Redirect(w, r, "/signin", http.StatusSeeOther)
}

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
	render(w, r, http.StatusOK, goalPage(&current, g))
}

// render writes a templ component with the given status.
func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = c.Render(r.Context(), w)
}
