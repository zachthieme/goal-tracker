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

// theme is the colour theme a person has pinned the site to. The zero value,
// themeSystem, pins nothing and follows the operating system.
type theme string

const (
	themeSystem theme = ""
	themeLight  theme = "light"
	themeDark   theme = "dark"
)

// themeCookieLife is how long a choice is remembered; each choice renews it.
const themeCookieLife = 365 * 24 * time.Hour

// parseTheme reads a theme by its name in a form or cookie, reporting false for
// a name the server doesn't know.
func parseTheme(name string) (theme, bool) {
	switch name {
	case "system":
		return themeSystem, true
	case string(themeLight), string(themeDark):
		return theme(name), true
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

// withTheme records the chosen theme so the layout can pin it on every page
// without every page passing it along.
func withTheme(ctx context.Context, t theme) context.Context {
	return context.WithValue(ctx, themeKey{}, t)
}

// currentTheme is the chosen theme, System when none was recorded.
func currentTheme(ctx context.Context) theme {
	t, _ := ctx.Value(themeKey{}).(theme)
	return t
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
