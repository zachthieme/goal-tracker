package domain_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Goal page's loader shows any signed-in viewer any Goal, as ViewGoal
// does, and refuses a Goal that doesn't exist with ErrNotFound, as ViewGoal
// does (ADR 0010).
func TestGoalPageRefusesAGoalViewGoalRefuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	rita := h.SignIn("rita@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")

	if _, err := h.Service.ViewGoal(ctx, g.ID+1000); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ViewGoal of a missing Goal: err = %v, want ErrNotFound", err)
	}
	if _, err := h.Service.GoalPage(ctx, rita.ID, g.ID+1000); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GoalPage of a missing Goal: err = %v, want ErrNotFound", err)
	}
	p, err := h.Service.GoalPage(ctx, rita.ID, g.ID)
	if err != nil {
		t.Fatalf("GoalPage for a reader with no rights: %v", err)
	}
	if p.Goal.Title != "Ship search" {
		t.Errorf("GoalPage's Goal = %q, want Ship search", p.Goal.Title)
	}
}

// The Goal page's loader offers each viewer what the separate permission
// rules allow: the Owner and a Delegate may check in and set values, an
// Admin may only set values, and a reader with no rights may do neither; only
// those who may check in see the pending Draft Highlights.
func TestGoalPagePermitsWhatTheRulesAllow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	sam := h.SignIn("sam@example.com")
	morgan := h.SignIn("morgan@example.com")
	rita := h.SignIn("rita@example.com")
	admin := h.SignIn("admin@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, morgan, g.ID)
	if _, err := h.Service.LogDraftHighlight(ctx, domain.LogDraftHighlightInput{
		GoalID: g.ID, AuthorID: morgan.ID, Kind: domain.HighlightInsight, Note: "Search is slow on mobile.",
	}); err != nil {
		t.Fatalf("LogDraftHighlight: %v", err)
	}

	for _, v := range []struct {
		name                   string
		viewer                 domain.Account
		checkIn, setValues     bool
		draftHighlightsVisible int
	}{
		{"the Owner", sam, true, true, 1},
		{"a Delegate", morgan, true, true, 1},
		{"an Admin", admin, false, true, 0},
		{"a reader", rita, false, false, 0},
	} {
		p, err := h.Service.GoalPage(ctx, v.viewer.ID, g.ID)
		if err != nil {
			t.Fatalf("GoalPage for %s: %v", v.name, err)
		}
		if p.CanCheckin != v.checkIn {
			t.Errorf("CanCheckin for %s = %t, want %t", v.name, p.CanCheckin, v.checkIn)
		}
		if p.Permissions.SetValues != v.setValues {
			t.Errorf("Permissions.SetValues for %s = %t, want %t", v.name, p.Permissions.SetValues, v.setValues)
		}
		if len(p.DraftHighlights) != v.draftHighlightsVisible {
			t.Errorf("%s sees %d Draft Highlights, want %d", v.name, len(p.DraftHighlights), v.draftHighlightsVisible)
		}
	}
}

