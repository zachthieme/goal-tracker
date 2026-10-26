package web

import (
	"fmt"
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

// undoKind is one kind of change a toast offers to Undo, the whole of its
// round trip: the decision offers it (offerUndo), the page it returns to shows
// its toast (undoToast), and the toast's Undo restores it (handleUndo),
// landing back on that page. A new kind is one entry in undoKinds and the
// domain calls it makes (#605).
type undoKind struct {
	// tag names the kind in the undo cookie's value, <tag>:<id>:<token>.
	tag string
	// route is the Undo's path pattern, its record's id at {id}.
	route string
	// fallback is the page an Undo returns to when it posts no return.
	fallback string
	// message reads record id through the domain and is the toast's sentence
	// for current on the page r asks for, or "" when it isn't current's to
	// undo now, so the page shows no toast.
	message func(s *Server, r *http.Request, current domain.Account, id int64) string
	// undo restores record id for current with the toast's token.
	undo func(s *Server, r *http.Request, current domain.Account, id int64, token string) error
}

// The kinds of change a toast can offer to Undo.
var (
	undoLinkRemoval = undoKind{
		tag:      "link-removal",
		route:    "/link-removals/{id}/undo",
		fallback: "/home",
		message: func(s *Server, r *http.Request, current domain.Account, id int64) string {
			removal, err := s.svc.LinkRemoval(r.Context(), id)
			if err != nil || removal.Restored || removal.RemovedBy != current.ID {
				return ""
			}
			if goalID, ok := undoPageGoalID(r); !ok || (removal.Child.ID != goalID && removal.Parent.ID != goalID) {
				return ""
			}
			return fmt.Sprintf("Link removed: %s no longer contributes to %s.", removal.Child.Title, removal.Parent.Title)
		},
		undo: func(s *Server, r *http.Request, current domain.Account, id int64, token string) error {
			_, err := s.svc.RestoreLink(r.Context(), id, current.ID, token)
			return err
		},
	}
	undoLinkRejection = undoKind{
		tag:      "link-rejection",
		route:    "/link-rejections/{id}/undo",
		fallback: "/links",
		message: func(s *Server, r *http.Request, current domain.Account, id int64) string {
			rejection, err := s.svc.LinkRejection(r.Context(), id)
			if err != nil || rejection.Restored || rejection.RejectedBy != current.ID {
				return ""
			}
			return fmt.Sprintf("Request rejected: %s won't contribute to %s.", rejection.Child.Title, rejection.Parent.Title)
		},
		undo: func(s *Server, r *http.Request, current domain.Account, id int64, token string) error {
			_, err := s.svc.RestoreLinkRequest(r.Context(), id, current.ID, token)
			return err
		},
	}
	undoHandoffRejection = undoKind{
		tag:      "handoff-rejection",
		route:    "/handoffs/{id}/restore",
		fallback: "/handoffs",
		message: func(s *Server, r *http.Request, current domain.Account, id int64) string {
			ho, err := s.svc.Handoff(r.Context(), id)
			if err != nil || ho.Status != domain.HandoffRejected || ho.To.ID != current.ID {
				return ""
			}
			return fmt.Sprintf("Handoff rejected: %s stays with %s.", ho.Goal.Title, ho.From.Label())
		},
		undo: func(s *Server, r *http.Request, current domain.Account, id int64, token string) error {
			_, err := s.svc.RestoreHandoff(r.Context(), id, current.ID, token)
			return err
		},
	}
	// undoSuggestionDecline shows on Home, or on its own Goal's page.
	undoSuggestionDecline = undoKind{
		tag:      "parent-suggestion-decline",
		route:    "/parent-suggestions/{id}/undo-decline",
		fallback: "/home",
		message: func(s *Server, r *http.Request, current domain.Account, id int64) string {
			p, err := s.svc.ParentSuggestion(r.Context(), id)
			if err != nil || p.Status != domain.SuggestionDeclined || p.Goal.Owner.ID != current.ID {
				return ""
			}
			if goalID, ok := undoPageGoalID(r); ok && p.Goal.ID != goalID {
				return ""
			}
			return fmt.Sprintf("Suggestion declined: %s won't be linked to %s.", p.Goal.Title, p.Parent.Title)
		},
		undo: func(s *Server, r *http.Request, current domain.Account, id int64, token string) error {
			return s.svc.UndoDeclineParentSuggestion(r.Context(), current.ID, id, token)
		},
	}
	// undoValueRetire shows only to an Admin, and only while the value is
	// still Retired. The Retired value's own Restore button needs none of its
	// checks (handleRestoreDimensionValue).
	undoValueRetire = undoKind{
		tag:      "value-retire",
		route:    "/dimension-values/{id}/undo-retire",
		fallback: "/dimensions",
		message: func(s *Server, r *http.Request, current domain.Account, id int64) string {
			if !current.IsAdmin {
				return ""
			}
			dims, err := s.svc.ListDimensions(r.Context())
			if err != nil {
				return ""
			}
			for _, d := range dims {
				for _, v := range d.Values {
					if v.ID == id && v.Retired {
						return fmt.Sprintf("Retired %s from %s.", v.Value, d.Name)
					}
				}
			}
			return ""
		},
		undo: func(s *Server, r *http.Request, current domain.Account, id int64, token string) error {
			return s.svc.UndoRetireDimensionValue(r.Context(), current.ID, id, token)
		},
	}
)

// undoKinds are every kind of Undo; routes registers each one's route.
var undoKinds = []undoKind{undoLinkRemoval, undoLinkRejection, undoHandoffRejection, undoSuggestionDecline, undoValueRetire}

// undoPageGoalID is the Goal whose page r asks for, when it is a Goal page.
func undoPageGoalID(r *http.Request) (int64, bool) {
	rest, ok := strings.CutPrefix(r.URL.Path, "/goals/")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	return id, err == nil
}

// offerUndo has back, the page the decision returns to, offer Undo of kind's
// record id, once, with the token the domain issued for it, and redirects
// there. The offer is scoped to back's path part alone: a browser never sends
// a cookie whose Path holds a query or fragment, so a return like /home?x=1
// is offered at /home, and shown there, query and all (#597).
func (s *Server) offerUndo(w http.ResponseWriter, r *http.Request, kind undoKind, id int64, token, back string) {
	path, _, _ := strings.Cut(back, "#")
	path, _, _ = strings.Cut(path, "?")
	http.SetCookie(w, &http.Cookie{
		Name:     undoCookie,
		Value:    kind.tag + ":" + strconv.FormatInt(id, 10) + ":" + token,
		Path:     path,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// undoToast is the toast the requested page carries straight after a
// decision offered it Undo, or nil when there is no offer, or none current
// can undo here now. Its Undo posts the offer's token and returns to this
// page, query and all.
func (s *Server) undoToast(w http.ResponseWriter, r *http.Request, current domain.Account) *toast {
	offer := takeUndo(w, r)
	if offer.id <= 0 {
		return nil
	}
	for _, kind := range undoKinds {
		if kind.tag != offer.kind {
			continue
		}
		msg := kind.message(s, r, current, offer.id)
		if msg == "" {
			return nil
		}
		return &toast{
			Message: msg,
			Undo:    strings.Replace(kind.route, "{id}", strconv.FormatInt(offer.id, 10), 1),
			Fields: []toastField{
				{Name: undoField, Value: offer.token},
				{Name: "return", Value: r.URL.RequestURI()},
			},
		}
	}
	return nil
}

// handleUndo is kind's Undo of the record in the path: the domain checks the
// toast's token and what has happened since. Done, it returns to the page the
// Undo was offered on (its return, query and all, or else kind's fallback);
// refused, it shows a page saying why, linking back there, and changes
// nothing.
func (s *Server) handleUndo(kind undoKind) func(http.ResponseWriter, *http.Request, domain.Account) {
	return func(w http.ResponseWriter, r *http.Request, current domain.Account) {
		back := returnTo(r, kind.fallback)
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			s.refuseUndo(w, r, current, back, domain.ErrNotFound)
			return
		}
		if err := kind.undo(s, r, current, id, r.FormValue(undoField)); err != nil {
			s.refuseUndo(w, r, current, back, err)
			return
		}
		http.Redirect(w, r, back, http.StatusSeeOther)
	}
}

// undoOffer is the request's page's Undo offer, read by takeUndo.
type undoOffer struct {
	kind  string
	id    int64
	token string
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
