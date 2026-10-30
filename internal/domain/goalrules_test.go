package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// departedCommand is one Goal command a person makes, run on a Service bound
// to a transaction.
type departedCommand struct {
	name string
	run  func(tx *domain.Service) error
}

// A Departed person can't act (CONTEXT.md: Departed): every command the Owner
// of the Goal they leave Ownerless would make, and creating a Goal for
// themselves, is refused them with ErrNotAuthorized once they leave, though
// each was accepted the moment before.
func TestGoalRulesRefuseADepartedOwner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	olive := h.SignIn("olive@example.com")
	nina := h.SignIn("nina@example.com")
	pat := h.SignIn("pat@example.com")
	pillar := h.CreateDimension(admin, "Pillar", "Growth")
	revenue := h.ActiveGoal(pat, "Grow revenue", "The company needs it.")
	margin := h.ActiveGoal(pat, "Grow margin", "The company needs it too.")
	ownerless := h.ActiveGoal(olive, "Ship export", "People can't leave.")
	link := h.RequestLink(olive, ownerless, revenue, "")
	if _, err := h.Service.AcceptLink(ctx, link.ID, pat.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}

	title := "Ship export, again"
	commands := []departedCommand{
		{"AssignGoalValue", func(tx *domain.Service) error {
			return tx.AssignGoalValue(ctx, olive.ID, ownerless.ID, pillar.Values[0].ID)
		}},
		{"AddDelegate", func(tx *domain.Service) error {
			return tx.AddDelegate(ctx, olive.ID, ownerless.ID, nina.ID)
		}},
		{"AddMetric", func(tx *domain.Service) error {
			_, err := tx.AddMetric(ctx, olive.ID, domain.AddMetricInput{
				GoalID: ownerless.ID, Name: "Exports", Unit: "a day", Direction: domain.MetricUp,
				Baseline: 10, Target: 100, TargetDate: h.Clock.Now().AddDate(0, 4, 0),
			})
			return err
		}},
		{"RequestLink", func(tx *domain.Service) error {
			_, err := tx.RequestLink(ctx, domain.RequestLinkInput{ChildID: ownerless.ID, ParentID: margin.ID, RequesterID: olive.ID})
			return err
		}},
		{"RemoveLink", func(tx *domain.Service) error {
			_, err := tx.RemoveLink(ctx, link.ID, olive.ID)
			return err
		}},
		{"EditTitleAndWhyItMatters", func(tx *domain.Service) error {
			_, err := tx.EditTitleAndWhyItMatters(ctx, olive.ID, ownerless.ID, &title, ownerless.WhyItMatters)
			return err
		}},
		{"CreateGoal", func(tx *domain.Service) error {
			_, err := tx.CreateGoal(ctx, olive.ID, domain.CreateGoalInput{Title: "Ship import", WhyItMatters: "People bring data.", OwnerID: olive.ID})
			return err
		}},
	}

	expectAcceptedThenRefused(t, h, commands, func() { h.MarkDeparted(admin, olive) })
}

// A Departed Delegate can neither set the Goal's values nor suggest it a
// parent, though they could the moment before they left.
func TestGoalRulesRefuseADepartedDelegate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	sam := h.SignIn("sam@example.com")
	dana := h.SignIn("dana@example.com")
	pat := h.SignIn("pat@example.com")
	pillar := h.CreateDimension(admin, "Pillar", "Growth")
	revenue := h.ActiveGoal(pat, "Grow revenue", "The company needs it.")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, dana, g.ID)

	commands := []departedCommand{
		{"AssignGoalValue", func(tx *domain.Service) error {
			return tx.AssignGoalValue(ctx, dana.ID, g.ID, pillar.Values[0].ID)
		}},
		{"SuggestParent", func(tx *domain.Service) error {
			_, err := tx.SuggestParent(ctx, dana.ID, g.ID, revenue.ID, "")
			return err
		}},
	}

	expectAcceptedThenRefused(t, h, commands, func() { h.MarkDeparted(admin, dana) })
}

