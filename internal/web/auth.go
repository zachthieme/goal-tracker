package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		if acc := s.currentAccount(r); acc != nil {
			r = s.withSidebar(r, *acc)
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
	switch {
	case s.oidc != nil:
		render(w, r, http.StatusOK, orgSignInPage(""))
	case s.passwordFailures != nil:
		render(w, r, http.StatusOK, passwordSignInPage("", ""))
	default:
		render(w, r, http.StatusOK, signInPage(""))
	}
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
	if err := s.startSession(w, r, acc); err != nil {
		s.serverError(w, r, "sign-in failed", err)
		return
	}
	http.Redirect(w, r, "/home", http.StatusSeeOther)
}

const (
	// sessionLifetime is how long a session lasts from sign-in, however often
	// it is used.
	sessionLifetime = 30 * 24 * time.Hour
	// sessionIdleLimit is how long a session lasts without a request.
	sessionIdleLimit = 7 * 24 * time.Hour
	// sessionSlideAfter is how stale a session's last-seen time gets before a
	// request re-issues its cookie, so not every request sets one.
	sessionSlideAfter = 24 * time.Hour
)

// startSession signs acc in on this browser: a signed session cookie carrying
// their current session generation, so EndSessions ends it, and the time they
// signed in.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, acc domain.Account) error {
	gen, err := s.svc.SessionGeneration(r.Context(), acc.ID)
	if err != nil {
		return err
	}
	now := s.svc.Now()
	s.setSessionCookie(w, session{accountID: acc.ID, generation: gen, signedIn: now, lastSeen: now})
	return nil
}

// setSessionCookie sets the session cookie to carry sess.
func (s *Server) setSessionCookie(w http.ResponseWriter, sess session) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    s.sessions.encode(sess),
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// slideSession re-issues sess's cookie with the last-seen time now, so the
// idle limit counts from this request, once its last-seen time is more than
// sessionSlideAfter old. The sign-in time stays.
func (s *Server) slideSession(w http.ResponseWriter, sess session) {
	now := s.svc.Now()
	if now.Sub(sess.lastSeen) <= sessionSlideAfter {
		return
	}
	sess.lastSeen = now
	s.setSessionCookie(w, sess)
}

// openSession is the session r's cookie carries and the Account it signs in,
// if it still does: its signature verifies, its generation is the Account's
// current one, the Account hasn't Departed, it was signed in less than
// sessionLifetime ago, and it hasn't sat idle past sessionIdleLimit.
func (s *Server) openSession(r *http.Request) (session, domain.Account, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return session{}, domain.Account{}, false
	}
	sess, ok := s.sessions.decode(c.Value)
	if !ok {
		return session{}, domain.Account{}, false
	}
	now := s.svc.Now()
	if now.Sub(sess.signedIn) >= sessionLifetime || now.Sub(sess.lastSeen) >= sessionIdleLimit {
		return session{}, domain.Account{}, false
	}
	acc, err := s.svc.Account(r.Context(), sess.accountID)
	if err != nil || acc.Departed {
		return session{}, domain.Account{}, false
	}
	if current, err := s.svc.SessionGeneration(r.Context(), acc.ID); err != nil || sess.generation != current {
		return session{}, domain.Account{}, false
	}
	return sess, acc, true
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

// session is what a session cookie carries: the Account it signs in, at which
// session generation, when they signed in, and when the app last re-issued it.
type session struct {
	accountID  int64
	generation int64
	signedIn   time.Time
	lastSeen   time.Time
}

// sessionCodec turns a session into a session cookie value and back. The value
// carries it with its HMAC-SHA256 under key, so a cookie can't be edited to
// name someone else, an ended generation, or a later time.
type sessionCodec struct {
	key []byte
}

// encode is the cookie value that carries sess:
// "<id>.<generation>.<signed-in>.<last-seen>.<signature>", the times in Unix
// seconds.
func (c sessionCodec) encode(sess session) string {
	payload := strings.Join([]string{
		strconv.FormatInt(sess.accountID, 10),
		strconv.FormatInt(sess.generation, 10),
		strconv.FormatInt(sess.signedIn.Unix(), 10),
		strconv.FormatInt(sess.lastSeen.Unix(), 10),
	}, ".")
	return payload + "." + c.sign(payload)
}

// decode is the session value carries, and whether its signature verifies. A
// value of any other shape, like the "<id>.<generation>.<signature>",
// "<id>.<signature>" or bare IDs older versions issued, doesn't.
func (c sessionCodec) decode(value string) (session, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 5 {
		return session{}, false
	}
	payload := strings.Join(parts[:4], ".")
	if !c.verify(payload, parts[4]) {
		return session{}, false
	}
	var nums [4]int64
	for i, part := range parts[:4] {
		n, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return session{}, false
		}
		nums[i] = n
	}
	return session{
		accountID:  nums[0],
		generation: nums[1],
		signedIn:   time.Unix(nums[2], 0),
		lastSeen:   time.Unix(nums[3], 0),
	}, true
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
