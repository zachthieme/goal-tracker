package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// undoCookie carries an Undo offer from the post that made a change to the one
// page shown straight after it. It is scoped to that page's path and cleared
// the moment the page reads it, so the toast is shown once: a reload or a later
// visit finds nothing (#83). It carries the change's one-time Undo token for
// the toast to post; the domain checks the token, and what has happened
// since, when the Undo is posted (#106).
const undoCookie = "gt_undo"

// undoField is the hidden input a toast's Undo posts its token in.
const undoField = "undo"

// Undo offers a toast can carry, each naming what the change was.
const (
	undoLinkRemoval      = "link-removal"
	undoValueRetire      = "value-retire"
	undoLinkRejection    = "link-rejection"
	undoHandoffRejection = "handoff-rejection"
)

// offerUndo has the page at path offer Undo for kind's record id, once, with
// the token the domain issued for it. The offer is scoped to path's path part
// alone: a browser never sends a cookie whose Path holds a query or fragment,
// so a return like /home?x=1 is offered at /home (#597).
func offerUndo(w http.ResponseWriter, path, kind string, id int64, token string) {
	path, _, _ = strings.Cut(path, "#")
	path, _, _ = strings.Cut(path, "?")
	http.SetCookie(w, &http.Cookie{
		Name:     undoCookie,
		Value:    kind + ":" + strconv.FormatInt(id, 10) + ":" + token,
		Path:     path,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// undoOffer is the request's page's Undo offer, read by takeUndo.
type undoOffer struct {
	kind  string
	id    int64
	token string
}

// of returns the offer's record id when it is of kind.
func (o undoOffer) of(kind string) (int64, bool) {
	return o.id, o.kind == kind && o.id > 0
}

// fields are the hidden inputs the offer's Undo posts: its token, then extra.
func (o undoOffer) fields(extra ...toastField) []toastField {
	return append([]toastField{{Name: undoField, Value: o.token}}, extra...)
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
	parts := strings.SplitN(c.Value, ":", 3)
	if len(parts) != 3 {
		return undoOffer{}
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return undoOffer{}
	}
	return undoOffer{kind: parts[0], id: id, token: parts[2]}
}

// refuseUndo shows why an Undo was refused on refuse's page, with a link back
// to the page it was offered on: nothing to undo is a 404 saying so, and a
// failure a 500 saying nothing changed. refuse resolves the signed-in Account
// itself, so the one passed in goes unused.
func (s *Server) refuseUndo(w http.ResponseWriter, r *http.Request, _ domain.Account, back string, err error) {
	s.refuse(w, r, "Can't undo", back, err,
		refuseNotFoundSaying("There's nothing here to undo."),
		refuseFailureSaying("The Undo couldn't be done, so nothing changed. Try again."))
}

// sentence is msg ending in a full stop, its first letter upper-cased only
// when its first word, up to the first space, is all lowercase a–z. A first
// word holding anything else, such as an "@", a ".", a digit or an uppercase
// letter, is a value like an email or a Goal's title and is left as written
// (#583). A value that is all lowercase letters is still capitalised.
func sentence(msg string) string {
	if msg == "" {
		return msg
	}
	first, _, _ := strings.Cut(msg, " ")
	if strings.Trim(first, "abcdefghijklmnopqrstuvwxyz") == "" {
		msg = strings.ToUpper(msg[:1]) + msg[1:]
	}
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return msg
}

// undoIDFromPath is the id in an Undo's path, or a refusal saying there's
// nothing to undo, linking back.
func (s *Server) undoIDFromPath(w http.ResponseWriter, r *http.Request, current domain.Account, back string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.refuseUndo(w, r, current, back, domain.ErrNotFound)
		return 0, false
	}
	return id, true
}