// A Departed Admin can no longer set a Goal's values, edit its title, reassign
// an Ownerless Goal or suggest a parent, though they could the moment before
// they left.
func TestGoalRulesRefuseADepartedAdmin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com", "ada@example.com")
	admin := h.SignIn("admin@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	olive := h.SignIn("olive@example.com")
	nina := h.SignIn("nina@example.com")
	pat := h.SignIn("pat@example.com")
	pillar := h.CreateDimension(admin, "Pillar", "Growth")
	revenue := h.ActiveGoal(pat, "Grow revenue", "The company needs it.")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	ownerless := h.ActiveGoal(olive, "Ship export", "People can't leave.")
	h.MarkDeparted(admin, olive)

	title := "Ship search, again"
	commands := []departedCommand{
		{"AssignGoalValue", func(tx *domain.Service) error {
			return tx.AssignGoalValue(ctx, ada.ID, g.ID, pillar.Values[0].ID)
		}},
		{"EditTitleAndWhyItMatters", func(tx *domain.Service) error {
			_, err := tx.EditTitleAndWhyItMatters(ctx, ada.ID, g.ID, &title, g.WhyItMatters)
			return err
		}},
		{"ReassignGoal", func(tx *domain.Service) error {
			_, err := tx.ReassignGoal(ctx, ada.ID, ownerless.ID, nina.ID)
			return err
		}},
		{"SuggestParent", func(tx *domain.Service) error {
			_, err := tx.SuggestParent(ctx, ada.ID, g.ID, revenue.ID, "")
			return err
		}},
	}

	expectAcceptedThenRefused(t, h, commands, func() { h.MarkDeparted(admin, ada) })
}

// expectAcceptedThenRefused runs each of commands, rolled back, before and
// after depart: each must be accepted before and refused with
// ErrNotAuthorized after.
func expectAcceptedThenRefused(t *testing.T, h *testsupport.Harness, commands []departedCommand, depart func()) {
	t.Helper()
	for _, c := range commands {
		if err := runRolledBack(t, h, c); err != nil {
			t.Errorf("%s before departing: %v, want it accepted", c.name, err)
		}
	}
	depart()
	for _, c := range commands {
		if err := runRolledBack(t, h, c); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s after departing: %v, want ErrNotAuthorized", c.name, err)
		}
	}
}

// runRolledBack runs c in a transaction it then rolls back, returning what
// c answered.
func runRolledBack(t *testing.T, h *testsupport.Harness, c departedCommand) error {
	t.Helper()
	var got error
	if err := h.Service.WithinTx(t.Context(), func(tx *domain.Service) error {
		got = c.run(tx)
		return errRolledBack
	}); !errors.Is(err, errRolledBack) {
		t.Fatalf("%s: the transaction ended %v", c.name, err)
	}
	return got
}

// goalCommandCall is one Goal command, or Goal-page probe, made on goalID.
type goalCommandCall struct {
	name string
	run  func(goalID int64) error
}

