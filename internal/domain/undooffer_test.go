package domain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// undoOfferKind is one kind of Undo the agreement tests run: setup makes a
// fresh harness holding one undoable change, sc, whose record is id(sc); offer
// asks whether a viewer may still undo it with a token, and restore makes that
// Undo through tx.
type undoOfferKind[S any] struct {
	setup   func(t *testing.T) (*testsupport.Harness, S)
	id      func(sc S) int64
	offer   func(s *domain.Service, ctx context.Context, actorID, id int64, token string) (bool, error)
	restore func(ctx context.Context, tx *domain.Service, actorID, id int64, token string) error
}

// undoOfferCase is one case an agreement test asks about: arrange makes its
// state from the kind's change and says who asks, with what token, and want
// is the case's answer: nil when the Undo is offered and accepted, or the
// error kind (ErrNotAuthorized or ErrValidation) it is refused with.
type undoOfferCase[S any] struct {
	name    string
	want    error
	arrange func(h *testsupport.Harness, sc S) (viewer domain.Account, token string)
}

// undoOfferAgrees checks, for each case on a fresh harness, that kind's offer
// is made exactly when the case wants it, and that its restore, made with the
// same viewer, record and token in a transaction rolled back after it,
// accepts the Undo when offered and otherwise refuses it with the case's
// error kind. The two agreeing isn't enough alone, since both ask the same
// rule: loosening it would still agree. It fails unless some case is both
// offered and accepted and some refused, since either alone proves nothing of
// the rule, and unless the offer for a record that doesn't exist is
// ErrNotFound.
func undoOfferAgrees[S any](t *testing.T, kind undoOfferKind[S], cases []undoOfferCase[S]) {
	t.Helper()
	ctx := context.Background()
	var accepted, refused bool
	for _, c := range cases {
		h, sc := kind.setup(t)
		viewer, token := c.arrange(h, sc)
		id := kind.id(sc)
		offered, err := kind.offer(h.Service, ctx, viewer.ID, id, token)
		if err != nil {
			t.Errorf("%s: the offer failed: %v", c.name, err)
			continue
		}
		var got error
		if err := h.Service.WithinTx(ctx, func(tx *domain.Service) error {
			got = kind.restore(ctx, tx, viewer.ID, id, token)
			return errRolledBack
		}); !errors.Is(err, errRolledBack) {
			t.Fatalf("%s: the transaction ended %v", c.name, err)
		}
		switch {
		case offered != (c.want == nil):
			t.Errorf("%s: offered = %v, want %v", c.name, offered, c.want == nil)
		case c.want == nil && got == nil:
			accepted = true
		case c.want != nil && errors.Is(got, c.want):
			refused = true
		default:
			t.Errorf("%s: offered = %v, but the Undo returned %v, want %v", c.name, offered, got, c.want)
		}
	}
	if !accepted {
		t.Error("no case was both offered and accepted")
	}
	if !refused {
		t.Error("no case was refused")
	}
	h, sc := kind.setup(t)
	if _, err := kind.offer(h.Service, ctx, 1, kind.id(sc)+1000, "not-a-token"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the offer for a record that doesn't exist: err = %v, want ErrNotFound", err)
	}
}

// handOffAndAccept has owner hand g off to to, who accepts it.
func handOffAndAccept(h *testsupport.Harness, owner domain.Account, g domain.Goal, to domain.Account) {
	h.T.Helper()
	ho := h.StartHandoff(owner, g.ID, to)
	if _, err := h.Service.AcceptHandoff(context.Background(), ho.ID, to.ID, nil); err != nil {
		h.T.Fatalf("AcceptHandoff: %v", err)
	}
}

// linkRemovalUndo is Sam's removal of the link from Sam's Goal to Pat's.
type linkRemovalUndo struct {
	pat, sam, quinn domain.Account
	child           domain.Goal
	removal         domain.LinkRemoval
}

