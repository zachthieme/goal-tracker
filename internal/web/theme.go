package web

import (
	"context"
	"net/http"
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

// themeFromCookie is the theme the request's cookie chose: System when there
// is no cookie or its value isn't one the server knows.
func themeFromCookie(r *http.Request) theme {
	c, err := r.Cookie(themeCookie)
	if err != nil {
		return themeSystem
	}
	switch t := theme(c.Value); t {
	case themeLight, themeDark:
		return t
	default:
		return themeSystem
	}
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