// Every Goal command and Goal-page probe opens its Goal the one way
// (openGoalCommand), so a Goal that doesn't exist is ErrNotFound from each,
// even when everything else it is sent is valid.
func TestGoalCommandsAnswerAMissingGoalNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	pillar := h.CreateDimension(admin, "Pillar", "Growth")
	headcount := h.CreateField(admin, "Headcount", domain.FieldNumber, "people")
	parent := h.ActiveGoal(kim, "Grow revenue", "The company needs it.")
	missing := parent.ID + 1000
	due := h.Clock.Now().AddDate(0, 3, 0)

	calls := []goalCommandCall{
		{"SubmitCheckin", func(id int64) error {
			_, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{GoalID: id, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "On track."})
			return err
		}},
		{"SubmitNoChangeCheckin", func(id int64) error {
			_, err := h.Service.SubmitNoChangeCheckin(ctx, id, sam.ID)
			return err
		}},
		{"NoChangeRefusal", func(id int64) error { return h.Service.NoChangeRefusal(ctx, id) }},
		{"AddDelegate", func(id int64) error { return h.Service.AddDelegate(ctx, sam.ID, id, kim.ID) }},
		{"AddDelegateByEmail", func(id int64) error { return h.Service.AddDelegateByEmail(ctx, sam.ID, id, kim.Email) }},
		{"RemoveDelegate", func(id int64) error { return h.Service.RemoveDelegate(ctx, sam.ID, id, kim.ID) }},
		{"RemoveDelegateByEmail", func(id int64) error { return h.Service.RemoveDelegateByEmail(ctx, sam.ID, id, kim.Email) }},
		{"StartHandoff", func(id int64) error {
			_, err := h.Service.StartHandoff(ctx, domain.StartHandoffInput{GoalID: id, ToOwnerID: kim.ID, ActorID: sam.ID})
			return err
		}},
		{"StartHandoffByEmail", func(id int64) error {
			_, err := h.Service.StartHandoffByEmail(ctx, id, kim.Email, sam.ID)
			return err
		}},
		{"ReassignGoal", func(id int64) error {
			_, err := h.Service.ReassignGoal(ctx, admin.ID, id, kim.ID)
			return err
		}},
		{"ReassignGoalByEmail", func(id int64) error {
			_, err := h.Service.ReassignGoalByEmail(ctx, admin.ID, id, kim.Email)
			return err
		}},
		{"AssignGoalValue", func(id int64) error { return h.Service.AssignGoalValue(ctx, sam.ID, id, pillar.Values[0].ID) }},
		{"AssignGoalValueByName", func(id int64) error {
			_, err := h.Service.AssignGoalValueByName(ctx, sam.ID, id, pillar.ID, "Growth")
			return err
		}},
		{"SetGoalValues", func(id int64) error {
			return h.Service.SetGoalValues(ctx, sam.ID, id, pillar.ID, []int64{pillar.Values[0].ID})
		}},
		{"SetGoalField", func(id int64) error { return h.Service.SetGoalField(ctx, sam.ID, id, headcount.ID, "12") }},
		{"AddMetric", func(id int64) error {
			_, err := h.Service.AddMetric(ctx, sam.ID, domain.AddMetricInput{
				GoalID: id, Name: "Exports", Unit: "a day", Direction: domain.MetricUp,
				Baseline: 10, Target: 100, TargetDate: due,
			})
			return err
		}},
		{"AddMilestoneAsAuthor", func(id int64) error {
			_, err := h.Service.AddMilestoneAsAuthor(ctx, sam.ID, domain.AddMilestoneInput{GoalID: id, Name: "Beta", TargetDate: due})
			return err
		}},
		{"RequestLink", func(id int64) error {
			_, err := h.Service.RequestLink(ctx, domain.RequestLinkInput{ChildID: id, ParentID: parent.ID, RequesterID: sam.ID})
			return err
		}},
		{"SuggestParent", func(id int64) error {
			_, err := h.Service.SuggestParent(ctx, sam.ID, id, parent.ID, "")
			return err
		}},
		{"AddGoalComment", func(id int64) error {
			_, err := h.Service.AddGoalComment(ctx, sam.ID, id, "Looks good.")
			return err
		}},
		{"MarkGoalDated", func(id int64) error {
			_, err := h.Service.MarkGoalDated(ctx, sam.ID, id, due)
			return err
		}},
		{"MarkGoalOngoing", func(id int64) error {
			_, err := h.Service.MarkGoalOngoing(ctx, sam.ID, id)
			return err
		}},
		{"SetCadence", func(id int64) error {
			_, err := h.Service.SetCadence(ctx, sam.ID, id, 14)
			return err
		}},
		{"ActivateGoal", func(id int64) error {
			_, err := h.Service.ActivateGoal(ctx, sam.ID, id)
			return err
		}},
		{"DefineGoal", func(id int64) error {
			_, err := h.Service.DefineGoal(ctx, sam.ID, id, domain.DefinedGoalInput{
				WhyItMatters: "People can't find things.", Kind: domain.GoalDated, DeliveryDate: due,
				Milestones: []domain.MilestoneDefinition{{Name: "Beta", Date: due}},
			})
			return err
		}},
		{"MarkTopLevel", func(id int64) error {
			_, err := h.Service.MarkTopLevel(ctx, admin.ID, id)
			return err
		}},
		{"EditTitle", func(id int64) error {
			_, err := h.Service.EditTitle(ctx, id, "Ship search, again", sam.ID)
			return err
		}},
		{"EditWhyItMatters", func(id int64) error {
			_, err := h.Service.EditWhyItMatters(ctx, id, "People still can't find things.", sam.ID)
			return err
		}},
		{"LogDraftHighlight", func(id int64) error {
			_, err := h.Service.LogDraftHighlight(ctx, domain.LogDraftHighlightInput{GoalID: id, AuthorID: sam.ID, Note: "Search shipped to beta."})
			return err
		}},
		{"RequireGoalOwner", func(id int64) error { return h.Service.RequireGoalOwner(ctx, sam.ID, id) }},
		{"RequireCheckinAuthor", func(id int64) error { return h.Service.RequireCheckinAuthor(ctx, sam.ID, id) }},
		{"RequireGoalValueSetter", func(id int64) error { return h.Service.RequireGoalValueSetter(ctx, sam.ID, id) }},
	}

	for _, c := range calls {
		if err := c.run(missing); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s on a missing Goal = %v, want ErrNotFound", c.name, err)
		}
	}
}