// The Goal page's loader says what each viewer may do with the Goal, by the
// rule each command enforces: on an Active Goal the Owner may do everything
// but an Admin's own acts and suggest a parent, a Delegate what a Check-in
// author may, set values and suggest a parent, an Admin set values, edit, hand
// off, mark the Owner departed, mark Top-level and suggest a parent, and a
// reader suggest a parent. Only while the Goal is Proposed may its Owner
// define and activate it, and its Owner and Delegates edit its Milestones, and
// no one checks in on it yet. Once the Goal is Done no one may check in, add a
// Milestone, log a Draft Highlight or suggest a parent, the Owner included.
// Its Owner and Delegates write its Check-ins in every Lifecycle.
func TestGoalPagePermissionsFollowEachCommandsRule(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	sam := h.SignIn("sam@example.com")
	morgan := h.SignIn("morgan@example.com")
	rita := h.SignIn("rita@example.com")
	admin := h.SignIn("admin@example.com")
	proposed := h.CreateGoal(sam, "Ship billing", "People can't pay.")
	active := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	done := h.ActiveGoal(sam, "Ship sign-in", "People can't get in.")
	for _, g := range []domain.Goal{proposed, active, done} {
		h.AddDelegate(sam, morgan, g.ID)
	}
	h.EndGoalInCheckin(sam, done.ID, domain.LifecycleDone)

	checksIn := domain.CheckinPermissions{
		CheckIn: true, KeepLifecycle: true,
		LifecycleChanges: []string{domain.LifecycleOnHold, domain.LifecycleDone, domain.LifecycleCancelled},
	}
	owner := domain.GoalPermissions{
		CheckinPermissions: checksIn, WritesCheckins: true, AddMilestone: true, LogDraftHighlight: true, SetValues: true,
		ManageDelegates: true, LinkParent: true, EditMetrics: true, DecideSuggestions: true,
		EditTitleAndWhyItMatters: true, HandOff: true,
	}
	delegate := domain.GoalPermissions{
		CheckinPermissions: checksIn, WritesCheckins: true, AddMilestone: true, LogDraftHighlight: true, SetValues: true,
		SuggestParent: true,
	}
	anAdmin := domain.GoalPermissions{
		SetValues: true, EditTitleAndWhyItMatters: true, HandOff: true,
		MarkOwnerDeparted: true, MarkTopLevel: true, SuggestParent: true,
	}
	reader := domain.GoalPermissions{SuggestParent: true}
	ownerOfProposed := owner
	ownerOfProposed.CheckinPermissions = domain.CheckinPermissions{}
	ownerOfProposed.DefineGoal, ownerOfProposed.Activate, ownerOfProposed.EditMilestones = true, true, true
	delegateOnProposed := delegate
	delegateOnProposed.CheckinPermissions, delegateOnProposed.EditMilestones = domain.CheckinPermissions{}, true
	ownerOfDone := owner
	ownerOfDone.CheckinPermissions, ownerOfDone.AddMilestone, ownerOfDone.LogDraftHighlight = domain.CheckinPermissions{}, false, false
	adminOnDone := anAdmin
	adminOnDone.SuggestParent = false
	for _, v := range []struct {
		name   string
		viewer domain.Account
		goal   domain.Goal
		want   domain.GoalPermissions
	}{
		{"the Owner of the Proposed Goal", sam, proposed, ownerOfProposed},
		{"a Delegate on the Proposed Goal", morgan, proposed, delegateOnProposed},
		{"an Admin on the Proposed Goal", admin, proposed, anAdmin},
		{"a reader of the Proposed Goal", rita, proposed, reader},
		{"the Owner of the Active Goal", sam, active, owner},
		{"a Delegate on the Active Goal", morgan, active, delegate},
		{"an Admin on the Active Goal", admin, active, anAdmin},
		{"a reader of the Active Goal", rita, active, reader},
		{"the Owner of the Done Goal", sam, done, ownerOfDone},
		{"a Delegate on the Done Goal", morgan, done, domain.GoalPermissions{WritesCheckins: true, SetValues: true}},
		{"an Admin on the Done Goal", admin, done, adminOnDone},
		{"a reader of the Done Goal", rita, done, domain.GoalPermissions{}},
	} {
		p, err := h.Service.GoalPage(ctx, v.viewer.ID, v.goal.ID)
		if err != nil {
			t.Fatalf("GoalPage for %s: %v", v.name, err)
		}
		if !reflect.DeepEqual(p.Permissions, v.want) {
			t.Errorf("Permissions for %s = %+v, want %+v", v.name, p.Permissions, v.want)
		}
	}
}

// goalCommand is one Goal command the parity table runs: allowed reads
// whether the Goal page offers it, and run makes it, as viewer on g, through
// tx.
type goalCommand struct {
	name    string
	allowed func(domain.GoalPermissions) bool
	run     func(tx *domain.Service, viewer domain.Account, g domain.Goal) error
}

// errRolledBack ends the transaction a parity run makes its command in, so
// each run starts from the same Goals.
var errRolledBack = errors.New("rolled back")