// A link removal's Undo is offered exactly when RestoreLink would accept it:
// to the remover, with their token, while it isn't restored, the token is
// unused and within UndoWindow, and they still own one of the two Goals.
func TestALinkRemovalUndoIsOfferedExactlyWhenRestoreLinkAccepts(t *testing.T) {
	t.Parallel()
	kind := undoOfferKind[linkRemovalUndo]{
		setup: func(t *testing.T) (*testsupport.Harness, linkRemovalUndo) {
			h := testsupport.New(t)
			pat, sam, child, _, link := acceptedAcrossOwners(t, h)
			removal, err := h.Service.RemoveLink(context.Background(), link.ID, sam.ID)
			if err != nil {
				t.Fatalf("RemoveLink: %v", err)
			}
			return h, linkRemovalUndo{pat: pat, sam: sam, quinn: h.SignIn("quinn@example.com"), child: child, removal: removal}
		},
		id:    func(sc linkRemovalUndo) int64 { return sc.removal.ID },
		offer: (*domain.Service).CanUndoLinkRemoval,
		restore: func(ctx context.Context, tx *domain.Service, actorID, id int64, token string) error {
			_, err := tx.RestoreLink(ctx, id, actorID, token)
			return err
		},
	}
	undoOfferAgrees(t, kind, []undoOfferCase[linkRemovalUndo]{
		{"the remover", nil, func(h *testsupport.Harness, sc linkRemovalUndo) (domain.Account, string) {
			return sc.sam, sc.removal.UndoToken
		}},
		{"the parent's Owner, with the remover's token", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc linkRemovalUndo) (domain.Account, string) {
			return sc.pat, sc.removal.UndoToken
		}},
		{"someone else, with the remover's token", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc linkRemovalUndo) (domain.Account, string) {
			return sc.quinn, sc.removal.UndoToken
		}},
		{"the remover, with a made-up token", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc linkRemovalUndo) (domain.Account, string) {
			return sc.sam, "not-a-token"
		}},
		{"the remover, after an earlier Undo", domain.ErrValidation, func(h *testsupport.Harness, sc linkRemovalUndo) (domain.Account, string) {
			if _, err := h.Service.RestoreLink(context.Background(), sc.removal.ID, sc.sam.ID, sc.removal.UndoToken); err != nil {
				h.T.Fatalf("RestoreLink: %v", err)
			}
			return sc.sam, sc.removal.UndoToken
		}},
		{"the remover, after UndoWindow and a second", domain.ErrValidation, func(h *testsupport.Harness, sc linkRemovalUndo) (domain.Account, string) {
			h.Clock.Advance(domain.UndoWindow + time.Second)
			return sc.sam, sc.removal.UndoToken
		}},
		{"the remover, once they own neither Goal", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc linkRemovalUndo) (domain.Account, string) {
			handOffAndAccept(h, sc.sam, sc.child, sc.quinn)
			return sc.sam, sc.removal.UndoToken
		}},
	})
}

// linkRejectionUndo is Pat's rejection of Sam's request to link Sam's Goal to
// Pat's.
type linkRejectionUndo struct {
	pat, sam, quinn domain.Account
	rejection       domain.LinkRejection
}