// A Goal that can't be read for want of the database is neither missing nor
// refused: the probes answer the failure as it is.
func TestGoalProbesAnswerADatabaseFailureAsItIs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	if err := h.DB.Close(); err != nil {
		t.Fatalf("close the database: %v", err)
	}

	for _, c := range []goalCommandCall{
		{"RequireGoalValueSetter", func(id int64) error { return h.Service.RequireGoalValueSetter(ctx, sam.ID, id) }},
		{"RequireGoalOwner", func(id int64) error { return h.Service.RequireGoalOwner(ctx, sam.ID, id) }},
	} {
		err := c.run(g.ID)
		if err == nil || errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s with the database closed = %v, want the failure, neither ErrNotFound nor ErrValidation", c.name, err)
		}
	}
}

// A command taking values its caller couldn't parse judges who is asking
// first: an Account with no right to the Goal is refused for that, and
// learns nothing of what's wrong with what they sent.
func TestGoalCommandsRefuseUnparsedInputFromAnyoneWithNoRight(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	g := h.CreateGoal(sam, "Ship search", "People can't find things.")
	due := h.Clock.Now().AddDate(0, 3, 0)
	milestone, err := h.Service.AddMilestoneAsAuthor(ctx, sam.ID, domain.AddMilestoneInput{GoalID: g.ID, Name: "Beta", TargetDate: due})
	if err != nil {
		t.Fatalf("AddMilestoneAsAuthor: %v", err)
	}
	metric, err := h.Service.AddMetric(ctx, sam.ID, validMetric(g.ID))
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	unparsed := []*domain.InputError{{Input: "target_date", Message: `"soon" isn't a date`}}

	for _, c := range []struct {
		name string
		run  func() error
	}{
		{"AddMilestoneAsAuthor", func() error {
			_, err := h.Service.AddMilestoneAsAuthor(ctx, kim.ID, domain.AddMilestoneInput{GoalID: g.ID, Name: "Launch", Unparsed: unparsed})
			return err
		}},
		{"EditMilestone", func() error {
			_, err := h.Service.EditMilestone(ctx, kim.ID, domain.EditMilestoneInput{MilestoneID: milestone.ID, Name: "Beta", Unparsed: unparsed})
			return err
		}},
		{"EditMetric", func() error {
			in := validMetric(g.ID)
			_, err := h.Service.EditMetric(ctx, kim.ID, domain.EditMetricInput{
				MetricID: metric.ID, Name: in.Name, Unit: in.Unit, Direction: in.Direction,
				Baseline: in.Baseline, Target: in.Target, Unparsed: unparsed,
			})
			return err
		}},
		{"DefineGoal", func() error {
			_, err := h.Service.DefineGoal(ctx, kim.ID, g.ID, domain.DefinedGoalInput{
				WhyItMatters: g.WhyItMatters, Kind: domain.GoalDated, Unparsed: unparsed,
			})
			return err
		}},
	} {
		if err := c.run(); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s of unparsed input by someone with no right to the Goal = %v, want ErrNotAuthorized", c.name, err)
		}
	}
}

