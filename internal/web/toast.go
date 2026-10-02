package web

import (
	"net/http"
	"strconv"
	"strings"
)

// undoCookie carries an Undo offer from the post that made a change to the one
// page shown straight after it. It is scoped to that page's path and cleared
// the moment the page reads it, so the toast is shown once: a reload or a later
// visit finds nothing (#83). It only decides whether to show the toast; the
// Undo itself is checked again by the domain when it is posted.
const undoCookie = "gt_undo"

// Undo offers a toast can carry, each naming what the change was.
const (
	undoLinkRemoval      = "link-removal"
	undoValueRetire      = "value-retire"
	undoLinkRejection    = "link-rejection"
	undoHandoffRejection = "handoff-rejection"
)

// offerUndo has the page at path offer Undo for kind's record id, once.
func offerUndo(w http.ResponseWriter, path, kind string, id int64) {
	http.SetCookie(w, &http.Cookie{
		Name:     undoCookie,
		Value:    kind + ":" + strconv.FormatInt(id, 10),
		Path:     path,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// undoOffer is the request's page's Undo offer, read by takeUndo.
type undoOffer struct {
	kind string
	id   int64
}

// of returns the offer's record id when it is of kind.
func (o undoOffer) of(kind string) (int64, bool) {
	return o.id, o.kind == kind && o.id > 0
}

// takeUndo returns the Undo offer made to the requested page, if any, and
// clears it so the page shows its toast only this once.
func takeUndo(w http.ResponseWriter, r *http.Request) undoOffer {
	c, err := r.Cookie(undoCookie)
	if err != nil {
		return undoOffer{}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     undoCookie,
		Path:     r.URL.Path,
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	kind, raw, _ := strings.Cut(c.Value, ":")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return undoOffer{}
	}
	return undoOffer{kind: kind, id: id}
}