// A link rejection's Undo is offered exactly when RestoreLinkRequest would
// accept it: to the rejecter, with their token, while it isn't restored and
// the token is unused and within UndoWindow.
func TestALinkRejectionUndoIsOfferedExactlyWhenRestoreLinkRequestAccepts(t *testing.T) {
	t.Parallel()
	kind := undoOfferKind[linkRejectionUndo]{
		setup: func(t *testing.T) (*testsupport.Harness, linkRejectionUndo) {
			h := testsupport.New(t)
			pat := h.SignIn("pat@example.com")
			sam := h.SignIn("sam@example.com")
			parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
			child := h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")
			link := h.RequestLink(sam, child, parent, "displays cause outages")
			rejection, err := h.Service.RejectLink(context.Background(), link.ID, pat.ID)
			if err != nil {
				t.Fatalf("RejectLink: %v", err)
			}
			return h, linkRejectionUndo{pat: pat, sam: sam, quinn: h.SignIn("quinn@example.com"), rejection: rejection}
		},
		id:    func(sc linkRejectionUndo) int64 { return sc.rejection.ID },
		offer: (*domain.Service).CanUndoLinkRejection,
		restore: func(ctx context.Context, tx *domain.Service, actorID, id int64, token string) error {
			_, err := tx.RestoreLinkRequest(ctx, id, actorID, token)
			return err
		},
	}
	undoOfferAgrees(t, kind, []undoOfferCase[linkRejectionUndo]{
		{"the rejecter", nil, func(h *testsupport.Harness, sc linkRejectionUndo) (domain.Account, string) {
			return sc.pat, sc.rejection.UndoToken
		}},
		{"the requester, with the rejecter's token", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc linkRejectionUndo) (domain.Account, string) {
			return sc.sam, sc.rejection.UndoToken
		}},
		{"someone else, with the rejecter's token", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc linkRejectionUndo) (domain.Account, string) {
			return sc.quinn, sc.rejection.UndoToken
		}},
		{"the rejecter, with a made-up token", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc linkRejectionUndo) (domain.Account, string) {
			return sc.pat, "not-a-token"
		}},
		{"the rejecter, after an earlier Undo", domain.ErrValidation, func(h *testsupport.Harness, sc linkRejectionUndo) (domain.Account, string) {
			if _, err := h.Service.RestoreLinkRequest(context.Background(), sc.rejection.ID, sc.pat.ID, sc.rejection.UndoToken); err != nil {
				h.T.Fatalf("RestoreLinkRequest: %v", err)
			}
			return sc.pat, sc.rejection.UndoToken
		}},
		{"the rejecter, after UndoWindow and a second", domain.ErrValidation, func(h *testsupport.Harness, sc linkRejectionUndo) (domain.Account, string) {
			h.Clock.Advance(domain.UndoWindow + time.Second)
			return sc.pat, sc.rejection.UndoToken
		}},
	})
}

// handoffRejectionUndo is Quinn's rejection of Sam's Handoff of Sam's Goal to
// them.
type handoffRejectionUndo struct {
	sam, quinn, rita domain.Account
	handoff          domain.Handoff
	token            string
}

// A Handoff rejection's Undo is offered exactly when RestoreHandoff would
// accept it: to the rejecter, the proposed new Owner, with their token, while
// it is rejected and the token is unused and within UndoWindow.
func TestAHandoffRejectionUndoIsOfferedExactlyWhenRestoreHandoffAccepts(t *testing.T) {
	t.Parallel()
	kind := undoOfferKind[handoffRejectionUndo]{
		setup: func(t *testing.T) (*testsupport.Harness, handoffRejectionUndo) {
			h := testsupport.New(t)
			sam := h.SignIn("sam@example.com")
			quinn := h.SignIn("quinn@example.com")
			g := h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")
			ho := h.StartHandoff(sam, g.ID, quinn)
			token, err := h.Service.RejectHandoff(context.Background(), ho.ID, quinn.ID)
			if err != nil {
				t.Fatalf("RejectHandoff: %v", err)
			}
			return h, handoffRejectionUndo{sam: sam, quinn: quinn, rita: h.SignIn("rita@example.com"), handoff: ho, token: token}
		},
		id:    func(sc handoffRejectionUndo) int64 { return sc.handoff.ID },
		offer: (*domain.Service).CanUndoHandoffRejection,
		restore: func(ctx context.Context, tx *domain.Service, actorID, id int64, token string) error {
			_, err := tx.RestoreHandoff(ctx, id, actorID, token)
			return err
		},
	}
	undoOfferAgrees(t, kind, []undoOfferCase[handoffRejectionUndo]{
		{"the rejecter", nil, func(h *testsupport.Harness, sc handoffRejectionUndo) (domain.Account, string) {
			return sc.quinn, sc.token
		}},
		{"the Goal's Owner, with the rejecter's token", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc handoffRejectionUndo) (domain.Account, string) {
			return sc.sam, sc.token
		}},
		{"someone else, with the rejecter's token", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc handoffRejectionUndo) (domain.Account, string) {
			return sc.rita, sc.token
		}},
		{"the rejecter, with a made-up token", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc handoffRejectionUndo) (domain.Account, string) {
			return sc.quinn, "not-a-token"
		}},
		{"the rejecter, after an earlier Undo", domain.ErrValidation, func(h *testsupport.Harness, sc handoffRejectionUndo) (domain.Account, string) {
			if _, err := h.Service.RestoreHandoff(context.Background(), sc.handoff.ID, sc.quinn.ID, sc.token); err != nil {
				h.T.Fatalf("RestoreHandoff: %v", err)
			}
			return sc.quinn, sc.token
		}},
		{"the rejecter, after UndoWindow and a second", domain.ErrValidation, func(h *testsupport.Harness, sc handoffRejectionUndo) (domain.Account, string) {
			h.Clock.Advance(domain.UndoWindow + time.Second)
			return sc.quinn, sc.token
		}},
	})
}