// Once a Goal leaves Proposed its Owner can't mark it Dated outside a
// Check-in, and the refusal's article agrees with its Lifecycle.
func TestMarkGoalDatedRefusesAnOwnerOnceTheGoalIsActiveOrDone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	sam := h.SignIn("sam@example.com")
	active := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	done := h.ActiveGoal(sam, "Ship export", "People can't leave.")
	h.EndGoalInCheckin(sam, done.ID, domain.LifecycleDone)
	due := h.Clock.Now().AddDate(0, 6, 0)

	for _, c := range []struct {
		name   string
		goalID int64
		want   string
	}{
		{"Active", active.ID, "an Active Goal's delivery date changes only in a Check-in, with a reason"},
		{"Done", done.ID, "a Done Goal's delivery date changes only in a Check-in, with a reason"},
	} {
		_, err := h.Service.MarkGoalDated(ctx, sam.ID, c.goalID, due)
		if !errors.Is(err, domain.ErrValidation) || domain.Reason(err) != c.want {
			t.Errorf("MarkGoalDated on a %s Goal = %v, want ErrValidation %q", c.name, err, c.want)
		}
	}
}

// reasonedCommand is one Goal command and the reason it is refused with.
type reasonedCommand struct {
	name string
	run  func() error
	want string
}

// expectRefusedWithReasons runs each of commands, each to be refused with
// ErrNotAuthorized saying its own reason.
func expectRefusedWithReasons(t *testing.T, commands []reasonedCommand) {
	t.Helper()
	for _, c := range commands {
		err := c.run()
		if !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s: %v, want ErrNotAuthorized", c.name, err)
			continue
		}
		if got := domain.Reason(err); got != c.want {
			t.Errorf("%s says %q, want %q", c.name, got, c.want)
		}
	}
}

// Setting a Goal's Fields, its Goal table cells or its Dimension values is
// refused with the reason the value-setter rule judges: a Departed Delegate is
// told a Departed person can't act, and a present reader with no rights who
// may.
func TestGoalValueSettersAreRefusedWithTheRulesReason(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	sam := h.SignIn("sam@example.com")
	dana := h.SignIn("dana@example.com")
	rita := h.SignIn("rita@example.com")
	pillar := h.CreateDimension(admin, "Pillar", "Growth")
	budget := h.CreateField(admin, "Budget", domain.FieldNumber, "$")
	g := h.ActiveGoal(sam, "Theirs", "People can't find things.")
	h.AddDelegate(sam, dana, g.ID)
	h.MarkDeparted(admin, dana)

	commands := func(actorID int64) (field, cell, values func() error) {
		return func() error {
				return h.Service.SetGoalField(ctx, actorID, g.ID, budget.ID, "10")
			}, func() error {
				return h.Service.EditGoalValues(ctx, actorID, []domain.ValueEdit{{GoalID: g.ID, FieldID: budget.ID, Value: "10"}})
			}, func() error {
				_, err := h.Service.SetGoalDimensionValues(ctx, actorID, domain.DimensionValuesEdit{GoalID: g.ID, DimensionID: pillar.ID, ValueIDs: []int64{pillar.Values[0].ID}})
				return err
			}
	}
	danaField, danaCell, danaValues := commands(dana.ID)
	ritaField, ritaCell, _ := commands(rita.ID)
	expectRefusedWithReasons(t, []reasonedCommand{
		{"SetGoalField by a Departed Delegate", danaField, "a Departed person can't set a Goal's Fields"},
		{"EditGoalValues by a Departed Delegate", danaCell, "a Departed person can't set the values of Theirs"},
		{"SetGoalDimensionValues by a Departed Delegate", danaValues, "a Departed person can't set a Goal's Dimension values"},
		{"SetGoalField by a reader", ritaField, "only the Owner, a Delegate or an Admin may set a Goal's Fields"},
		{"EditGoalValues by a reader", ritaCell, "only the Owner, a Delegate or an Admin may set the values of Theirs"},
	})
}