// For each Goal command, each kind of viewer — the Owner, a Delegate, an
// Admin, anyone else, a Departed Delegate and a Departed Admin — and each
// Lifecycle, and on an Ownerless Goal, the Goal page offers the command
// exactly when the command, made with input it would otherwise take, doesn't
// refuse for who the viewer is or for the Goal's Lifecycle or state: what it
// offers is accepted, and what it doesn't is refused with ErrNotAuthorized or
// ErrValidation. A command no viewer was both offered and had accepted on some
// Goal fails the test, since its refusals would then prove nothing of its
// rule. Each Lifecycle a Check-in may move the Goal to is a command of its
// own, and deciding a Parent suggestion is made where the Goal has an open
// one.
func TestGoalPageOffersExactlyWhatEachCommandAccepts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com", "ada@example.com")
	sam := h.SignIn("sam@example.com")
	morgan := h.SignIn("morgan@example.com")
	admin := h.SignIn("admin@example.com")
	ada := h.SignIn("ada@example.com")
	rita := h.SignIn("rita@example.com")
	dana := h.SignIn("dana@example.com")
	nina := h.SignIn("nina@example.com")
	olive := h.SignIn("olive@example.com")
	pat := h.SignIn("pat@example.com")
	suggested := h.ActiveGoal(pat, "Grow revenue", "The company needs it.")
	parent := h.ActiveGoal(pat, "Grow margin", "The company needs it too.")
	pillar := h.CreateDimension(admin, "Pillar", "Growth")

	proposed := h.CreateGoal(sam, "Ship billing", "People can't pay.")
	if _, err := h.Service.MarkGoalDated(ctx, sam.ID, proposed.ID, h.Clock.Now().AddDate(0, 6, 0)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	if _, err := h.Service.AddMilestone(ctx, sam.ID, domain.AddMilestoneInput{GoalID: proposed.ID, Name: "Beta", TargetDate: h.Clock.Now().AddDate(0, 3, 0)}); err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	active := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	onHold := h.ActiveGoal(sam, "Ship sharing", "People can't share.")
	done := h.ActiveGoal(sam, "Ship sign-in", "People can't get in.")
	cancelled := h.ActiveGoal(sam, "Ship chat", "People can't talk.")
	ownerless := h.ActiveGoal(olive, "Ship export", "People can't leave.")
	goals := []domain.Goal{proposed, active, onHold, done, cancelled, ownerless}
	later := h.Clock.Now().AddDate(0, 4, 0)
	metrics := map[int64]domain.Metric{}
	for _, g := range goals {
		m, err := h.Service.AddMetric(ctx, g.Owner.ID, domain.AddMetricInput{
			GoalID: g.ID, Name: "Uses", Unit: "a day", Direction: domain.MetricUp, Baseline: 10, Target: 100, TargetDate: later,
		})
		if err != nil {
			t.Fatalf("AddMetric for %s: %v", g.Title, err)
		}
		metrics[g.ID] = m
		h.AddDelegate(g.Owner, morgan, g.ID)
		h.AddDelegate(g.Owner, dana, g.ID)
		h.SuggestParent(rita, g, suggested, "")
		if g.Lifecycle == domain.LifecycleActive {
			h.Checkin(g.Owner, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
		}
	}
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: onHold.ID, AuthorID: sam.ID, Status: "Pausing.", Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Waiting on legal.",
	}); err != nil {
		t.Fatalf("SubmitCheckin On Hold: %v", err)
	}
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: done.ID, AuthorID: sam.ID, Status: "Wrapping this up.", Lifecycle: domain.LifecycleDone, Outcome: "It shipped.",
		Readings: []domain.MetricReadingInput{{MetricID: metrics[done.ID].ID, Value: 100}}, // a Done Goal's Metrics each need a final value
	}); err != nil {
		t.Fatalf("SubmitCheckin to Done: %v", err)
	}
	h.EndGoalInCheckin(sam, cancelled.ID, domain.LifecycleCancelled)
	h.MarkDeparted(admin, dana)
	h.MarkDeparted(admin, olive)
	h.MarkDeparted(admin, ada)

	commands := []goalCommand{
		{"CheckIn", func(p domain.GoalPermissions) bool { return p.CheckIn }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			in := domain.SubmitCheckinInput{GoalID: g.ID, AuthorID: v.ID, Health: domain.HealthGreen, Status: "On track."}
			if g.Lifecycle == domain.LifecycleOnHold {
				in.Lifecycle = domain.LifecycleActive // an On Hold Goal's Check-in resumes or cancels it
			}
			_, err := tx.SubmitCheckin(ctx, in)
			return err
		}},
		{"KeepLifecycle", func(p domain.GoalPermissions) bool { return p.KeepLifecycle }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			_, err := tx.SubmitCheckin(ctx, domain.SubmitCheckinInput{GoalID: g.ID, AuthorID: v.ID, Health: domain.HealthGreen, Status: "On track."})
			return err
		}},
		// WritesCheckins holds whatever the Goal's Lifecycle, so it is checked
		// against a Check-in on an Active Goal the viewer stands to as they do
		// to g: g itself when it is Active, else sam's Active one.
		{"WritesCheckins", func(p domain.GoalPermissions) bool { return p.WritesCheckins }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			on := g
			if g.Lifecycle != domain.LifecycleActive {
				on = active
			}
			_, err := tx.SubmitCheckin(ctx, domain.SubmitCheckinInput{GoalID: on.ID, AuthorID: v.ID, Health: domain.HealthGreen, Status: "On track."})
			return err
		}},
		{"NoChange", func(p domain.GoalPermissions) bool { return p.NoChange }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			_, err := tx.SubmitNoChangeCheckin(ctx, g.ID, v.ID)
			return err
		}},
		{"AddMilestone", func(p domain.GoalPermissions) bool { return p.AddMilestone }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			_, err := tx.AddMilestoneAsAuthor(ctx, v.ID, domain.AddMilestoneInput{GoalID: g.ID, Name: "GA", TargetDate: later})
			return err
		}},
		{"EditMilestones", func(p domain.GoalPermissions) bool { return p.EditMilestones }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			ms, err := tx.ListMilestones(ctx, g.ID)
			if err != nil || len(ms) == 0 {
				t.Fatalf("ListMilestones for %s: %v %v", g.Title, ms, err)
			}
			_, err = tx.EditMilestone(ctx, v.ID, domain.EditMilestoneInput{MilestoneID: ms[0].ID, Name: "Public beta", TargetDate: ms[0].TargetDate})
			return err
		}},
		{"LogDraftHighlight", func(p domain.GoalPermissions) bool { return p.LogDraftHighlight }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			_, err := tx.LogDraftHighlight(ctx, domain.LogDraftHighlightInput{GoalID: g.ID, AuthorID: v.ID, Kind: domain.HighlightInsight, Note: "Slow on mobile."})
			return err
		}},
		{"SetValues", func(p domain.GoalPermissions) bool { return p.SetValues }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			return tx.AssignGoalValue(ctx, v.ID, g.ID, pillar.Values[0].ID)
		}},
		{"ManageDelegates", func(p domain.GoalPermissions) bool { return p.ManageDelegates }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			return tx.AddDelegate(ctx, v.ID, g.ID, nina.ID)
		}},
		{"LinkParent", func(p domain.GoalPermissions) bool { return p.LinkParent }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			_, err := tx.RequestLink(ctx, domain.RequestLinkInput{ChildID: g.ID, ParentID: parent.ID, RequesterID: v.ID})
			return err
		}},
		{"EditMetrics", func(p domain.GoalPermissions) bool { return p.EditMetrics }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			_, err := tx.AddMetric(ctx, v.ID, domain.AddMetricInput{
				GoalID: g.ID, Name: "Searches", Unit: "a day", Direction: domain.MetricUp, Baseline: 10, Target: 100, TargetDate: later,
			})
			return err
		}},
		{"EditMetric", func(p domain.GoalPermissions) bool { return p.EditMetrics }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			m := metrics[g.ID]
			_, err := tx.EditMetric(ctx, v.ID, domain.EditMetricInput{
				MetricID: m.ID, Name: "Daily uses", Unit: m.Unit, Direction: m.Direction, Baseline: m.Baseline, Target: m.Target, TargetDate: m.TargetDate,
			})
			return err
		}},
		{"DefineGoal", func(p domain.GoalPermissions) bool { return p.DefineGoal }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			_, err := tx.DefineGoal(ctx, v.ID, g.ID, domain.DefinedGoalInput{
				Title: g.Title, WhyItMatters: g.WhyItMatters, Kind: domain.GoalDated, DeliveryDate: later,
			})
			return err
		}},
		{"Activate", func(p domain.GoalPermissions) bool { return p.Activate }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			_, err := tx.ActivateGoal(ctx, v.ID, g.ID)
			return err
		}},
		{"SuggestParent", func(p domain.GoalPermissions) bool { return p.SuggestParent }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			_, err := tx.SuggestParent(ctx, v.ID, g.ID, parent.ID, "")
			return err
		}},
		{"EditTitleAndWhyItMatters", func(p domain.GoalPermissions) bool { return p.EditTitleAndWhyItMatters }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			title := g.Title + ", again"
			_, err := tx.EditTitleAndWhyItMatters(ctx, v.ID, g.ID, &title, g.WhyItMatters)
			return err
		}},
		{"HandOff", func(p domain.GoalPermissions) bool { return p.HandOff }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			_, err := tx.StartHandoff(ctx, domain.StartHandoffInput{GoalID: g.ID, ToOwnerID: nina.ID, ActorID: v.ID})
			return err
		}},
		{"Reassign", func(p domain.GoalPermissions) bool { return p.Reassign }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			_, err := tx.ReassignGoal(ctx, v.ID, g.ID, nina.ID)
			return err
		}},
		{"MarkOwnerDeparted", func(p domain.GoalPermissions) bool { return p.MarkOwnerDeparted }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			return tx.MarkDeparted(ctx, v.ID, g.Owner.ID)
		}},
		{"MarkOwnerReturned", func(p domain.GoalPermissions) bool { return p.MarkOwnerReturned }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			return tx.MarkReturned(ctx, v.ID, g.Owner.ID)
		}},
		{"MarkTopLevel", func(p domain.GoalPermissions) bool { return p.MarkTopLevel }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			_, err := tx.MarkTopLevel(ctx, v.ID, g.ID)
			return err
		}},
	}
	for _, to := range []string{domain.LifecycleActive, domain.LifecycleOnHold, domain.LifecycleDone, domain.LifecycleCancelled} {
		commands = append(commands, goalCommand{"LifecycleChanges to " + to, func(p domain.GoalPermissions) bool {
			return slices.Contains(p.LifecycleChanges, to)
		}, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
			if g.Lifecycle == to {
				return domain.Refuse(domain.ErrValidation, "a Check-in naming its own Lifecycle moves it nowhere")
			}
			_, err := tx.SubmitCheckin(ctx, domain.SubmitCheckinInput{
				GoalID: g.ID, AuthorID: v.ID, Health: domain.HealthGreen, Status: "Moving on.",
				Lifecycle: to, LifecycleReason: "Plans changed.", Outcome: "It shipped.",
				Readings: []domain.MetricReadingInput{{MetricID: metrics[g.ID].ID, Value: 100}},
			})
			return err
		}})
	}
	commands = append(commands, goalCommand{"DecideSuggestions", func(p domain.GoalPermissions) bool { return p.DecideSuggestions }, func(tx *domain.Service, v domain.Account, g domain.Goal) error {
		all, err := tx.ParentSuggestions(ctx, g.ID)
		if err != nil {
			t.Fatalf("ParentSuggestions: %v", err)
		}
		for _, sg := range all {
			if sg.Status == domain.SuggestionOpen {
				_, err := tx.DeclineParentSuggestion(ctx, v.ID, sg.ID)
				return err
			}
		}
		return errNoOpenSuggestion
	}})

	viewers := []struct {
		name   string
		viewer func(g domain.Goal) domain.Account
	}{
		{"the Owner", func(g domain.Goal) domain.Account { return g.Owner }},
		{"a Delegate", func(domain.Goal) domain.Account { return morgan }},
		{"an Admin", func(domain.Goal) domain.Account { return admin }},
		{"anyone else", func(domain.Goal) domain.Account { return rita }},
		{"a Departed Delegate", func(domain.Goal) domain.Account { return dana }},
		{"a Departed Admin", func(domain.Goal) domain.Account { return ada }},
	}
	// accepted are the commands some viewer was offered on some Goal and the
	// command then accepted: each must be, or its refusals elsewhere could be
	// of its input rather than by its rule.
	accepted := map[string]bool{}
	for _, g := range goals {
		state := g.Lifecycle
		if g.ID == ownerless.ID {
			state = "Ownerless " + state
		}
		for _, who := range viewers {
			viewer := who.viewer(g)
			p, err := h.Service.GoalPage(ctx, viewer.ID, g.ID)
			if err != nil {
				t.Fatalf("GoalPage for %s of the %s Goal: %v", who.name, state, err)
			}
			// A Departed person can't act (CONTEXT.md: Departed), so is
			// offered nothing: the Owner of the Ownerless Goal, the Departed
			// Delegate and the Departed Admin alike.
			if p.Viewer.Departed && !reflect.DeepEqual(p.Permissions, domain.GoalPermissions{}) {
				t.Errorf("%s, Departed, is offered %+v on the %s Goal, want nothing", who.name, p.Permissions, state)
			}
			for _, c := range commands {
				var got error
				if err := h.Service.WithinTx(ctx, func(tx *domain.Service) error {
					got = c.run(tx, viewer, p.Goal)
					return errRolledBack
				}); !errors.Is(err, errRolledBack) {
					t.Fatalf("%s by %s on the %s Goal: the transaction ended %v", c.name, who.name, state, err)
				}
				if errors.Is(got, errNoOpenSuggestion) {
					continue
				}
				refused := errors.Is(got, domain.ErrNotAuthorized) || errors.Is(got, domain.ErrValidation)
				switch offered := c.allowed(p.Permissions); {
				case offered && got == nil:
					accepted[c.name] = true
				case offered && got != nil:
					t.Errorf("%s by %s on the %s Goal: the page offers it, but the command refused: %v", c.name, who.name, state, got)
				case !offered && !refused:
					t.Errorf("%s by %s on the %s Goal: the page doesn't offer it, but the command answered %v", c.name, who.name, state, got)
				}
			}
		}
	}
	for _, c := range commands {
		if !accepted[c.name] {
			t.Errorf("%s is offered and accepted for no viewer on any Goal, so its refusals prove nothing of its rule", c.name)
		}
	}
}

