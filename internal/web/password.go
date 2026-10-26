package web

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// passwordSignInFailed is what the password sign-in form says to every failed
// try, whatever the cause, so it can't be used to learn who has an Account.
const passwordSignInFailed = "That email and password didn't work. After 5 failed tries an account is locked for 15 minutes."

// passwordLinkGone is what the set-password page says for a link that's
// unknown, expired, or used.
const passwordLinkGone = "This link has expired or was already used. Ask an Admin for a new one."

// handlePasswordSignIn signs a person in with their email and password. Every
// failure gets the same 401 page, including a client IP that has failed too
// often, which isn't asked about the password at all. A Departed person with
// the right password is told they've departed.
func (s *Server) handlePasswordSignIn(w http.ResponseWriter, r *http.Request) {
	emailAddr := r.FormValue("email")
	ip := s.clientIP(r)
	if s.passwordFailures.blocked(ip, s.svc.Now()) {
		render(w, r, http.StatusUnauthorized, passwordSignInPage(passwordSignInFailed, emailAddr))
		return
	}
	acc, err := s.svc.PasswordSignIn(r.Context(), emailAddr, r.FormValue("password"))
	switch {
	case errors.Is(err, domain.ErrBadCredentials):
		s.passwordFailures.record(ip, s.svc.Now())
		render(w, r, http.StatusUnauthorized, passwordSignInPage(passwordSignInFailed, emailAddr))
		return
	case errors.Is(err, domain.ErrDeparted):
		render(w, r, http.StatusForbidden, passwordSignInPage(departedSentence(emailAddr), emailAddr))
		return
	case err != nil:
		s.serverError(w, r, "sign-in failed", err)
		return
	}
	if err := s.startSession(w, r, acc); err != nil {
		s.serverError(w, r, "sign-in failed", err)
		return
	}
	http.Redirect(w, r, "/home", http.StatusSeeOther)
}

// handleSetPasswordForm is the page a set-password link opens: the person's
// email and the two password fields.
func (s *Server) handleSetPasswordForm(w http.ResponseWriter, r *http.Request) {
	keepPasswordLinkPrivate(w)
	token := r.URL.Query().Get("token")
	acc, ok := s.passwordLinkAccount(w, r, token)
	if !ok {
		return
	}
	render(w, r, http.StatusOK, setPasswordPage(acc.Email, token, ""))
}

// handleSetPassword sets the password a set-password link is for, then signs
// the person in afresh and sends them Home. Fields that don't match, or a
// password too short or too long, show the form again with the reason.
func (s *Server) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	keepPasswordLinkPrivate(w)
	token := r.FormValue("token")
	acc, ok := s.passwordLinkAccount(w, r, token)
	if !ok {
		return
	}
	password := r.FormValue("password")
	if password != r.FormValue("confirm") {
		render(w, r, http.StatusUnprocessableEntity, setPasswordPage(acc.Email, token, "The two passwords don't match."))
		return
	}
	signedIn, err := s.svc.SetPassword(r.Context(), token, password)
	switch {
	case errors.Is(err, domain.ErrPasswordRules):
		render(w, r, http.StatusUnprocessableEntity, setPasswordPage(acc.Email, token, refusalSentence(err)))
		return
	case errors.Is(err, domain.ErrLinkInvalid):
		render(w, r, http.StatusGone, setPasswordNoticePage(passwordLinkGone))
		return
	case errors.Is(err, domain.ErrDeparted):
		render(w, r, http.StatusForbidden, setPasswordNoticePage(departedSentence(acc.Email)))
		return
	case err != nil:
		s.serverError(w, r, "could not set the password", err)
		return
	}
	if err := s.startSession(w, r, signedIn); err != nil {
		s.serverError(w, r, "sign-in failed", err)
		return
	}
	http.Redirect(w, r, "/home", http.StatusSeeOther)
}

