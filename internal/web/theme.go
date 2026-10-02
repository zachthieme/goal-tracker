package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// themeCookie keeps a browser's chosen theme. It isn't tied to the session, so
// the choice holds before sign-in and after sign-out.
const themeCookie = "gt_theme"

// theme is the colour theme a person has pinned the site to, by the name the
// menu posts and the cookie keeps. System pins nothing and follows the
// operating system.
type theme string

const (
	themeSystem theme = "system"
	themeLight  theme = "light"
	themeDark   theme = "dark"
)

// themeChoices are the Theme menu's choices, in the order it lists them.
var themeChoices = []struct {
	theme theme
	label string
}{
	{themeSystem, "System"},
	{themeLight, "Light"},
	{themeDark, "Dark"},
}

// themeCookieLife is how long a choice is remembered; each choice renews it.
const themeCookieLife = 365 * 24 * time.Hour

// parseTheme reads a theme by its name, reporting false for a name the server
// doesn't know.
func parseTheme(name string) (theme, bool) {
	switch t := theme(name); t {
	case themeSystem, themeLight, themeDark:
		return t, true
	default:
		return themeSystem, false
	}
}

// themeFromCookie is the theme the request's cookie chose: System when there
// is no cookie or its value isn't one the server knows.
func themeFromCookie(r *http.Request) theme {
	c, err := r.Cookie(themeCookie)
	if err != nil {
		return themeSystem
	}
	t, _ := parseTheme(c.Value)
	return t
}

type themeKey struct{}

// themeState is what the layout needs to pin the chosen theme and offer the
// others: the choice, and the page to come back to after choosing again.
type themeState struct {
	chosen theme
	back   string
}

// withTheme records the browser's chosen theme in r's context, so the layout
// can pin it on every page without every page passing it along. The page to
// come back to is the one requested; a page rendered by a post has no address
// to come back to, so choosing from it lands on Home.
func withTheme(r *http.Request) *http.Request {
	st := themeState{chosen: themeFromCookie(r)}
	if r.Method == http.MethodGet {
		st.back = r.URL.RequestURI()
	}
	return r.WithContext(context.WithValue(r.Context(), themeKey{}, st))
}

// currentTheme is the chosen theme, System when none was recorded.
func currentTheme(ctx context.Context) theme {
	if st, ok := ctx.Value(themeKey{}).(themeState); ok {
		return st.chosen
	}
	return themeSystem
}

// themeReturn is the page the Theme menu comes back to after a choice.
func themeReturn(ctx context.Context) string {
	st, _ := ctx.Value(themeKey{}).(themeState)
	return st.back
}

// handleSetTheme remembers the theme a person chose in the browser's cookie,
// clearing it for System, and sends them back to the page they chose it on.
// It needs no sign-in: the choice belongs to the browser, not the session.
func (s *Server) handleSetTheme(w http.ResponseWriter, r *http.Request) {
	t, ok := parseTheme(r.FormValue("theme"))
	if !ok {
		http.Error(w, "unknown theme", http.StatusBadRequest)
		return
	}
	c := &http.Cookie{
		Name:     themeCookie,
		Value:    string(t),
		Path:     "/",
		MaxAge:   int(themeCookieLife.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	if t == themeSystem {
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
	http.Redirect(w, r, returnPath(r.FormValue("return")), http.StatusSeeOther)
}

// returnPath is where to send someone back to: back when it is a path on this
// site, Home otherwise, so the form can't be used to send people elsewhere.
func returnPath(back string) string {
	u, err := url.Parse(back)
	if err != nil || u.Scheme != "" || u.Host != "" ||
		!strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") || strings.Contains(back, `\`) {
		return "/home"
	}
	return back
}