// errNoOpenSuggestion marks a parity run of deciding a Parent suggestion on a
// Goal with none open, which has nothing to decide.
var errNoOpenSuggestion = errors.New("no open suggestion")

// The Goal page lists the open Parent suggestions its viewer sees: every one
// for the Owner, who decides them, and only their own, to withdraw, for
// whoever suggested; a Delegate who suggested none sees none, and a declined
// one is listed for no one, though it stays in the Goal's History.
func TestGoalPageListsTheOpenSuggestionsItsViewerSees(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	rita := h.SignIn("rita@example.com")
	pat := h.SignIn("pat@example.com")
	morgan := h.SignIn("morgan@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, morgan, g.ID)
	revenue := h.ActiveGoal(pat, "Grow revenue", "The company needs it.")
	margin := h.ActiveGoal(pat, "Grow margin", "The company needs it too.")
	trust := h.ActiveGoal(pat, "Earn trust", "Customers stay.")
	ritas := h.SuggestParent(rita, g, revenue, "")
	pats := h.SuggestParent(pat, g, margin, "")
	declined := h.SuggestParent(rita, g, trust, "")
	if _, err := h.Service.DeclineParentSuggestion(ctx, sam.ID, declined.ID); err != nil {
		t.Fatalf("DeclineParentSuggestion: %v", err)
	}

	for _, v := range []struct {
		name   string
		viewer domain.Account
		want   []int64
	}{
		{"the Owner", sam, []int64{ritas.ID, pats.ID}},
		{"one who suggested", rita, []int64{ritas.ID}},
		{"another who suggested", pat, []int64{pats.ID}},
		{"a Delegate", morgan, nil},
	} {
		p, err := h.Service.GoalPage(ctx, v.viewer.ID, g.ID)
		if err != nil {
			t.Fatalf("GoalPage for %s: %v", v.name, err)
		}
		var got []int64
		for _, sg := range p.OpenSuggestions {
			got = append(got, sg.ID)
		}
		if !slices.Equal(got, v.want) {
			t.Errorf("OpenSuggestions for %s = %v, want %v", v.name, got, v.want)
		}
		if len(p.Suggestions) != 3 {
			t.Errorf("Suggestions for %s list %d, want all 3 for the History", v.name, len(p.Suggestions))
		}
	}
}

// On an Ownerless Goal an Admin may reassign it and mark its Owner returned,
// but not hand it off or mark its Owner departed again.
func TestGoalPagePermitsAnAdminToReassignAnOwnerlessGoal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	sam := h.SignIn("sam@example.com")
	admin := h.SignIn("admin@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.MarkDeparted(admin, sam)

	p, err := h.Service.GoalPage(ctx, admin.ID, g.ID)
	if err != nil {
		t.Fatalf("GoalPage: %v", err)
	}
	want := domain.GoalPermissions{
		SetValues: true, EditTitleAndWhyItMatters: true, Reassign: true,
		MarkOwnerReturned: true, MarkTopLevel: true, SuggestParent: true,
	}
	if !reflect.DeepEqual(p.Permissions, want) {
		t.Errorf("Permissions for an Admin on an Ownerless Goal = %+v, want %+v", p.Permissions, want)
	}
}

// The Goal page's loader says, for each of the Goal's parent and child links,
// whether its viewer may remove it, by RemoveLink's rule: the Owner of either
// linked Goal may, and a Delegate, an Admin, anyone else and an Owner who has
// Departed may not.
func TestGoalPageSaysWhichLinksItsViewerMayRemove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	chris := h.SignIn("chris@example.com")
	morgan := h.SignIn("morgan@example.com")
	rita := h.SignIn("rita@example.com")
	admin := h.SignIn("admin@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	parent := h.ActiveGoal(pat, "Grow revenue", "We need to grow.")
	child := h.ActiveGoal(chris, "Index the catalog", "Search needs an index.")
	h.AddDelegate(sam, morgan, g.ID)
	up := h.RequestLink(sam, g, parent, "")
	if _, err := h.Service.AcceptLink(ctx, up.ID, pat.ID); err != nil {
		t.Fatalf("AcceptLink of the parent link: %v", err)
	}
	down := h.RequestLink(chris, child, g, "")
	if _, err := h.Service.AcceptLink(ctx, down.ID, sam.ID); err != nil {
		t.Fatalf("AcceptLink of the child link: %v", err)
	}

	removable := func(t *testing.T, name string, viewer domain.Account, want map[int64]bool) {
		t.Helper()
		p, err := h.Service.GoalPage(ctx, viewer.ID, g.ID)
		if err != nil {
			t.Fatalf("GoalPage for %s: %v", name, err)
		}
		if !reflect.DeepEqual(p.RemovableLinks, want) {
			t.Errorf("RemovableLinks for %s = %v, want %v", name, p.RemovableLinks, want)
		}
	}
	for _, v := range []struct {
		name   string
		viewer domain.Account
		want   map[int64]bool
	}{
		{"the Goal's Owner", sam, map[int64]bool{up.ID: true, down.ID: true}},
		{"the parent's Owner", pat, map[int64]bool{up.ID: true, down.ID: false}},
		{"the child's Owner", chris, map[int64]bool{up.ID: false, down.ID: true}},
		{"a Delegate", morgan, map[int64]bool{up.ID: false, down.ID: false}},
		{"an Admin", admin, map[int64]bool{up.ID: false, down.ID: false}},
		{"a stranger", rita, map[int64]bool{up.ID: false, down.ID: false}},
	} {
		removable(t, v.name, v.viewer, v.want)
	}
	h.MarkDeparted(admin, sam)
	removable(t, "the Goal's Departed Owner", sam, map[int64]bool{up.ID: false, down.ID: false})
}

// A viewer ID that names no Account is neither the Owner, a Delegate nor an
// Admin, so the Goal page's loader offers them neither Check in nor setting
// values, and none of the pending Draft Highlights.
func TestGoalPagePermitsNothingToAViewerWithNoAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	if _, err := h.Service.LogDraftHighlight(ctx, domain.LogDraftHighlightInput{
		GoalID: g.ID, AuthorID: sam.ID, Kind: domain.HighlightInsight, Note: "Search is slow on mobile.",
	}); err != nil {
		t.Fatalf("LogDraftHighlight: %v", err)
	}

	p, err := h.Service.GoalPage(ctx, sam.ID+1000, g.ID)
	if err != nil {
		t.Fatalf("GoalPage for a viewer with no Account: %v", err)
	}
	if p.CanCheckin {
		t.Errorf("CanCheckin for a viewer with no Account = true, want false")
	}
	if !reflect.DeepEqual(p.Permissions, domain.GoalPermissions{}) {
		t.Errorf("Permissions for a viewer with no Account = %+v, want none", p.Permissions)
	}
	if len(p.DraftHighlights) != 0 {
		t.Errorf("a viewer with no Account sees %d Draft Highlights, want none", len(p.DraftHighlights))
	}
}

