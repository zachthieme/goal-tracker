package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"

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
	if s.oidc != nil {
		render(w, r, http.StatusOK, orgSignInPage(""))
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
		render(w, r, http.StatusForbidden, signInPage(departedSentence(emailAddr)))
		return
	}
	if err != nil {
		s.serverError(w, r, "sign-in failed", err)
		return
	}
	s.startSession(w, acc.ID)
	http.Redirect(w, r, "/home", http.StatusSeeOther)
}

// startSession signs accountID in on this browser: a signed session cookie.
func (s *Server) startSession(w http.ResponseWriter, accountID int64) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    s.sessions.encode(accountID),
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// departedSentence is what the sign-in page says to a Departed person.
func departedSentence(emailAddr string) string {
	return emailAddr + " has been marked departed and can't sign in. Ask an Admin if this is a mistake."
}

// handleSignOut ends the session. With the org's provider on, it also sends
// the browser to the provider to end its session there, so the next sign-in
// asks who the person is.
func (s *Server) handleSignOut(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	if to := s.providerSignOutURL(r); to != "" {
		http.SetCookie(w, &http.Cookie{Name: idTokenCookie, Path: "/signout", MaxAge: -1, HttpOnly: true, Secure: s.secureCookies, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, to, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/signin", http.StatusSeeOther)
}

// sessionCodec turns an Account ID into a session cookie value and back. The
// value is the ID and its HMAC-SHA256 under key, so a cookie can't be edited to
// name someone else.
type sessionCodec struct {
	key []byte
}

// encode is the cookie value that signs id in: "<id>.<signature>".
func (c sessionCodec) encode(id int64) string {
	payload := strconv.FormatInt(id, 10)
	return payload + "." + c.sign(payload)
}

// decode is the Account ID value carries, and whether its signature verifies.
// A value without one, like the bare IDs older versions issued, doesn't.
func (c sessionCodec) decode(value string) (int64, bool) {
	payload, sig, ok := strings.Cut(value, ".")
	if !ok || !c.verify(payload, sig) {
		return 0, false
	}
	id, err := strconv.ParseInt(payload, 10, 64)
	return id, err == nil
}

// verify reports whether sig is payload's signature.
func (c sessionCodec) verify(payload, sig string) bool {
	return hmac.Equal([]byte(sig), []byte(c.sign(payload)))
}

// sign is payload's signature under the key.
func (c sessionCodec) sign(payload string) string {
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
