package web

import (
	"net/http"
	"strconv"
)

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