// What the Goal page's loader works out for a Goal with a parent and a child,
// Check-ins, Milestones and a Metric — its Rolled-up Health, Run off Green,
// freshness, Health strip and signals — is what the separate Service methods
// give for the same Goal, and it judges a No change on it.
func TestGoalPageDerivesWhatTheServiceMethodsGive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Grow revenue", "The company needs it.")
	g := h.ActiveChildOf(sam, parent, "Ship search", "People can't find things.")
	child := h.ActiveChildOf(sam, g, "Index the catalogue", "Search needs it.")
	metric, err := h.Service.AddMetric(ctx, sam.ID, domain.AddMetricInput{
		GoalID: g.ID, Name: "Searches", Unit: "a day", Direction: domain.MetricUp,
		Baseline: 10, Target: 100, TargetDate: h.Clock.Now().AddDate(0, 2, 0),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	if _, err := h.Service.AddMilestone(ctx, sam.ID, domain.AddMilestoneInput{
		GoalID: g.ID, Name: "Beta", TargetDate: h.Clock.Now().AddDate(0, 0, 10),
	}); err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: g.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "On track.",
		Readings: []domain.MetricReadingInput{{MetricID: metric.ID, Value: 40}},
	}); err != nil {
		t.Fatalf("SubmitCheckin Green: %v", err)
	}
	h.Clock.Advance(8 * 24 * time.Hour)
	h.Checkin(sam, child.ID, domain.HealthYellow, "Slow going.", "Add an indexer.", h.Clock.Now().AddDate(0, 0, 7))
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: g.ID, AuthorID: sam.ID, Health: domain.HealthRed, Status: "Blocked on indexing.",
		PathToGreen: "Index the catalogue first.", PathTargetDate: h.Clock.Now().AddDate(0, 0, 3),
		Explanation: "The index is the blocker, not the child's pace.",
		Readings:    []domain.MetricReadingInput{{MetricID: metric.ID, Value: 45}},
	}); err != nil {
		t.Fatalf("SubmitCheckin Red: %v", err)
	}
	h.Clock.Advance(10 * 24 * time.Hour)

	p, err := h.Service.GoalPage(ctx, sam.ID, g.ID)
	if err != nil {
		t.Fatalf("GoalPage: %v", err)
	}
	if !p.RolledUp.Present || p.RolledUp.Health != domain.HealthYellow {
		t.Errorf("Rolled-up Health = %+v, want Yellow from the child", p.RolledUp)
	}
	if p.RunOffGreen.Weeks < 2 {
		t.Errorf("Run off Green = %+v, want a run of 2 weeks or more", p.RunOffGreen)
	}
	if !p.Freshness.CheckinOverdue || !p.Freshness.PathToGreenOverdue {
		t.Errorf("Freshness = %+v, want Check-in overdue with its Path to Green overdue", p.Freshness)
	}

	rollup, err := h.Service.RolledUpHealth(ctx, g.ID)
	if err != nil {
		t.Fatalf("RolledUpHealth: %v", err)
	}
	same(t, "Rolled-up Health", p.RolledUp, rollup)
	run, err := h.Service.RunOffGreen(ctx, g.ID)
	if err != nil {
		t.Fatalf("RunOffGreen: %v", err)
	}
	same(t, "Run off Green", p.RunOffGreen, run)
	fresh, err := h.Service.Freshness(ctx, g.ID)
	if err != nil {
		t.Fatalf("Freshness: %v", err)
	}
	same(t, "freshness", p.Freshness, fresh)
	strip, err := h.Service.HealthStrip(ctx, g.ID)
	if err != nil {
		t.Fatalf("HealthStrip: %v", err)
	}
	same(t, "Health strip", p.HealthStrip, strip)
	signals, err := h.Service.GoalSignals(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalSignals: %v", err)
	}
	same(t, "signals", p.Signals, signals)
	checkins, err := h.Service.ListCheckins(ctx, g.ID)
	if err != nil {
		t.Fatalf("ListCheckins: %v", err)
	}
	same(t, "Check-ins", p.Checkins, checkins)
	additions, err := h.Service.MilestoneChangesOutsideCheckins(ctx, g.ID)
	if err != nil {
		t.Fatalf("MilestoneChangesOutsideCheckins: %v", err)
	}
	same(t, "Milestone additions", p.MilestoneAdditions, additions)
	// Its No change is judged as NoChangeRefusal judges one: the Red repeats
	// its explanation of the Yellow roll-up, but the Beta due in ten days has
	// since gone overdue.
	if overdue := (*domain.MilestoneOverdueError)(nil); !errors.As(p.NoChangeRefusal, &overdue) || overdue.Milestone.Name != "Beta" {
		t.Errorf("No change refusal = %v, want Beta overdue", p.NoChangeRefusal)
	}
	if p.OverdueMilestone == nil || p.OverdueMilestone.Name != "Beta" {
		t.Errorf("OverdueMilestone = %+v, want Beta", p.OverdueMilestone)
	}
}