// suggestionDeclineUndo is Sam's decline of Rita's suggestion that Sam's Goal
// contribute to Pat's.
type suggestionDeclineUndo struct {
	sam, rita, quinn domain.Account
	goal             domain.Goal
	suggestion       domain.ParentSuggestion
	token            string
}

// A Parent suggestion decline's Undo is offered exactly when
// UndoDeclineParentSuggestion would accept it: to the decliner, whose token
// alone says it was theirs, while it is declined and the token is unused and
// within UndoWindow, even once they have handed the Goal off.
func TestASuggestionDeclineUndoIsOfferedExactlyWhenUndoDeclineAccepts(t *testing.T) {
	t.Parallel()
	kind := undoOfferKind[suggestionDeclineUndo]{
		setup: func(t *testing.T) (*testsupport.Harness, suggestionDeclineUndo) {
			h := testsupport.New(t)
			sam := h.SignIn("sam@example.com")
			pat := h.SignIn("pat@example.com")
			rita := h.SignIn("rita@example.com")
			g := h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")
			parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
			p := h.SuggestParent(rita, g, parent, "")
			token, err := h.Service.DeclineParentSuggestion(context.Background(), sam.ID, p.ID)
			if err != nil {
				t.Fatalf("DeclineParentSuggestion: %v", err)
			}
			return h, suggestionDeclineUndo{sam: sam, rita: rita, quinn: h.SignIn("quinn@example.com"), goal: g, suggestion: p, token: token}
		},
		id:    func(sc suggestionDeclineUndo) int64 { return sc.suggestion.ID },
		offer: (*domain.Service).CanUndoParentSuggestionDecline,
		restore: func(ctx context.Context, tx *domain.Service, actorID, id int64, token string) error {
			return tx.UndoDeclineParentSuggestion(ctx, actorID, id, token)
		},
	}
	undoOfferAgrees(t, kind, []undoOfferCase[suggestionDeclineUndo]{
		{"the decliner", nil, func(h *testsupport.Harness, sc suggestionDeclineUndo) (domain.Account, string) {
			return sc.sam, sc.token
		}},
		{"the suggester, with the decliner's token", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc suggestionDeclineUndo) (domain.Account, string) {
			return sc.rita, sc.token
		}},
		{"someone else, with the decliner's token", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc suggestionDeclineUndo) (domain.Account, string) {
			return sc.quinn, sc.token
		}},
		{"the decliner, with a made-up token", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc suggestionDeclineUndo) (domain.Account, string) {
			return sc.sam, "not-a-token"
		}},
		{"the decliner, after an earlier Undo", domain.ErrValidation, func(h *testsupport.Harness, sc suggestionDeclineUndo) (domain.Account, string) {
			if err := h.Service.UndoDeclineParentSuggestion(context.Background(), sc.sam.ID, sc.suggestion.ID, sc.token); err != nil {
				h.T.Fatalf("UndoDeclineParentSuggestion: %v", err)
			}
			return sc.sam, sc.token
		}},
		{"the decliner, after UndoWindow and a second", domain.ErrValidation, func(h *testsupport.Harness, sc suggestionDeclineUndo) (domain.Account, string) {
			h.Clock.Advance(domain.UndoWindow + time.Second)
			return sc.sam, sc.token
		}},
		{"the decliner, after handing the Goal off", nil, func(h *testsupport.Harness, sc suggestionDeclineUndo) (domain.Account, string) {
			handOffAndAccept(h, sc.sam, sc.goal, sc.quinn)
			return sc.sam, sc.token
		}},
	})
}

// valueRetirementUndo is Admin's retirement of Pillar's value Growth.
type valueRetirementUndo struct {
	admin, ada, rita domain.Account
	value            domain.DimensionValue
	token            string
}