// passwordLinkAccount is the Account token's set-password link is for. When
// the link doesn't work, or the person has departed, it answers the request
// itself and reports false.
func (s *Server) passwordLinkAccount(w http.ResponseWriter, r *http.Request, token string) (domain.Account, bool) {
	acc, err := s.svc.PasswordLinkAccount(r.Context(), token)
	switch {
	case errors.Is(err, domain.ErrLinkInvalid):
		render(w, r, http.StatusGone, setPasswordNoticePage(passwordLinkGone))
		return domain.Account{}, false
	case err != nil:
		s.serverError(w, r, "could not read the set-password link", err)
		return domain.Account{}, false
	case acc.Departed:
		render(w, r, http.StatusForbidden, setPasswordNoticePage(departedSentence(acc.Email)))
		return domain.Account{}, false
	}
	return acc, true
}

// keepPasswordLinkPrivate stops the browser sending the set-password page's
// address, token and all, to anything the page loads, and caching the page.
func keepPasswordLinkPrivate(w http.ResponseWriter) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
}

// clientIP is the client a password sign-in is throttled by. It's the host of
// r's RemoteAddr, unless that is a trusted proxy (see WithTrustedProxies): then
// it's the right-most address in X-Forwarded-For that isn't a trusted proxy,
// the last one a proxy we trust saw. A client can write the rest of the
// header, so it's never read past that address, nor at all from a request
// that didn't come through a trusted proxy. With the header missing, an entry
// that isn't an address before the client's, or nothing but trusted proxies
// in it, it's RemoteAddr's host.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if remote, err := netip.ParseAddr(host); err != nil || !s.trustedProxy(remote) {
		return host
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for _, hop := range slices.Backward(hops) {
		addr, err := netip.ParseAddr(strings.TrimSpace(hop))
		if err != nil {
			return host
		}
		if !s.trustedProxy(addr) {
			return addr.Unmap().String()
		}
	}
	return host
}

// trustedProxy reports whether addr is within one of the trusted proxies'
// ranges.
func (s *Server) trustedProxy(addr netip.Addr) bool {
	addr = addr.Unmap().WithZone("") // a range can't name a zone
	for _, p := range s.trustedProxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// The per-IP limit on password sign-ins: after passwordFailuresPerIP failed
// tries within passwordFailureWindow, an IP's sign-ins fail until the oldest
// of them is that old.
const (
	passwordFailuresPerIP = 50
	passwordFailureWindow = 15 * time.Minute
	// passwordFailureIPsBeforeSweep is how many IPs the count holds before a
	// failure sweeps out those whose failures have all aged out.
	passwordFailureIPsBeforeSweep = 4096
)

// passwordFailuresByIP counts each client IP's failed password sign-ins within
// the window, in memory, so a restart forgets them. The times come from the
// domain service's clock.
type passwordFailuresByIP struct {
	mu sync.Mutex
	// byIP is when each IP failed within the window, oldest first.
	byIP map[string][]time.Time
}

func newPasswordFailuresByIP() *passwordFailuresByIP {
	return &passwordFailuresByIP{byIP: map[string][]time.Time{}}
}

// blocked reports whether ip has failed too often within the window to now.
func (f *passwordFailuresByIP) blocked(ip string, now time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.recent(ip, now)) >= passwordFailuresPerIP
}

// record counts a failed sign-in from ip at now.
func (f *passwordFailuresByIP) record(ip string, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byIP[ip] = append(f.recent(ip, now), now)
	if len(f.byIP) > passwordFailureIPsBeforeSweep { // forget the IPs whose failures have all aged out
		for other := range f.byIP {
			f.recent(other, now)
		}
	}
}

// recent drops ip's failures older than the window to now, and returns the
// rest. f.mu must be held.
func (f *passwordFailuresByIP) recent(ip string, now time.Time) []time.Time {
	times := f.byIP[ip]
	i := 0
	for i < len(times) && now.Sub(times[i]) >= passwordFailureWindow {
		i++
	}
	if i == len(times) {
		delete(f.byIP, ip)
		return nil
	}
	f.byIP[ip] = times[i:]
	return times[i:]
}