// same fails the test unless the loader's got is what the Service method of
// the same name gave, want.
func same[T any](t *testing.T, what string, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GoalPage's %s = %+v, want the Service method's %+v", what, got, want)
	}
}

// The Goal page carries the Goal's first overdue Planned Milestone, as of today
// in its timezone, with or without a Check-in and whatever its Health, so the
// Check-in form never starts from a Green the Milestone rules out (#566).
func TestGoalPageCarriesTheOverdueMilestone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	overdue := func() *domain.Milestone {
		t.Helper()
		p, err := h.Service.GoalPage(ctx, sam.ID, g.ID)
		if err != nil {
			t.Fatalf("GoalPage: %v", err)
		}
		return p.OverdueMilestone
	}

	if m := overdue(); m != nil {
		t.Errorf("before Beta's date, OverdueMilestone = %+v, want none", *m)
	}
	h.Clock.Advance(100 * 24 * time.Hour) // past Beta's date
	if m := overdue(); m == nil || m.Name != "Beta" {
		t.Errorf("with no Check-in, OverdueMilestone = %+v, want Beta", m)
	}
	h.Checkin(sam, g.ID, domain.HealthYellow, "Slipping.", "Add a reviewer.", h.Clock.Now().AddDate(0, 0, 7))
	if m := overdue(); m == nil || m.Name != "Beta" {
		t.Errorf("after a Yellow Check-in, OverdueMilestone = %+v, want Beta", m)
	}
}
