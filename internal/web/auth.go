package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		if acc := s.currentAccount(r); acc != nil {
			r = s.withTopBarCounts(r, *acc)
		}
		s.notFound(w, r)
		return
	}
	if s.currentAccount(r) != nil {
		http.Redirect(w, r, "/home", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/signin", http.StatusSeeOther)
}

func (s *Server) handleSignInForm(w http.ResponseWriter, r *http.Request) {
	if s.currentAccount(r) != nil {
		http.Redirect(w, r, "/home", http.StatusSeeOther)
		return
	}
	render(w, r, http.StatusOK, signInPage(""))
}

func (s *Server) handleSignIn(w http.ResponseWriter, r *http.Request) {
	emailAddr := r.FormValue("email")
	if emailAddr == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}
	acc, err := s.svc.SignIn(r.Context(), emailAddr)
	if errors.Is(err, domain.ErrDeparted) {
		render(w, r, http.StatusForbidden, signInPage(emailAddr+" has been marked departed and can't sign in. Ask an Admin if this is a mistake."))
		return
	}
	if err != nil {
		s.serverError(w, r, "sign-in failed", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    strconv.FormatInt(acc.ID, 10),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/home", http.StatusSeeOther)
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