// Changing a Goal's Milestones, or logging, seeing, editing or deleting its
// Draft Highlights, is refused with the reason the rule for who writes its
// Check-ins judges: a Departed Delegate is told a Departed person can't act,
// and a present reader with no rights who may.
func TestMilestoneAndDraftHighlightWritersAreRefusedWithTheRulesReason(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	sam := h.SignIn("sam@example.com")
	dana := h.SignIn("dana@example.com")
	rita := h.SignIn("rita@example.com")
	g := h.CreateGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, dana, g.ID)
	due := h.Clock.Now().AddDate(0, 2, 0)
	m, err := h.Service.AddMilestoneAsAuthor(ctx, sam.ID, domain.AddMilestoneInput{GoalID: g.ID, Name: "Beta", TargetDate: due})
	if err != nil {
		t.Fatalf("AddMilestoneAsAuthor: %v", err)
	}
	draft := h.LogDraftHighlight(dana, g.ID, "", "Indexing is fast.")
	h.MarkDeparted(admin, dana)

	commands := func(actorID int64) []func() error {
		return []func() error{
			func() error {
				_, err := h.Service.AddMilestoneAsAuthor(ctx, actorID, domain.AddMilestoneInput{GoalID: g.ID, Name: "Launch", TargetDate: due})
				return err
			},
			func() error {
				_, err := h.Service.EditMilestone(ctx, actorID, domain.EditMilestoneInput{MilestoneID: m.ID, Name: "Beta, again", TargetDate: due})
				return err
			},
			func() error {
				_, err := h.Service.LogDraftHighlight(ctx, domain.LogDraftHighlightInput{GoalID: g.ID, AuthorID: actorID, Note: "Ranking is better."})
				return err
			},
			func() error {
				_, err := h.Service.PendingDraftHighlights(ctx, actorID, g.ID)
				return err
			},
			func() error {
				_, err := h.Service.EditDraftHighlight(ctx, actorID, draft.ID, "", "Indexing is faster.")
				return err
			},
			func() error {
				_, err := h.Service.DeleteDraftHighlight(ctx, actorID, draft.ID)
				return err
			},
		}
	}
	names := []string{"AddMilestoneAsAuthor", "EditMilestone", "LogDraftHighlight", "PendingDraftHighlights", "EditDraftHighlight", "DeleteDraftHighlight"}
	const (
		departedMilestones = "a Departed person can't change this Goal's Milestones"
		departedDrafts     = "a Departed person can't log, see, edit or delete this Goal's Draft Highlights"
		readerMilestones   = "only the Owner or a Delegate may change this Goal's Milestones"
		readerDrafts       = "only the Owner or a Delegate may log, see, edit or delete this Goal's Draft Highlights"
	)
	var want []reasonedCommand
	for _, who := range []struct {
		name               string
		actorID            int64
		milestones, drafts string
	}{
		{"a Departed Delegate", dana.ID, departedMilestones, departedDrafts},
		{"a reader", rita.ID, readerMilestones, readerDrafts},
	} {
		for i, run := range commands(who.actorID) {
			reason := who.drafts
			if i < 2 {
				reason = who.milestones
			}
			want = append(want, reasonedCommand{names[i] + " by " + who.name, run, reason})
		}
	}
	expectRefusedWithReasons(t, want)
}