// A value retirement's Undo is offered exactly when UndoRetireDimensionValue
// would accept it: to the Admin who retired it, with their token, while they
// are still an Admin, the value is still Retired under the name it was retired
// under, and the token is unused and within UndoWindow.
func TestAValueRetirementUndoIsOfferedExactlyWhenUndoRetireAccepts(t *testing.T) {
	t.Parallel()
	kind := undoOfferKind[valueRetirementUndo]{
		setup: func(t *testing.T) (*testsupport.Harness, valueRetirementUndo) {
			h := testsupport.New(t, "admin@example.com", "ada@example.com")
			admin := h.SignIn("admin@example.com")
			dim := h.CreateDimension(admin, "Pillar", "Growth")
			value := dim.Values[0]
			token, err := h.Service.RetireDimensionValueWithUndo(context.Background(), admin.ID, value.ID)
			if err != nil {
				t.Fatalf("RetireDimensionValueWithUndo: %v", err)
			}
			return h, valueRetirementUndo{admin: admin, ada: h.SignIn("ada@example.com"), rita: h.SignIn("rita@example.com"), value: value, token: token}
		},
		id:    func(sc valueRetirementUndo) int64 { return sc.value.ID },
		offer: (*domain.Service).CanUndoValueRetirement,
		restore: func(ctx context.Context, tx *domain.Service, actorID, id int64, token string) error {
			return tx.UndoRetireDimensionValue(ctx, actorID, id, token)
		},
	}
	ctx := context.Background()
	undoOfferAgrees(t, kind, []undoOfferCase[valueRetirementUndo]{
		{"the retiring Admin", nil, func(h *testsupport.Harness, sc valueRetirementUndo) (domain.Account, string) {
			return sc.admin, sc.token
		}},
		{"another Admin, with the retiring Admin's token", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc valueRetirementUndo) (domain.Account, string) {
			return sc.ada, sc.token
		}},
		{"someone not an Admin, with the retiring Admin's token", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc valueRetirementUndo) (domain.Account, string) {
			return sc.rita, sc.token
		}},
		{"the retiring Admin, with a made-up token", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc valueRetirementUndo) (domain.Account, string) {
			return sc.admin, "not-a-token"
		}},
		{"the retiring Admin, after the value's own Restore", domain.ErrValidation, func(h *testsupport.Harness, sc valueRetirementUndo) (domain.Account, string) {
			if err := h.Service.RestoreDimensionValue(ctx, sc.admin.ID, sc.value.ID); err != nil {
				h.T.Fatalf("RestoreDimensionValue: %v", err)
			}
			return sc.admin, sc.token
		}},
		{"the retiring Admin, after it is restored, renamed and retired again", domain.ErrValidation, func(h *testsupport.Harness, sc valueRetirementUndo) (domain.Account, string) {
			if err := h.Service.RestoreDimensionValue(ctx, sc.admin.ID, sc.value.ID); err != nil {
				h.T.Fatalf("RestoreDimensionValue: %v", err)
			}
			if _, err := h.Service.RenameDimensionValue(ctx, sc.admin.ID, sc.value.ID, "Expansion"); err != nil {
				h.T.Fatalf("RenameDimensionValue: %v", err)
			}
			if err := h.Service.RetireDimensionValue(ctx, sc.admin.ID, sc.value.ID); err != nil {
				h.T.Fatalf("RetireDimensionValue: %v", err)
			}
			return sc.admin, sc.token
		}},
		// Refused for who they are, not for a changed value.
		{"the retiring Admin, once no longer an Admin", domain.ErrNotAuthorized, func(h *testsupport.Harness, sc valueRetirementUndo) (domain.Account, string) {
			if err := h.Service.RemoveAdmin(ctx, sc.ada.ID, sc.admin.ID); err != nil {
				h.T.Fatalf("RemoveAdmin: %v", err)
			}
			return sc.admin, sc.token
		}},
		{"the retiring Admin, after UndoWindow and a second", domain.ErrValidation, func(h *testsupport.Harness, sc valueRetirementUndo) (domain.Account, string) {
			h.Clock.Advance(domain.UndoWindow + time.Second)
			return sc.admin, sc.token
		}},
	})
}
