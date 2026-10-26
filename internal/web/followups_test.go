package web_test

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// followUpRowsIn returns every row of the Follow-ups page's table, in order.
func followUpRowsIn(root *html.Node) []*html.Node {
	return findAllByTestID(root, "follow-up-row")
}

// followUpRowIn returns the Follow-ups page's one row for g, failing the test
// unless the table holds exactly one row linking to it.
func followUpRowIn(t *testing.T, root *html.Node, g domain.Goal) *html.Node {
	t.Helper()
	var found []*html.Node
	for _, row := range followUpRowsIn(root) {
		if isFollowUpRowFor(row, g) {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the Follow-ups table has %d rows for %q, want 1:\n%s", len(found), g.Title, renderNode(root))
	}
	return found[0]
}

// hasFollowUpRowIn reports whether the Follow-ups page's table has a row for g.
func hasFollowUpRowIn(root *html.Node, g domain.Goal) bool {
	return slices.ContainsFunc(followUpRowsIn(root), func(row *html.Node) bool { return isFollowUpRowFor(row, g) })
}

// isFollowUpRowFor reports whether a Follow-ups table row is g's: its Goal
// cell opens with a link to g reading its title. A chip's link to a parent
// comes after it.
func isFollowUpRowFor(row *html.Node, g domain.Goal) bool {
	cells := findAllByTestID(row, "follow-up-goal")
	if len(cells) != 1 {
		return false
	}
	links := findAll(cells[0], func(n *html.Node) bool { return n.Data == "a" })
	return len(links) > 0 && attrOf(links[0], "href") == goalPageURL("", g) && textOf(links[0]) == g.Title
}

// followUpChipsIn returns each chip under root, a row or a group card, for
// the signal of kind.
func followUpChipsIn(root *html.Node, kind string) []*html.Node {
	return findAll(root, func(n *html.Node) bool {
		return attrOf(n, "data-testid") == "follow-up-signal" && attrOf(n, "data-kind") == kind
	})
}

// followUpChipIn returns the first chip under root, a row or a group card,
// for the signal of kind, failing the test unless there is one. A row under
// two halted parents has a chip for each.
func followUpChipIn(t *testing.T, root *html.Node, kind string) *html.Node {
	t.Helper()
	chips := followUpChipsIn(root, kind)
	if len(chips) == 0 {
		t.Fatalf("found no %s chip:\n%s", kind, renderNode(root))
	}
	return chips[0]
}

// followUpKindsIn lists the kinds of the signal chips under root, in order.
func followUpKindsIn(root *html.Node) []string {
	var kinds []string
	for _, chip := range findAllByTestID(root, "follow-up-signal") {
		kinds = append(kinds, attrOf(chip, "data-kind"))
	}
	return kinds
}

// followUpClassesOf is the classes n carries, in order.
func followUpClassesOf(n *html.Node) []string {
	return strings.Fields(attrOf(n, "class"))
}

// followUpTipIn returns a row chip's first button, the one naming its signal,
// failing the test unless the chip holds one.
func followUpTipIn(t *testing.T, chip *html.Node) *html.Node {
	t.Helper()
	buttons := findAll(chip, func(n *html.Node) bool { return n.Data == "button" })
	if len(buttons) == 0 {
		t.Fatalf("the %s chip has no button: %s", attrOf(chip, "data-kind"), renderNode(chip))
	}
	return buttons[0]
}

// assertFollowUpChip fails unless chip is drawn with classes, where classes
// isn't "", and reads says and nothing more, all of it on the button naming
// its signal.
func assertFollowUpChip(t *testing.T, chip *html.Node, classes, says string) {
	t.Helper()
	if classes != "" && !slices.Equal(followUpClassesOf(chip), strings.Fields(classes)) {
		t.Errorf("the %s chip is drawn %q, want %q: %s", attrOf(chip, "data-kind"), attrOf(chip, "class"), classes, renderNode(chip))
	}
	if read, tip := textOf(chip), textOf(followUpTipIn(t, chip)); read != says || tip != says {
		t.Errorf("the %s chip reads %q, its button %q, want both %q: %s", attrOf(chip, "data-kind"), read, tip, says, renderNode(chip))
	}
}

// followUpFixIn returns a row's one Fix: its label, its address (a form's
// action, for a Fix that posts, and "" for a disabled one), and whether it is
// the primary button, failing the test unless the fix cell holds exactly one
// link, one form or one button.
func followUpFixIn(t *testing.T, row *html.Node) (label, href string, primary bool) {
	t.Helper()
	cell := findByTestID(t, row, "follow-up-fix")
	tagged := func(tag string) []*html.Node { return findAll(cell, func(n *html.Node) bool { return n.Data == tag }) }
	links, forms, buttons := tagged("a"), tagged("form"), tagged("button")
	if len(links)+len(buttons) != 1 || len(forms) > len(buttons) {
		t.Fatalf("fix cell has %d links, %d forms and %d buttons, want one Fix: %s", len(links), len(forms), len(buttons), renderNode(cell))
	}
	if len(buttons) == 1 {
		if len(forms) == 1 {
			href = attrOf(forms[0], "action")
		}
		return textOf(buttons[0]), href, slices.Contains(followUpClassesOf(buttons[0]), "primary")
	}
	return textOf(links[0]), attrOf(links[0], "href"), slices.Contains(followUpClassesOf(links[0]), "primary")
}

// followUpsElementByID returns the one element under root with id, failing
// the test unless there is exactly one.
func followUpsElementByID(t *testing.T, root *html.Node, id string) *html.Node {
	t.Helper()
	found := findAll(root, func(n *html.Node) bool { return attrOf(n, "id") == id })
	if len(found) != 1 {
		t.Fatalf("page has %d elements with id %q, want 1", len(found), id)
	}
	return found[0]
}

// followUpPopoverIn returns the page's native popover with id, failing the
// test unless one is there.
func followUpPopoverIn(t *testing.T, root *html.Node, id string) *html.Node {
	t.Helper()
	pop := followUpsElementByID(t, root, id)
	if v := attrOf(pop, "popover"); !hasAttr(pop, "popover") || v != "" && v != "auto" {
		t.Fatalf("#%s is not a native popover: %s", id, renderNode(pop))
	}
	return pop
}

// followUpsWithTestIDPrefix returns every element under root whose test ID
// starts with prefix, in document order.
func followUpsWithTestIDPrefix(root *html.Node, prefix string) []*html.Node {
	return findAll(root, func(n *html.Node) bool { return strings.HasPrefix(attrOf(n, "data-testid"), prefix) })
}

// followUpsCardIn returns the page's group card for key.
func followUpsCardIn(t *testing.T, root *html.Node, key string) *html.Node {
	t.Helper()
	return findByTestID(t, root, "follow-ups-group-"+key)
}

// followUpsCardCount is how many Goals a group card holding some counts.
func followUpsCardCount(t *testing.T, card *html.Node) string {
	t.Helper()
	return textOf(findByTestID(t, card, "follow-up-group-count"))
}

// followUpsCardChips is what each of a group card's chips reads, in order.
func followUpsCardChips(card *html.Node) []string {
	return textsOf(findAllByTestID(card, "follow-up-signal"))
}

// followUpsAttention returns what the page's count line reads, and the count
// it shows in bold, failing the test unless it shows one.
func followUpsAttention(t *testing.T, root *html.Node) (line, count string) {
	t.Helper()
	head := findByTestID(t, root, "follow-ups-attention")
	bold := findAll(head, func(n *html.Node) bool { return n.Data == "strong" })
	if len(bold) != 1 {
		t.Fatalf("the count line shows %d counts in bold, want 1: %s", len(bold), renderNode(head))
	}
	return textOf(head), textOf(bold[0])
}

// followUpsNavCount is the count the sidebar's Follow-ups item shows, or ""
// when it shows none.
func followUpsNavCount(t *testing.T, root *html.Node) string {
	t.Helper()
	counts := findAll(findByTestID(t, root, "nav-follow-ups"), func(n *html.Node) bool { return flatFrameHasClass(n, "count") })
	if len(counts) == 0 {
		return ""
	}
	return textOf(counts[0])
}

// followUpsValueSelectIn returns the Follow-ups page's value select for the
// Dimension named dim, failing the test unless there is exactly one.
func followUpsValueSelectIn(t *testing.T, root *html.Node, dim string) *html.Node {
	t.Helper()
	var found []*html.Node
	for _, sel := range findAllByTestID(root, "follow-ups-value") {
		if attrOf(sel, "data-dimension") == dim {
			found = append(found, sel)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the Follow-ups page has %d value selects for %s, want 1", len(found), dim)
	}
	return found[0]
}

// followUpsOptionIn returns the option of sel submitting value, failing the
// test unless there is exactly one.
func followUpsOptionIn(t *testing.T, sel *html.Node, value string) *html.Node {
	t.Helper()
	options := findAll(sel, func(n *html.Node) bool { return n.Data == "option" && attrOf(n, "value") == value })
	if len(options) != 1 {
		t.Fatalf("the select has %d options submitting %q, want 1: %s", len(options), value, renderNode(sel))
	}
	return options[0]
}

// followUpsSelected is how many of sel's options are selected.
func followUpsSelected(sel *html.Node) int {
	return len(findAll(sel, func(n *html.Node) bool { return n.Data == "option" && hasAttr(n, "selected") }))
}

// The Follow-ups page is served at /follow-ups, headed and titled Follow-ups,
// with its nav item marked current. The page's old address, under its old
// name, Risks, is gone: no redirect, just Not found.
func TestFollowUpsPageIsAtFollowUps(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")
	client := signInClient(t, ts.URL, "sam@example.com")

	doc := getPage(t, client, ts.URL+"/follow-ups")
	titles := findAll(doc, func(n *html.Node) bool { return n.Data == "title" })
	if heading := headingNamed(doc, "Follow-ups"); len(titles) != 1 || textOf(titles[0]) != "Follow-ups · Goal Tracker" || heading == nil || heading.Data != "h1" {
		t.Errorf("/follow-ups isn't titled and headed Follow-ups:\n%s", renderNode(doc))
	}
	nav := findByTestID(t, doc, "nav-follow-ups")
	if nav.Data != "a" || attrOf(nav, "href") != "/follow-ups" || attrOf(nav, "aria-current") != "page" || textOf(nav) != "Follow-ups" {
		t.Errorf("the nav's Follow-ups item isn't a current link to /follow-ups: %s", renderNode(nav))
	}
	assertNotFoundPageFor(t, client, http.MethodGet, ts.URL+"/"+strings.ToLower("Risks"), "/home")
}

// The Follow-ups page lists each Check-in overdue Goal once, with its Owner, "You"
// for the viewer's own, and a Check-in overdue chip saying how long it has gone
// without a Check-in against its cadence. A fresh Goal isn't Check-in overdue.
func TestFollowUpsPageListsCheckinOverdueGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)
	fresh := h.ActiveGoal(sam, "Fresh work", "It matters.")
	h.Checkin(sam, fresh.ID, domain.HealthGreen, "On track.", "", time.Time{})

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	row := followUpRowIn(t, doc, silent)
	assertFollowUpChip(t, followUpChipIn(t, row, "checkin-overdue"), "badge st", "Check-in overdue · 10 days, checks in weekly")
	// Everything the Owner cell holds, hidden or not, reads You: it shows no
	// person, so no email either.
	if owner := findByTestID(t, row, "follow-up-owner"); textOf(owner) != "You" || strings.Join(nodeWords(owner, owner, nil), " ") != "You" {
		t.Errorf("the viewer's own Goal isn't Owned by You: %s", renderNode(owner))
	}
	if row := followUpRowIn(t, doc, fresh); len(followUpChipsIn(row, "checkin-overdue")) != 0 {
		t.Errorf("%q, which checked in today, is Check-in overdue:\n%s", fresh.Title, renderNode(row))
	}
}

// A Check-in overdue chip says in words how long its Goal has gone without a
// Check-in and how often it checks in, naming its cadence as the New goal
// form's chips do, and counting the days of any other.
func TestFollowUpsCheckinOverdueChipSaysItsCadenceInWords(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	want := map[int]string{
		7:  "Check-in overdue · 42 days, checks in weekly",
		14: "Check-in overdue · 42 days, checks in every 2 weeks",
		30: "Check-in overdue · 42 days, checks in monthly",
		3:  "Check-in overdue · 42 days, checks in every 3 days",
	}
	goals := map[int]domain.Goal{}
	for cadence := range want {
		g := h.ActiveGoal(sam, "Every "+strconv.Itoa(cadence)+" days", "It matters.")
		if _, err := h.Service.SetCadence(t.Context(), g.Owner.ID, g.ID, cadence); err != nil {
			t.Fatalf("SetCadence: %v", err)
		}
		goals[cadence] = g
	}
	h.Clock.Advance(42 * day)

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	for cadence, g := range goals {
		assertFollowUpChip(t, followUpChipIn(t, followUpRowIn(t, doc, g), "checkin-overdue"), "badge st", want[cadence])
	}
}

// The Follow-ups page lists each Goal still not Green past its Path to Green's
// target date with its Health and a chip saying how many days overdue it is.
// A Green Goal isn't overdue.
func TestFollowUpsPageListsOverduePathsToGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
	green := h.ActiveGoal(sam, "On track", "It matters.")
	h.Checkin(sam, green.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	h.Clock.Advance(5 * day)

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	row := followUpRowIn(t, doc, stalled)
	assertFollowUpChip(t, followUpChipIn(t, row, "path-overdue"), "badge st", "Path to Green 2 days overdue")
	assertHealthChip(t, findByTestID(t, row, "follow-up-health"), "Red")
	if row := followUpRowIn(t, doc, green); len(followUpChipsIn(row, "path-overdue")) != 0 {
		t.Errorf("Green %q is overdue:\n%s", green.Title, renderNode(row))
	}
}

// A Path to Green overdue chip counts its days in the Goal's Owner's Timezone,
// as the flag is judged (CONTEXT.md: Timezone): just past midnight in Tokyo it
// is a day further overdue than for an Owner with no Timezone, counted in the
// company's UTC.
func TestFollowUpsPathToGreenDaysAreCountedInTheOwnersTimezone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kenji := h.SignIn("kenji@example.com")
	h.SetTimezone(kenji, "Asia/Tokyo")
	target := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	tokyo := h.ActiveGoal(kenji, "Tokyo recovery", "It matters.")
	h.Checkin(kenji, tokyo.ID, domain.HealthRed, "Blocked.", "Escalate.", target)
	plain := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, plain.ID, domain.HealthRed, "Blocked.", "Escalate.", target)
	h.Clock.Set(time.Date(2026, 1, 7, 15, 4, 0, 0, time.UTC)) // Jan 8, 12:04am in Tokyo

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups?org=all")

	assertFollowUpChip(t, followUpChipIn(t, followUpRowIn(t, doc, tokyo), "path-overdue"), "", "Path to Green 3 days overdue")
	assertFollowUpChip(t, followUpChipIn(t, followUpRowIn(t, doc, plain), "path-overdue"), "", "Path to Green 2 days overdue")
}

// A Milestone overdue chip counts its days in the Goal's Owner's Timezone, as
// the flag is judged (CONTEXT.md: Timezone): at 1am in Tokyo it is a day
// further overdue than once the Owner has no Timezone, counted in the
// company's UTC.
func TestFollowUpsMilestoneOverdueDaysAreCountedInTheOwnersTimezone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")
	kenji := h.SignIn("kenji@example.com")
	h.SetTimezone(kenji, "Asia/Tokyo")
	g, beta := h.GreenGoalWithOverdueMilestone(kenji, "Ship search")
	h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1).Add(16 * time.Hour)) // Beta + 2 days, 1am in Tokyo
	client := signInClient(t, ts.URL, "sam@example.com")

	doc := getPage(t, client, ts.URL+"/follow-ups?org=all")
	assertFollowUpChip(t, followUpChipIn(t, followUpRowIn(t, doc, g), "milestone-overdue"), "", "Milestone 2 days overdue")

	h.SetTimezone(kenji, "")
	doc = getPage(t, client, ts.URL+"/follow-ups?org=all")
	assertFollowUpChip(t, followUpChipIn(t, followUpRowIn(t, doc, g), "milestone-overdue"), "", "Milestone 1 day overdue")
}

// A Path to Green overdue chip says in words how many days overdue it is.
func TestFollowUpsPathToGreenChipSaysItsDaysInWords(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	start := h.Clock.Now()
	want := map[int]string{12: "Path to Green 12 days overdue", 1: "Path to Green 1 day overdue"}
	goals := map[int]domain.Goal{}
	for overdue := range want {
		g := h.ActiveGoal(sam, strconv.Itoa(overdue)+" days overdue", "It matters.")
		h.Checkin(sam, g.ID, domain.HealthRed, "Blocked.", "Escalate.", start.AddDate(0, 0, 1-overdue))
		goals[overdue] = g
	}
	h.Clock.Advance(day)

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	for overdue, g := range goals {
		assertFollowUpChip(t, followUpChipIn(t, followUpRowIn(t, doc, g), "path-overdue"), "", want[overdue])
	}
}

// The Follow-ups page lists each Active Goal whose Owner has left the company with an
// Ownerless chip as prominent as Red, and the departed Owner, who isn't the
// viewer. A Proposed Goal of the same departed Owner isn't listed, nor is an
// Active Goal with a present Owner Ownerless.
func TestFollowUpsPageListsOwnerlessGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	orphaned := h.ActiveGoal(sam, "Migrate displays", "Displays fail often.")
	proposed := h.CreateGoal(sam, "Someday", "Maybe.")
	owned := h.ActiveGoal(kim, "Grow revenue", "It pays for everything.")
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	doc := getPage(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+"/follow-ups?org=all")

	row := followUpRowIn(t, doc, orphaned)
	assertFollowUpChip(t, followUpChipIn(t, row, "ownerless"), "badge ol", "Ownerless")
	owner := findByTestID(t, row, "follow-up-owner")
	if shown := findAll(owner, func(n *html.Node) bool { return attrOf(n, "title") == "sam@example.com" }); len(shown) != 1 || strings.Contains(textOf(owner), "You") {
		t.Errorf("row doesn't show the departed Owner: %s", renderNode(owner))
	}
	if hasFollowUpRowIn(doc, proposed) {
		t.Errorf("the Follow-ups table lists Proposed %q", proposed.Title)
	}
	if row := followUpRowIn(t, doc, owned); len(followUpChipsIn(row, "ownerless")) != 0 {
		t.Errorf("%q, whose Owner is present, is Ownerless:\n%s", owned.Title, renderNode(row))
	}
}

// The Follow-ups page chips each Unlinked Goal with an outlined chip saying why.
// A Top-level Goal and a Goal that contributes to it aren't Unlinked. How the
// outlined chip is drawn is TestFollowUpsOutlinedChipStyles's.
func TestFollowUpsPageListsUnlinkedGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	top := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Grow revenue", "It pays for everything."))
	aligned := h.ActiveChildOf(sam, top, "Ship search", "People can't find things.")
	loner := h.ActiveGoal(sam, "Side project", "Nobody asked.")

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	assertFollowUpChip(t, followUpChipIn(t, followUpRowIn(t, doc, loner), "unlinked"), "rk-tag", "Unlinked · no parent Goal, not Top-level")
	for _, g := range []domain.Goal{top, aligned} {
		if hasFollowUpRowIn(doc, g) {
			t.Errorf("the Follow-ups table lists %q", g.Title)
		}
	}
}

// assertFollowUpChipLinksParent fails unless chip is an outlined chip whose
// button reads tip, then reads says, ending with a link to parent reading its
// title.
func assertFollowUpChipLinksParent(t *testing.T, chip *html.Node, tip, says string, parent domain.Goal) {
	t.Helper()
	if !slices.Equal(followUpClassesOf(chip), []string{"rk-tag"}) || textOf(followUpTipIn(t, chip)) != tip || textOf(chip) != says+" "+parent.Title {
		t.Errorf("the %s chip is not an outlined chip saying %q: %s", attrOf(chip, "data-kind"), says, renderNode(chip))
	}
	last := chip.LastChild
	for last != nil && last.Type != html.ElementNode {
		last = last.PrevSibling
	}
	if last == nil || last.Data != "a" || attrOf(last, "href") != goalPageURL("", parent) || textOf(last) != parent.Title {
		t.Errorf("the %s chip doesn't end with a link to its parent %q: %s", attrOf(chip, "data-kind"), parent.Title, renderNode(chip))
	}
}

// The Follow-ups page chips each Goal due later than a Goal it contributes to with
// its delivery date, how many days after its parent's that is, and a link to
// the parent. A date in another year carries its year.
func TestFollowUpsPageListsScheduleConflicts(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 1, 0))
	h.RequestLink(sam, late, launch, "")
	early := h.ActiveGoalDue(sam, "Early piece", june.AddDate(0, -1, 0))
	h.RequestLink(sam, early, launch, "")
	yearEnd := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Year-end close", time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC)))
	slipped := h.ActiveGoalDue(sam, "Slipped piece", time.Date(2027, 1, 10, 0, 0, 0, 0, time.UTC))
	h.RequestLink(sam, slipped, yearEnd, "")

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	for _, c := range []struct {
		child, parent domain.Goal
		says          string
	}{
		{late, launch, "Schedule conflict · due Jul 1, 30 days after its parent"},
		{slipped, yearEnd, "Schedule conflict · due Jan 10, 2027, 21 days after its parent"},
	} {
		chip := followUpChipIn(t, followUpRowIn(t, doc, c.child), "schedule-conflicts")
		assertFollowUpChipLinksParent(t, chip, "Schedule conflict", c.says, c.parent)
	}
	if hasFollowUpRowIn(doc, early) && len(followUpChipsIn(followUpRowIn(t, doc, early), "schedule-conflicts")) != 0 {
		t.Errorf("%q, due before its parent, is a schedule conflict", early.Title)
	}
}

// The Follow-ups page chips each Goal contributing to a Goal that is On Hold or
// Cancelled with its parent's Lifecycle and a link to the parent. A Goal under
// an Active parent isn't chipped.
func TestFollowUpsPageListsGoalsUnderHaltedParents(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	live := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Live outcome", "It matters."))
	underLive := h.ActiveChildOf(sam, live, "Under live", "Feeds live.")
	paused := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Paused outcome", "It mattered."))
	underPaused := h.ActiveChildOf(sam, paused, "Under paused", "Feeds paused.")
	dropped := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Dropped outcome", "It mattered."))
	underDropped := h.ActiveChildOf(sam, dropped, "Under dropped", "Feeds dropped.")
	for parent, lifecycle := range map[int64]string{paused.ID: domain.LifecycleOnHold, dropped.ID: domain.LifecycleCancelled} {
		if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
			GoalID:          parent,
			AuthorID:        sam.ID,
			Status:          "Stopping.",
			Lifecycle:       lifecycle,
			LifecycleReason: "Budget freeze.",
		}); err != nil {
			t.Fatalf("SubmitCheckin %s: %v", lifecycle, err)
		}
	}

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	for _, c := range []struct {
		child, parent domain.Goal
		tip           string
	}{
		{underPaused, paused, "Parent On Hold"},
		{underDropped, dropped, "Parent Cancelled"},
	} {
		chip := followUpChipIn(t, followUpRowIn(t, doc, c.child), "halted-parents")
		assertFollowUpChipLinksParent(t, chip, c.tip, c.tip+" ·", c.parent)
	}
	if hasFollowUpRowIn(doc, underLive) {
		t.Errorf("the Follow-ups table lists %q, whose parent is Active", underLive.Title)
	}
}

// The Follow-ups page lists a Goal flagged by two signals once, in one
// table: one row with both chips, a neutral dash for no Health, and one Fix:
// Check in for its Owner, though it is Unlinked too. That the table is
// full-bleed and scrolls sideways inside itself is
// TestFollowUpsListScrollsSidewaysStyles's.
func TestFollowUpsPageListsEachFlaggedGoalOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	tables := findAll(doc, func(n *html.Node) bool { return n.Data == "table" })
	if len(tables) != 1 || attrOf(tables[0], "id") != "follow-ups-table" {
		t.Fatalf("page has %d tables, want the one Follow-ups table", len(tables))
	}
	heads := findAll(tables[0], func(n *html.Node) bool { return n.Data == "th" })
	if got := textsOf(heads); !slices.Equal(got, []string{"Health", "Goal", "Owner", ""}) || attrOf(heads[3], "aria-label") != "Next step" {
		t.Errorf("the Follow-ups table's head reads %q, want Health, Goal, Owner and a Next step column: %s", got, renderNode(tables[0]))
	}
	if rows := followUpRowsIn(doc); len(rows) != 1 {
		t.Fatalf("the Follow-ups table has %d rows, want 1", len(rows))
	}
	row := followUpRowIn(t, doc, silent)
	if got := followUpKindsIn(row); !slices.Equal(got, []string{"checkin-overdue", "unlinked"}) {
		t.Errorf("row has chips %q, want Check-in overdue and Unlinked:\n%s", got, renderNode(row))
	}
	if health := findByTestID(t, row, "follow-up-health"); !slices.Equal(followUpClassesOf(health), []string{"badge", "lc"}) || textOf(health) != "—" {
		t.Errorf("row doesn't show a neutral dash for no Health: %s", renderNode(health))
	}
	if label, href, _ := followUpFixIn(t, row); label != "Check in" || href != "/goals/"+strconv.FormatInt(silent.ID, 10)+"/checkin" {
		t.Errorf("the Owner's Check-in overdue and Unlinked Goal's Fix is %q → %s, want Check in", label, href)
	}
	for _, gone := range []string{"follow-ups-nothing-in", "follow-ups-checkin-overdue"} {
		if found := followUpsWithTestIDPrefix(doc, gone); len(found) > 0 {
			t.Errorf("page still has %s: %s", gone, renderNode(found[0]))
		}
	}
	if empty := findAll(doc, func(n *html.Node) bool { return flatFrameHasClass(n, "rk-empty") }); len(empty) > 0 {
		t.Errorf("page still has an empty line: %s", renderNode(empty[0]))
	}
	if strings.Contains(textOf(doc), "Why it's flagged") {
		t.Errorf("page still says Why it's flagged")
	}
}

// With nothing flagged, the Follow-ups page heads with no count and says so in one
// line, with no table, no group cards and nothing to clear.
func TestFollowUpsPageWithNothingFlaggedSaysSo(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups?org=all")

	if empty := findByTestID(t, doc, "follow-ups-empty"); empty.Data != "p" || textOf(empty) != "No Goals need attention." {
		t.Errorf("the empty line doesn't say no Goals need attention: %s", renderNode(empty))
	}
	if tables := findAll(doc, func(n *html.Node) bool { return n.Data == "table" }); len(tables) > 0 {
		t.Errorf("with nothing flagged, the page has a table")
	}
	for _, gone := range []string{"follow-ups-attention", "follow-ups-groups", "follow-ups-group-", "follow-ups-all-clear", "follow-ups-tile-"} {
		if found := followUpsWithTestIDPrefix(doc, gone); len(found) > 0 {
			t.Errorf("with nothing flagged, the page has %s: %s", gone, renderNode(found[0]))
		}
	}
	if strings.Contains(textOf(doc), "Show everything") {
		t.Errorf("with nothing flagged, the page offers Show everything")
	}
	if count := followUpsNavCount(t, doc); count != "" {
		t.Errorf("Follow-ups shows a count of %s with nothing flagged", count)
	}
}

// A group card holding no Goals while another holds some links nowhere and
// says why it is empty. That it is muted with no dashed edge, the groups
// being plain blocks, not cards, is TestFollowUpsEmptyGroupIsMutedStyles's.
func TestFollowUpsPageMutesAGroupWithNoGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	card := followUpsCardIn(t, doc, "admin")
	if card.Data != "div" {
		t.Errorf("the empty admin card is a %s, not a plain block", card.Data)
	}
	linking := findAll(card, func(n *html.Node) bool { return n.Data == "a" || hasAttr(n, "href") })
	if !strings.Contains(textOf(card), "Nothing needs an Admin.") || len(linking) > 0 {
		t.Errorf("the empty admin card doesn't say nothing needs an Admin, linking nowhere: %s", renderNode(card))
	}
}

// The Follow-ups page heads with how many Goals there are to review, most urgent
// first, in the singular for one Goal, and no longer says what's going wrong.
func TestFollowUpsPageHeadsWithHowManyGoalsToReview(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")

	doc := getPage(t, client, ts.URL+"/follow-ups?org=all")

	if line, count := followUpsAttention(t, doc); line != "1 Goal to review, most urgent first." || count != "1" {
		t.Errorf("header says %q, counting %q, want 1 Goal to review, most urgent first", line, count)
	}
	if strings.Contains(textOf(doc), "What's going wrong") {
		t.Errorf("page still says what's going wrong")
	}

	h.ActiveGoal(sam, "More silent work", "It matters.")
	h.Clock.Advance(10 * day)
	// The session from before the clock moved has ended; sign in again.
	client = signInClient(t, ts.URL, "sam@example.com")
	doc = getPage(t, client, ts.URL+"/follow-ups?org=all")

	if line, count := followUpsAttention(t, doc); line != "2 Goals to review, most urgent first." || count != "2" {
		t.Errorf("header says %q, counting %q, want 2 Goals to review, most urgent first", line, count)
	}
}

// Beside its count line, the Follow-ups page has a "What each signal means"
// button opening, through a native popover and no script, every signal's
// definition, once each, in the page's order. No list of them waits at the
// page's foot.
func TestFollowUpsPageDefinesEachSignalBesideItsCount(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	head := followUpsElementByID(t, doc, "follow-ups-head")
	if !hasTestID(head, "follow-ups-attention") {
		t.Fatalf("the page's head has no count line:\n%s", renderNode(head))
	}
	button := buttonNamed(head, "What each signal means")
	if button == nil {
		t.Fatalf("no What each signal means button beside the count line:\n%s", renderNode(head))
	}
	if attrOf(button, "type") != "button" || attrOf(button, "popovertarget") == "" {
		t.Fatalf("What each signal means isn't a button opening a popover: %s", renderNode(button))
	}
	pop := followUpPopoverIn(t, doc, attrOf(button, "popovertarget"))
	var got, want []string
	for c := pop.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == html.ElementNode && c.Data == "dl":
			for d := c.FirstChild; d != nil; d = d.NextSibling {
				if d.Type == html.ElementNode {
					got = append(got, d.Data+": "+textOf(d))
				}
			}
		case c.Type == html.ElementNode, c.Type == html.TextNode && strings.TrimSpace(c.Data) != "":
			got = append(got, "outside the list: "+renderNode(c))
		}
	}
	for _, d := range web.FollowUpSignalDefinitions {
		want = append(want, "dt: "+d.Name, "dd: "+d.Means)
	}
	if !slices.Equal(got, want) {
		t.Errorf("the list doesn't define each signal once, in the page's order:\n%q\nwant\n%q", got, want)
	}
	if hasTestID(doc, "follow-ups-definitions") || strings.Contains(textOf(doc), "What each risk means") {
		t.Errorf("the page still has its old list of definitions")
	}
}

// The Follow-ups page heads with how many Goals need attention, each once, then a
// card per group of signals, Not Green first, then by who acts: each counts the Goals it holds once,
// chips each of its signals with how many Goals it flags, and links to the
// page filtered to the group.
func TestFollowUpsPageSummarizesGroupsByWhoActs(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	silent := h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue, Unlinked and an overdue Action Item
	h.Clock.Advance(10 * day)
	h.Checkin(sam, launch.ID, domain.HealthGreen, "On track.", "", time.Time{})
	raiseFollowUpActionItem(t, h, ada, silent, sam, "Break the silence.", -1)
	h.ActiveGoal(kim, "Orphaned work", "It matters.") // Ownerless and Unlinked
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 1, 0)) // Schedule conflict
	h.RequestLink(sam, late, launch, "")
	stalled := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Stalled recovery", "It matters.")) // Red
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", june)

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups?org=all")

	if line, count := followUpsAttention(t, doc); count != "4" || !strings.HasPrefix(line, "4 Goals to review,") {
		t.Errorf("header does not count the 4 flagged Goals: %q", line)
	}
	var previous *html.Node
	for _, g := range []struct {
		key, name, href string
		count           int
		chips           []string
	}{
		{"health", "Not Green", "/follow-ups?group=health&org=all", 1, []string{"Red · 1"}},
		{"owner", "Waiting on a Check-in", "/follow-ups?group=owner&org=all", 1, []string{"Check-in overdue · 1"}},
		{"followup", "Action Items overdue", "/follow-ups?group=followup&org=all", 1, []string{"Action Item overdue · 1"}},
		{"plan", "Placement and dates", "/follow-ups?group=plan&org=all", 3, []string{"Unlinked · 2", "Schedule conflicts · 1"}},
		{"admin", "Needs an Admin", "/follow-ups?group=admin&org=all", 1, []string{"Ownerless · 1"}},
	} {
		card := followUpsCardIn(t, doc, g.key)
		if card.Data != "a" || attrOf(card, "href") != g.href {
			t.Errorf("%s card doesn't link to %s: %s", g.key, g.href, renderNode(card))
		}
		if !strings.HasPrefix(textOf(card), g.name+" ") || followUpsCardCount(t, card) != strconv.Itoa(g.count) {
			t.Errorf("%s card doesn't name %s counting %d: %s", g.key, g.name, g.count, renderNode(card))
		}
		if got := followUpsCardChips(card); !slices.Equal(got, g.chips) {
			t.Errorf("%s card chips %q, want %q", g.key, got, g.chips)
		}
		if hasAttr(card, "aria-current") {
			t.Errorf("%s card is current with no filter: %s", g.key, renderNode(card))
		}
		if previous != nil && !precedes(previous, card) {
			t.Errorf("%s card is out of order", g.key)
		}
		previous = card
	}
	for _, c := range []struct{ group, kind, class string }{
		{"health", "red", "badge r"},
		{"owner", "checkin-overdue", "badge st"},
		{"followup", "action-item-overdue", "badge st"},
		{"plan", "unlinked", "rk-tag"},
		{"plan", "schedule-conflicts", "rk-tag"},
		{"admin", "ownerless", "badge ol"},
	} {
		if chip := followUpChipIn(t, followUpsCardIn(t, doc, c.group), c.kind); !slices.Equal(followUpClassesOf(chip), strings.Fields(c.class)) {
			t.Errorf("the %s card's %s chip isn't drawn like the table's %s: %s", c.group, c.kind, c.class, renderNode(chip))
		}
	}
	if strings.Contains(textOf(doc), "Show all") {
		t.Errorf("page offers Show all with no filter")
	}
}

// A group card's address filters the page to the signals in that group, with
// no script: the table lists only the Goals they flag, chipped with them
// alone, the card is current under its name, the cards still count every group, and Show all
// clears the filter. An unknown group shows everything.
func TestFollowUpsPageFiltersToAGroup(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)
	orphaned := h.ActiveGoal(kim, "Orphaned work", "It matters.") // Ownerless and Unlinked
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	client := signInClient(t, ts.URL, "sam@example.com")

	doc := getPage(t, client, ts.URL+"/follow-ups?group=owner&org=all")

	row := followUpRowIn(t, doc, silent)
	followUpChipIn(t, row, "checkin-overdue")
	if len(followUpChipsIn(row, "unlinked")) != 0 {
		t.Errorf("filtered to owner, %q still has its Unlinked chip:\n%s", silent.Title, renderNode(row))
	}
	if hasFollowUpRowIn(doc, orphaned) {
		t.Errorf("filtered to owner, the page lists %q, flagged only outside it", orphaned.Title)
	}
	if line, count := followUpsAttention(t, doc); count != "2" {
		t.Errorf("filtered to owner, the header doesn't still count both flagged Goals: %q", line)
	}
	for key, want := range map[string]string{"owner": "1", "plan": "2", "admin": "1"} {
		card := followUpsCardIn(t, doc, key)
		if got := followUpsCardCount(t, card); got != want {
			t.Errorf("filtered to owner, the %s card counts %s, want %s", key, got, want)
		}
		if current := attrOf(card, "aria-current") == "page"; current != (key == "owner") {
			t.Errorf("filtered to owner, the %s card's aria-current is %v: %s", key, current, renderNode(card))
		}
	}
	if all := findByTestID(t, doc, "follow-ups-show-all"); all.Data != "a" || attrOf(all, "href") != "/follow-ups?org=all" || textOf(all) != "Show all" {
		t.Errorf("Show all doesn't clear the filter: %s", renderNode(all))
	}
	if card := followUpsCardIn(t, doc, "owner"); !strings.Contains(textOf(card), "Waiting on a Check-in") {
		t.Errorf("filtered to owner, the current card isn't Waiting on a Check-in: %s", renderNode(card))
	}

	doc = getPage(t, client, ts.URL+"/follow-ups?group=plan&org=all")

	for _, g := range []domain.Goal{silent, orphaned} {
		row := followUpRowIn(t, doc, g)
		followUpChipIn(t, row, "unlinked")
		for _, other := range []string{"checkin-overdue", "ownerless"} {
			if len(followUpChipsIn(row, other)) != 0 {
				t.Errorf("filtered to plan, %q still has its %s chip:\n%s", g.Title, other, renderNode(row))
			}
		}
	}
	for _, key := range []string{"owner", "plan", "admin"} {
		card := followUpsCardIn(t, doc, key)
		if current := attrOf(card, "aria-current") == "page"; current != (key == "plan") {
			t.Errorf("filtered to plan, the %s card's aria-current is %v: %s", key, current, renderNode(card))
		}
	}
	if card := followUpsCardIn(t, doc, "plan"); !strings.Contains(textOf(card), "Placement and dates") {
		t.Errorf("filtered to plan, the current card isn't Placement and dates: %s", renderNode(card))
	}

	doc = getPage(t, client, ts.URL+"/follow-ups?group=bogus&org=all")

	followUpChipIn(t, followUpRowIn(t, doc, silent), "unlinked")
	followUpChipIn(t, followUpRowIn(t, doc, orphaned), "ownerless")
	for _, key := range []string{"owner", "plan", "admin"} {
		if card := followUpsCardIn(t, doc, key); hasAttr(card, "aria-current") {
			t.Errorf("an unknown group marks the %s card current: %s", key, renderNode(card))
		}
	}
	if hasTestID(doc, "follow-ups-show-all") {
		t.Errorf("an unknown group offers Show all")
	}
}

// Scoped and filtered to nothing while other Goals are flagged, the Follow-ups page
// says where nothing needs attention, Mine then the value then the group, and
// links to everything, Everyone's, with no count, table or cards. Needs an
// Admin, company-wide, says nothing needs an Admin in the value alone, and
// has nowhere wider to link.
func TestFollowUpsPageFilteredToNothingSaysWhere(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	platform := h.CreateDimension(boss, "Team", "Platform").Values[0]
	own := h.ActiveGoal(sam, "Own work", "It matters.") // Unlinked only
	h.ActiveGoal(kim, "Kim's work", "It matters.")      // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)
	h.Checkin(sam, own.ID, domain.HealthGreen, "On track.", "", time.Time{})
	client := signInClient(t, ts.URL, "sam@example.com")

	for address, want := range map[string]string{
		"/follow-ups?org=mine&group=owner":                                                  "No Goals need attention in Mine, Waiting on a Check-in.",
		"/follow-ups?group=admin&value=" + strconv.FormatInt(platform.ID, 10) + "&org=mine": "Nothing needs an Admin in Team Platform.",
		"/follow-ups?org=all&value=" + strconv.FormatInt(platform.ID, 10):                   "No Goals need attention in Team Platform.",
	} {
		doc := getPage(t, client, ts.URL+address)

		empty := findByTestID(t, doc, "follow-ups-empty")
		links := findAll(empty, func(n *html.Node) bool { return n.Data == "a" })
		switch {
		case strings.HasPrefix(address, "/follow-ups?group=admin"):
			if textOf(empty) != want || len(links) != 0 {
				t.Errorf("at %s, the empty line doesn't say only %q: %s", address, want, renderNode(empty))
			}
		case textOf(empty) != want+" Show everything":
			t.Errorf("at %s, the empty line doesn't say %q: %s", address, want, renderNode(empty))
		case len(links) != 1 || attrOf(links[0], "href") != "/follow-ups?org=all" || textOf(links[0]) != "Show everything":
			t.Errorf("at %s, the empty line doesn't end linking to everything: %s", address, renderNode(empty))
		}
		if tables := findAll(doc, func(n *html.Node) bool { return n.Data == "table" }); len(tables) > 0 {
			t.Errorf("at %s, the page has a table", address)
		}
		for _, gone := range []string{"follow-ups-attention", "follow-ups-groups", "follow-ups-group-", "follow-ups-show-all"} {
			if found := followUpsWithTestIDPrefix(doc, gone); len(found) > 0 {
				t.Errorf("at %s, the page has %s: %s", address, gone, renderNode(found[0]))
			}
		}
	}
}

// Every page's sidebar has Follow-ups after Goals, counting the Goals the Follow-ups page
// flags in the viewer's default scope, each once however many sections list
// it: Ada's org's 3 for Ada, who manages Sam and Kim, Sam's own 2 for Sam. The
// nav doesn't link to the Graph or Freshness signals pages it replaces.
func TestNavCountsFlaggedGoalsOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	h.SetManager(sam, ada)
	h.SetManager(kim, ada)
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)
	h.Checkin(sam, launch.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.ActiveGoal(kim, "Orphaned work", "It matters.") // Ownerless and Unlinked
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 1, 0)) // Schedule conflict
	h.RequestLink(sam, late, launch, "")

	for who, want := range map[string]string{"sam@example.com": "2", "ada@example.com": "3"} {
		client := signInClient(t, ts.URL, who)
		for _, path := range []string{"/home", "/goals", "/follow-ups"} {
			doc := getPage(t, client, ts.URL+path)
			followUps := findByTestID(t, doc, "nav-follow-ups")
			if attrOf(followUps, "href") != "/follow-ups" || followUpsNavCount(t, doc) != want {
				t.Errorf("%s on %s: Follow-ups does not count the %s flagged Goals: %s", who, path, want, renderNode(followUps))
			}
			nav := followUps.Parent
			for nav != nil && nav.Data != "nav" {
				nav = nav.Parent
			}
			if nav == nil {
				t.Fatalf("%s on %s: Follow-ups sits in no nav", who, path)
			}
			for _, before := range []string{"nav-home", "nav-goals"} {
				if !precedes(findByTestID(t, nav, before), followUps) {
					t.Errorf("%s on %s: Follow-ups comes before %s:\n%s", who, path, before, renderNode(nav))
				}
			}
			for _, gone := range []string{"/signals", "/freshness"} {
				if linkTo(nav, gone) != nil {
					t.Errorf("%s on %s: the sidebar still links %s", who, path, gone)
				}
			}
		}
	}
}

// A Goal's page shows its freshness and graph signals as alert banners,
// Check-in overdue in its own color, and the Goal list marks a Check-in overdue
// or overdue row with a chip in the same color saying how long. The
// data-testids other tests find them by don't change.
func TestFollowUpFlagsUseAlertBannersAndChips(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	silent := h.ActiveGoalDue(sam, "Silent work", june.AddDate(0, 1, 0))
	h.Clock.Advance(16 * day)
	parent := h.ActiveGoalDue(sam, "Launch", june)
	h.RequestLink(sam, silent, parent, "")
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", testsupport.Epoch)

	client := signInClient(t, ts.URL, "sam@example.com")
	// banner is the page's one alert banner with testID: a paragraph drawn
	// in classes.
	banner := func(doc *html.Node, testID, classes string) {
		t.Helper()
		if el := findByTestID(t, doc, testID); el.Data != "p" || !slices.Equal(followUpClassesOf(el), strings.Fields(classes)) {
			t.Errorf("%s banner is not %s: %s", testID, classes, renderNode(el))
		}
	}
	silentPage := getPage(t, client, goalPageURL(ts.URL, silent))
	banner(silentPage, "goal-checkin-overdue", "alert st")
	banner(silentPage, "goal-schedule-conflict", "alert lc")
	if !hasTestID(silentPage, "goal-signals") {
		t.Errorf("Goal page lost its goal-signals group")
	}
	banner(getPage(t, client, goalPageURL(ts.URL, parent)), "goal-unlinked", "alert lc")
	banner(getPage(t, client, goalPageURL(ts.URL, stalled)), "goal-path-overdue", "alert st")

	list := getPage(t, client, ts.URL+"/goals")
	// chip is the Goal list's first chip with testID.
	chip := func(testID string) *html.Node {
		t.Helper()
		chips := findAll(list, func(n *html.Node) bool { return n.Data == "span" && attrOf(n, "data-testid") == testID })
		if len(chips) == 0 {
			t.Fatalf("the Goal list has no %s chip", testID)
		}
		return chips[0]
	}
	if c := chip("checkin-overdue"); !slices.Equal(followUpClassesOf(c), []string{"badge", "st"}) || !strings.Contains(textOf(c), "Check-in overdue · 16 days") {
		t.Errorf("Check-in overdue row chip is not a Check-in overdue badge saying how long: %s", renderNode(c))
	}
	if c := chip("path-overdue"); !slices.Equal(followUpClassesOf(c), []string{"badge", "st"}) || !strings.Contains(textOf(c), "Path to Green overdue") {
		t.Errorf("overdue row chip is not a Check-in overdue-colored badge: %s", renderNode(c))
	}

	adaPage := getPage(t, signInClient(t, ts.URL, "ada@example.com"), goalPageURL(ts.URL, silent)+"?open=top-level")
	control := findByTestID(t, adaPage, "mark-top-level")
	if plain := findAll(control, func(n *html.Node) bool {
		return n.Data == "button" && attrOf(n, "type") == "submit" && attrOf(n, "class") == "btn"
	}); control.Data != "section" || len(plain) == 0 {
		t.Errorf("Top-level control is not a plain button: %s", renderNode(control))
	}
}

// A Check-in overdue Goal's Fix is Check in, the primary button, for its Owner
// and its Delegates, who can check in; anyone else, an Admin included, gets
// Nudge.
func TestFollowUpFixOnACheckinOverdueGoalIsCheckInForWhoCanCheckIn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	h.SignIn("kim@example.com")
	top := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Grow revenue", "It pays for everything."))
	silent := h.ActiveChildOf(sam, top, "Silent work", "It matters.")
	h.AddDelegate(sam, dee, silent.ID)
	h.Clock.Advance(10 * day)
	h.Checkin(sam, top.ID, domain.HealthGreen, "On track.", "", time.Time{})

	for _, c := range []struct {
		viewer, label, href string
		primary             bool
	}{
		{"sam@example.com", "Check in", "/goals/" + strconv.FormatInt(silent.ID, 10) + "/checkin", true},
		{"dee@example.com", "Check in", "/goals/" + strconv.FormatInt(silent.ID, 10) + "/checkin", true},
		{"kim@example.com", "Nudge", "/goals/" + strconv.FormatInt(silent.ID, 10) + "/nudge", false},
		{"ada@example.com", "Nudge", "/goals/" + strconv.FormatInt(silent.ID, 10) + "/nudge", false},
	} {
		doc := getPage(t, signInClient(t, ts.URL, c.viewer), ts.URL+"/follow-ups?org=all")
		label, href, primary := followUpFixIn(t, followUpRowIn(t, doc, silent))
		if label != c.label || href != c.href || primary != c.primary {
			t.Errorf("%s sees Fix %q → %s (primary %v), want %q → %s (primary %v)", c.viewer, label, href, primary, c.label, c.href, c.primary)
		}
	}
}

// An Ownerless Goal's Fix for an Admin is Reassign, opening the Goal page's
// reassign form; a Delegate on it, who can still check in, gets Check in while
// it is Check-in overdue.
func TestFollowUpFixOnAnOwnerlessGoalIsReassignForAnAdmin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	top := h.MarkTopLevel(ada, h.ActiveGoal(ada, "Grow revenue", "It pays for everything."))
	orphaned := h.ActiveChildOf(sam, top, "Migrate displays", "Displays fail often.")
	h.AddDelegate(sam, dee, orphaned.ID)
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	h.Clock.Advance(10 * day)

	goal := "/goals/" + strconv.FormatInt(orphaned.ID, 10)
	for _, c := range []struct {
		viewer, label, href string
	}{
		{"ada@example.com", "Reassign", goal + "?open=reassign"},
		{"dee@example.com", "Check in", goal + "/checkin"},
	} {
		client := signInClient(t, ts.URL, c.viewer)
		row := followUpRowIn(t, getPage(t, client, ts.URL+"/follow-ups?org=all"), orphaned)
		followUpChipIn(t, row, "ownerless")
		followUpChipIn(t, row, "checkin-overdue")
		label, href, _ := followUpFixIn(t, row)
		if label != c.label || href != c.href {
			t.Fatalf("%s sees Fix %q → %s, want %q → %s", c.viewer, label, href, c.label, c.href)
		}
		if label == "Reassign" && !hasTestID(getPage(t, client, ts.URL+href), "reassign-goal") {
			t.Errorf("Reassign doesn't open the reassign form")
		}
	}
}

// An Unlinked Goal's Fix for its Owner is Link to a parent, opening the Goal
// page's parent-link form; anyone else, a Delegate included, who can't link
// it, gets Suggest a parent, opening the Suggest a parent form.
func TestFollowUpFixOnAnUnlinkedGoalIsLinkToAParentForItsOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	h.SignIn("kim@example.com")
	loner := h.ActiveGoal(sam, "Side project", "Nobody asked.")
	h.AddDelegate(sam, dee, loner.ID)

	goal := "/goals/" + strconv.FormatInt(loner.ID, 10)
	for _, c := range []struct {
		viewer, label, href string
	}{
		{"sam@example.com", "Link to a parent", goal + "?open=parent-link"},
		{"dee@example.com", "Suggest a parent", goal + "/suggest-parent"},
		{"kim@example.com", "Suggest a parent", goal + "/suggest-parent"},
	} {
		client := signInClient(t, ts.URL, c.viewer)
		label, href, _ := followUpFixIn(t, followUpRowIn(t, getPage(t, client, ts.URL+"/follow-ups?org=all"), loner))
		if label != c.label || href != c.href {
			t.Fatalf("%s sees Fix %q → %s, want %q → %s", c.viewer, label, href, c.label, c.href)
		}
		if label == "Link to a parent" && !hasTestID(getPage(t, client, ts.URL+href), "request-link") {
			t.Errorf("Link to a parent doesn't open the parent-link form")
		}
		if label == "Suggest a parent" && !hasTestID(getPage(t, client, ts.URL+href), "suggest-parent") {
			t.Errorf("Suggest a parent doesn't open the Suggest a parent form")
		}
	}
}

// A Goal due after its parent gets Check in, its dates open, for its Owner,
// even when that parent is On Hold too; a Goal under halted parents only gets
// Open parent, the halted parent first by title. Every row has exactly one Fix.
func TestFollowUpFixOnAStructuralSignal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 1, 0))
	h.RequestLink(sam, late, launch, "")
	zeta := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Zeta outcome", "It mattered."))
	alpha := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Alpha outcome", "It mattered."))
	under := h.ActiveChildOf(sam, zeta, "Under both", "Feeds both.")
	h.RequestLink(sam, under, alpha, "")
	for _, parent := range []domain.Goal{launch, zeta, alpha} {
		if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
			GoalID:          parent.ID,
			AuthorID:        sam.ID,
			Status:          "Stopping.",
			Lifecycle:       domain.LifecycleOnHold,
			LifecycleReason: "Budget freeze.",
		}); err != nil {
			t.Fatalf("SubmitCheckin %q On Hold: %v", parent.Title, err)
		}
	}

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	for _, c := range []struct {
		goal        domain.Goal
		kinds       []string
		label, href string
	}{
		{late, []string{"schedule-conflicts", "halted-parents"}, "Check in", "/goals/" + strconv.FormatInt(late.ID, 10) + "/checkin?open=dates"},
		{under, []string{"halted-parents"}, "Open parent", "/goals/" + strconv.FormatInt(alpha.ID, 10)},
	} {
		row := followUpRowIn(t, doc, c.goal)
		for _, kind := range c.kinds {
			followUpChipIn(t, row, kind)
		}
		if label, href, _ := followUpFixIn(t, row); label != c.label || href != c.href {
			t.Errorf("%q's Fix is %q → %s, want %q → %s", c.goal.Title, label, href, c.label, c.href)
		}
	}
	for _, row := range followUpRowsIn(doc) {
		followUpFixIn(t, row) // every row has exactly one Fix
	}
}

// A Schedule conflict's Fix is Check in, its dates open, for the Owner or a
// Delegate of either Goal, on their own Goal, the child first; anyone else
// gets Nudge on the child, the row's Goal, and once it's nudged, a disabled
// "Nudged today by" whoever did (CONTEXT.md: Schedule conflict, Nudge).
func TestFollowUpFixOnAScheduleConflict(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	dee := h.SignIn("dee@example.com")
	lee := h.SignIn("lee@example.com")
	h.SignInNamed("pat@example.com", "Pat Okafor")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	late := h.ActiveGoalDue(kim, "Late piece", june.AddDate(0, 1, 0))
	link := h.RequestLink(kim, late, launch, "")
	if _, err := h.Service.AcceptLink(t.Context(), link.ID, sam.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	h.AddDelegate(sam, dee, launch.ID)
	h.AddDelegate(kim, lee, late.ID)

	lateCheckin := "/goals/" + strconv.FormatInt(late.ID, 10) + "/checkin?open=dates"
	launchCheckin := "/goals/" + strconv.FormatInt(launch.ID, 10) + "/checkin?open=dates"
	nudge := "/goals/" + strconv.FormatInt(late.ID, 10) + "/nudge"
	for _, c := range []struct {
		viewer, label, href string
		primary             bool
	}{
		{"kim@example.com", "Check in", lateCheckin, true},
		{"lee@example.com", "Check in", lateCheckin, true},
		{"sam@example.com", "Check in", launchCheckin, true},
		{"dee@example.com", "Check in", launchCheckin, true},
		{"pat@example.com", "Nudge", nudge, false},
	} {
		row := followUpRowIn(t, getPage(t, signInClient(t, ts.URL, c.viewer), ts.URL+"/follow-ups?org=all"), late)
		followUpChipIn(t, row, "schedule-conflicts")
		label, href, primary := followUpFixIn(t, row)
		if label != c.label || href != c.href || primary != c.primary {
			t.Errorf("%s sees Fix %q → %s (primary %v), want %q → %s (primary %v)", c.viewer, label, href, primary, c.label, c.href, c.primary)
		}
		if form := findAllByTestID(findByTestID(t, row, "follow-up-fix"), "nudge"); c.label == "Nudge" && (len(form) != 1 || form[0].Data != "form") {
			t.Errorf("%s's Nudge is no Nudge form", c.viewer)
		}
	}

	checkin := getPage(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+lateCheckin)
	if dates := findByTestID(t, checkin, "checkin-dates-section"); dates.Data != "details" || !hasAttr(dates, "open") {
		t.Errorf("kim's Check in doesn't open the dates section")
	}

	pat := signInClient(t, ts.URL, "pat@example.com")
	resp := postForm(t, noRedirects(pat), ts.URL+nudge, url.Values{})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/follow-ups" {
		t.Fatalf("nudging got %d → %q, want 303 → /follow-ups", resp.StatusCode, resp.Header.Get("Location"))
	}
	cell := findByTestID(t, followUpRowIn(t, getPage(t, pat, ts.URL+"/follow-ups?org=all"), late), "follow-up-fix")
	disabled := findAll(cell, func(n *html.Node) bool { return n.Data == "button" && hasAttr(n, "disabled") })
	forms := findAll(cell, func(n *html.Node) bool { return n.Data == "form" })
	if len(disabled) != 1 || textOf(disabled[0]) != "Nudged today by Pat Okafor" || len(forms) != 0 {
		t.Errorf("pat sees Fix %s, want a disabled Nudged today by Pat Okafor", renderNode(cell))
	}
}

// A Proposed child in a Schedule conflict can't be checked in on or nudged,
// so its Owner and anyone else get Open Goal, its page, where its Owner
// finishes defining it; the parent's Owner still gets Check in on the parent.
func TestFollowUpFixOnAScheduleConflictWithAProposedChild(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	h.SignIn("pat@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	proposed := h.CreateGoal(kim, "Late plan", "It matters.")
	if _, err := h.Service.MarkGoalDated(t.Context(), proposed.Owner.ID, proposed.ID, june.AddDate(0, 1, 0)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	link := h.RequestLink(kim, proposed, launch, "")
	if _, err := h.Service.AcceptLink(t.Context(), link.ID, sam.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}

	page := "/goals/" + strconv.FormatInt(proposed.ID, 10)
	for _, c := range []struct {
		viewer, label, href string
		primary             bool
	}{
		{"kim@example.com", "Open Goal", page, false},
		{"pat@example.com", "Open Goal", page, false},
		{"sam@example.com", "Check in", "/goals/" + strconv.FormatInt(launch.ID, 10) + "/checkin?open=dates", true},
	} {
		row := followUpRowIn(t, getPage(t, signInClient(t, ts.URL, c.viewer), ts.URL+"/follow-ups?org=all"), proposed)
		followUpChipIn(t, row, "schedule-conflicts")
		label, href, primary := followUpFixIn(t, row)
		if label != c.label || href != c.href || primary != c.primary {
			t.Errorf("%s sees Fix %q → %s (primary %v), want %q → %s (primary %v)", c.viewer, label, href, primary, c.label, c.href, c.primary)
		}
		if forms := findAll(findByTestID(t, row, "follow-up-fix"), func(n *html.Node) bool { return n.Data == "form" }); len(forms) != 0 {
			t.Errorf("%s is offered a Nudge form on a Proposed Goal", c.viewer)
		}
	}
}

// Scoped to Mine, by ?org=mine, the Follow-ups page keeps only the Goals the viewer Owns or is a Delegate
// on, and its count line names Mine and counts those alone, as do its cards,
// while the sidebar still counts every flagged Goal. The Scope select shows
// the current scope; with no Managers it offers only Everyone and Mine.
func TestFollowUpsPageScopedToMine(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	own := h.ActiveGoal(sam, "Own work", "It matters.")
	delegated := h.ActiveGoal(kim, "Delegated work", "It matters.")
	h.AddDelegate(kim, sam, delegated.ID)
	other := h.ActiveGoal(kim, "Other work", "It matters.")
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")

	doc := getPage(t, client, ts.URL+"/follow-ups?org=mine")

	followUpRowIn(t, doc, own)
	followUpRowIn(t, doc, delegated)
	if hasFollowUpRowIn(doc, other) {
		t.Errorf("the page lists %q, which sam neither Owns nor is a Delegate on", other.Title)
	}
	if line, count := followUpsAttention(t, doc); line != "2 Goals to review in Mine, most urgent first." || count != "2" {
		t.Errorf("the count line doesn't count sam's 2 Goals in Mine: %q", line)
	}
	if card := followUpsCardIn(t, doc, "owner"); followUpsCardCount(t, card) != "2" || !slices.Contains(followUpsCardChips(card), "Check-in overdue · 2") {
		t.Errorf("the owner card doesn't count sam's 2 Goals: %s", renderNode(card))
	}
	if count := followUpsNavCount(t, doc); count != "2" {
		t.Errorf("the sidebar's Follow-ups count is %q, want sam's default, Mine's 2", count)
	}
	choices, value, label := selectChoices(scopeSelectIn(t, doc, "follow-ups-scope"))
	if value != "mine" || label != "Mine" {
		t.Errorf("the Scope select shows %q %q chosen, want Mine", value, label)
	}
	if want := []string{"Everyone", "Mine"}; !slices.Equal(choices, want) {
		t.Errorf("without Managers the Scope select offers %q, want %q", choices, want)
	}

	doc = getPage(t, client, ts.URL+"/follow-ups?org=all")

	followUpRowIn(t, doc, other)
	if line, count := followUpsAttention(t, doc); line != "3 Goals to review, most urgent first." || count != "3" {
		t.Errorf("for Everyone, the count line doesn't count all 3 Goals: %q", line)
	}
	if _, value, label := selectChoices(scopeSelectIn(t, doc, "follow-ups-scope")); value != "all" || label != "Everyone" {
		t.Errorf("for Everyone, the Scope select shows %q %q chosen, want Everyone", value, label)
	}
	if count := followUpsNavCount(t, doc); count != "2" {
		t.Errorf("for Everyone, the sidebar's Follow-ups count %q follows the page's scope", count)
	}
}

// The Follow-ups page has a value select per offered Dimension, in the Dimensions'
// order, by name, each named value and labelled by its Dimension's name, offering
// "<Dimension>: any", which scopes nothing, then the Dimension's values, a
// Retired one saying so.
func TestFollowUpsPageHasAValueSelectPerDimension(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	h.SignIn("sam@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Growth")
	h.CreateDimension(boss, "Quarter", "Q3")
	if err := h.Service.RetireDimensionValue(t.Context(), boss.ID, team.Values[1].ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	var dims []string
	for _, sel := range findAllByTestID(doc, "follow-ups-value") {
		dims = append(dims, attrOf(sel, "data-dimension"))
	}
	if !slices.Equal(dims, []string{"Quarter", "Team"}) {
		t.Fatalf("the value selects are for %q, want Quarter then Team", dims)
	}
	for dim, want := range map[string][]string{"Team": {"Team: any", "Platform", "Growth (retired)"}, "Quarter": {"Quarter: any", "Q3"}} {
		sel := followUpsValueSelectIn(t, doc, dim)
		if name := attrOf(sel, "name"); name != "value" {
			t.Errorf("the %s select is named %q, want value", dim, name)
		}
		if label := attrOf(sel, "aria-label"); label != dim {
			t.Errorf("the %s select is labelled %q", dim, label)
		}
		choices, value, _ := selectChoices(sel)
		if !slices.Equal(choices, want) {
			t.Errorf("the %s select offers %q, want %q", dim, choices, want)
		}
		options := findAll(sel, func(n *html.Node) bool { return n.Data == "option" })
		if len(options) == 0 || attrOf(options[0], "value") != "" || value != "" {
			t.Errorf("unscoped, the %s select's any option doesn't post nothing, chosen: %s", dim, renderNode(sel))
		}
	}
}

// Scoped to a Dimension value (?value=), the Follow-ups page keeps only the Goals
// that have it, counting those alone, and its Dimension's select offers
// "Team: any" then the Dimension's values, the chosen one selected. A value of
// a Retired Dimension, which has no select, or no value at all, scopes
// nothing.
func TestFollowUpsPageScopedToADimensionValue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Growth")
	region := h.CreateDimension(boss, "Region", "EMEA")
	platform, growth, emea := team.Values[0], team.Values[1], region.Values[0]
	onPlatform := h.ActiveGoal(sam, "Platform work", "It matters.")
	h.AssignGoalValue(onPlatform, platform)
	onGrowth := h.ActiveGoal(sam, "Growth work", "It matters.")
	h.AssignGoalValue(onGrowth, growth)
	h.AssignGoalValue(onGrowth, emea)
	untagged := h.ActiveGoal(sam, "Untagged work", "It matters.")
	if err := h.Service.RetireDimension(t.Context(), boss.ID, region.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")
	id := func(v domain.DimensionValue) string { return strconv.FormatInt(v.ID, 10) }

	doc := getPage(t, client, ts.URL+"/follow-ups?org=all&value="+id(platform))

	followUpRowIn(t, doc, onPlatform)
	for _, g := range []domain.Goal{onGrowth, untagged} {
		if hasFollowUpRowIn(doc, g) {
			t.Errorf("scoped to Platform, the page lists %q", g.Title)
		}
	}
	if line, count := followUpsAttention(t, doc); count != "1" || !strings.HasPrefix(line, "1 Goal to review,") {
		t.Errorf("scoped to Platform, the header doesn't count its 1 Goal: %q", line)
	}
	if count := followUpsCardCount(t, followUpsCardIn(t, doc, "owner")); count != "1" {
		t.Errorf("scoped to Platform, the owner card counts %s, want its 1 Goal", count)
	}
	if count := followUpsNavCount(t, doc); count != "3" {
		t.Errorf("scoped to Platform, the sidebar's Follow-ups count %q follows scope", count)
	}
	if sels := findAllByTestID(doc, "follow-ups-value"); len(sels) != 1 {
		t.Errorf("with Team the one offered Dimension, the page has %d value selects, want 1", len(sels))
	}
	if choices, value, label := selectChoices(followUpsValueSelectIn(t, doc, "Team")); !slices.Equal(choices, []string{"Team: any", "Platform", "Growth"}) || value != id(platform) || label != "Platform" {
		t.Errorf("scoped to Platform, the Team select offers %q with %q %q chosen, want Team: any, Platform, Growth with Platform", choices, value, label)
	}
	values := formSubmission(findByTestID(t, doc, "follow-ups-filters"))
	if values.Set("org", "mine"); values.Encode() != "org=mine&value="+id(platform) {
		t.Errorf("choosing Mine doesn't keep the value scope: %s", values.Encode())
	}

	for _, ignored := range []string{id(emea), "bogus"} {
		doc = getPage(t, client, ts.URL+"/follow-ups?org=all&value="+ignored)
		for _, g := range []domain.Goal{onPlatform, onGrowth, untagged} {
			followUpRowIn(t, doc, g)
		}
	}
}

// Scoped to values of two Dimensions, the Follow-ups page keeps only the flagged
// Goals carrying both, as the Goal list does; scoped to one, those carrying
// it. Of two values in one Dimension it keeps the Goals carrying either, as
// the Goal list does (#559), while its select shows the first chosen, and a
// value naming no offered one scopes nothing.
func TestFollowUpsPageScopedToValuesOfSeveralDimensions(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Growth")
	quarter := h.CreateDimension(boss, "Quarter", "Q3")
	platform, growth, q3 := team.Values[0], team.Values[1], quarter.Values[0]
	both := h.ActiveGoal(sam, "Platform in Q3", "It matters.")
	h.AssignGoalValue(both, platform)
	h.AssignGoalValue(both, q3)
	platformOnly := h.ActiveGoal(sam, "Platform any time", "It matters.")
	h.AssignGoalValue(platformOnly, platform)
	growthInQ3 := h.ActiveGoal(sam, "Growth in Q3", "It matters.")
	h.AssignGoalValue(growthInQ3, growth)
	h.AssignGoalValue(growthInQ3, q3)
	untagged := h.ActiveGoal(sam, "Untagged work", "It matters.")
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")
	id := func(v domain.DimensionValue) string { return strconv.FormatInt(v.ID, 10) }
	all := []domain.Goal{both, platformOnly, growthInQ3, untagged}

	for query, want := range map[string][]domain.Goal{
		"value=" + id(platform) + "&value=" + id(q3):     {both},
		"value=" + id(q3) + "&value=" + id(platform):     {both},
		"value=" + id(platform):                          {both, platformOnly},
		"value=&value=" + id(q3):                         {both, growthInQ3},
		"value=" + id(platform) + "&value=" + id(growth): {both, platformOnly, growthInQ3},
		"value=bogus&value=" + id(growth) + "&value=999": {growthInQ3},
	} {
		doc := getPage(t, client, ts.URL+"/follow-ups?"+query)
		for _, g := range all {
			if listed := hasFollowUpRowIn(doc, g); listed != slices.Contains(want, g) {
				t.Errorf("at ?%s, %q listed is %v", query, g.Title, listed)
			}
		}
	}

	doc := getPage(t, client, ts.URL+"/follow-ups?value="+id(platform)+"&value="+id(growth))

	if _, value, label := selectChoices(followUpsValueSelectIn(t, doc, "Team")); value != id(platform) || label != "Platform" {
		t.Errorf("of Platform then Growth, the Team select shows %q %q chosen, want Platform", value, label)
	}
	if _, value, label := selectChoices(followUpsValueSelectIn(t, doc, "Quarter")); value != "" || label != "Quarter: any" {
		t.Errorf("with no Quarter chosen, the Quarter select shows %q %q chosen, want Quarter: any", value, label)
	}
}

// The same ?value= selects the same Goals on the Goal list and Follow-ups:
// several values of one Dimension are ORed on both (#559, replacing #254's
// first value only), though Follow-ups' one select per Dimension shows the
// first.
func TestGoalListAndFollowUpsKeepEveryValueOfADimension(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Growth", "Core")
	platform, growth := team.Values[0], team.Values[1]
	red := func(title string, v domain.DimensionValue) domain.Goal {
		g := h.ActiveGoal(sam, title, "It matters.")
		h.AssignGoalValue(g, v)
		h.Checkin(sam, g.ID, domain.HealthRed, "Blocked.", "Unblock it.", h.Clock.Now().AddDate(0, 0, 14))
		return g
	}
	onPlatform, onGrowth, onCore := red("Platform work", platform), red("Growth work", growth), red("Core work", team.Values[2])
	client := signInClient(t, ts.URL, "boss@example.com")
	query := "?org=all&value=" + strconv.FormatInt(platform.ID, 10) + "&value=" + strconv.FormatInt(growth.ID, 10)

	listed := goalRowTitlesIn(goalListRowsIn(getPage(t, client, ts.URL+"/goals"+query)), onPlatform, onGrowth, onCore)
	slices.Sort(listed)
	if want := []string{"Growth work", "Platform work"}; !slices.Equal(listed, want) {
		t.Errorf("the Goal list at %s lists %q, want %q", query, listed, want)
	}
	doc := getPage(t, client, ts.URL+"/follow-ups"+query)
	for _, g := range []domain.Goal{onPlatform, onGrowth, onCore} {
		if got, want := hasFollowUpRowIn(doc, g), g.ID != onCore.ID; got != want {
			t.Errorf("Follow-ups at %s lists %q: %v, want %v", query, g.Title, got, want)
		}
	}
	if _, value, _ := selectChoices(followUpsValueSelectIn(t, doc, "Team")); value != strconv.FormatInt(platform.ID, 10) {
		t.Errorf("the Team select shows %q chosen, want Platform, the first", value)
	}
	if href := attrOf(followUpsCardIn(t, doc, "health"), "href"); !strings.Contains(href, "value="+strconv.FormatInt(growth.ID, 10)) {
		t.Errorf("the Not Green card's link %q drops Growth", href)
	}
}

// Scoped to values of two Dimensions, the group cards and Show all keep every
// value, and with nothing flagged there the empty line names each value with
// its Dimension, in the Dimensions' order.
func TestFollowUpsPageKeepsEveryValueInItsAddress(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	platform := h.CreateDimension(boss, "Team", "Platform").Values[0]
	q3 := h.CreateDimension(boss, "Quarter", "Q3").Values[0]
	g := h.ActiveGoal(sam, "Platform in Q3", "It matters.")
	h.AssignGoalValue(g, platform)
	h.AssignGoalValue(g, q3)
	h.ActiveGoal(sam, "Untagged work", "It matters.")
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")
	scope := "value=" + strconv.FormatInt(q3.ID, 10) + "&value=" + strconv.FormatInt(platform.ID, 10)

	doc := getPage(t, client, ts.URL+"/follow-ups?value="+strconv.FormatInt(platform.ID, 10)+"&value="+strconv.FormatInt(q3.ID, 10))

	href := attrOf(followUpsCardIn(t, doc, "owner"), "href")
	if href != "/follow-ups?group=owner&"+scope {
		t.Fatalf("the owner card's link %q doesn't keep both values", href)
	}
	doc = getPage(t, client, ts.URL+href)
	if all := attrOf(findByTestID(t, doc, "follow-ups-show-all"), "href"); all != "/follow-ups?"+scope {
		t.Errorf("Show all %q doesn't keep both values", all)
	}

	doc = getPage(t, client, ts.URL+"/follow-ups?group=admin&"+scope)

	if empty := findByTestID(t, doc, "follow-ups-empty"); !strings.HasPrefix(textOf(empty), "Nothing needs an Admin in Quarter Q3, Team Platform.") {
		t.Errorf("the empty line doesn't name both values with their Dimensions: %s", renderNode(empty))
	}
}

// With no Dimension offered, the Follow-ups page has no value select, and a value
// in its address scopes nothing.
func TestFollowUpsPageWithNoDimensionsHasNoValueSelect(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	team := h.CreateDimension(boss, "Team", "Platform")
	g := h.ActiveGoal(sam, "Platform work", "It matters.")
	h.AssignGoalValue(g, team.Values[0])
	other := h.ActiveGoal(sam, "Other work", "It matters.")
	if err := h.Service.RetireDimension(t.Context(), boss.ID, team.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	h.Clock.Advance(10 * day)

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups?value="+strconv.FormatInt(team.Values[0].ID, 10))

	if hasTestID(doc, "follow-ups-value") {
		t.Errorf("with no Dimension offered, the page has a value select")
	}
	followUpRowIn(t, doc, g)
	followUpRowIn(t, doc, other)
}

// Scoped to Mine and a value, the group cards and Show all keep the scope, so
// clicking a card narrows the scoped page to the group: the table lists only
// the Goals the scope and the group both keep, while the cards count every
// group within the scope.
func TestFollowUpsPageGroupsWithinScope(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	platform := h.CreateDimension(boss, "Team", "Platform").Values[0]
	ownCheckinOverdue := h.MarkTopLevel(boss, h.ActiveGoal(sam, "Own overdue", "It matters.")) // Check-in overdue only
	h.AssignGoalValue(ownCheckinOverdue, platform)
	ownUnlinked := h.ActiveGoal(sam, "Own unlinked", "It matters.") // Unlinked only
	h.AssignGoalValue(ownUnlinked, platform)
	ownUntagged := h.ActiveGoal(sam, "Own untagged", "It matters.")
	kims := h.ActiveGoal(kim, "Kim's platform work", "It matters.")
	h.AssignGoalValue(kims, platform)
	h.Clock.Advance(10 * day)
	h.Checkin(sam, ownUnlinked.ID, domain.HealthGreen, "On track.", "", time.Time{})
	client := signInClient(t, ts.URL, "sam@example.com")
	scope := "org=mine&value=" + strconv.FormatInt(platform.ID, 10)

	doc := getPage(t, client, ts.URL+"/follow-ups?"+scope)

	href := attrOf(followUpsCardIn(t, doc, "owner"), "href")
	if href != "/follow-ups?group=owner&"+scope {
		t.Fatalf("the owner card's link %q doesn't keep the scope", href)
	}

	doc = getPage(t, client, ts.URL+href)

	followUpRowIn(t, doc, ownCheckinOverdue)
	for _, g := range []domain.Goal{ownUnlinked, ownUntagged, kims} {
		if hasFollowUpRowIn(doc, g) {
			t.Errorf("scoped and filtered to owner, the page lists %q", g.Title)
		}
	}
	for key, want := range map[string]string{"owner": "1", "plan": "1"} {
		if got := followUpsCardCount(t, followUpsCardIn(t, doc, key)); got != want {
			t.Errorf("scoped and filtered to owner, the %s card counts %s, want %s", key, got, want)
		}
	}
	if all := attrOf(findByTestID(t, doc, "follow-ups-show-all"), "href"); all != "/follow-ups?"+scope {
		t.Errorf("Show all %q doesn't keep the scope", all)
	}
	values := formSubmission(findByTestID(t, doc, "follow-ups-filters"))
	if values.Set("org", "all"); values.Get("group") != "owner" || values.Get("value") != strconv.FormatInt(platform.ID, 10) {
		t.Errorf("choosing Everyone doesn't keep the group and value: %v", values)
	}
}

// The scope form applies itself under htmx as the Goal list's filter bar does:
// a change GETs the page and swaps in only #follow-ups-body, which holds the
// groups and the list but not the form, and the header strip holding the count
// out of band beside it, and pushes the address,
// while the page opts out of htmx's history snapshot so Back loads the earlier
// address afresh. The form carries the page's group and its Scope, Mine here
// as ?org=mine, so a value chosen while filtered keeps both, and without
// JavaScript it is a plain GET with an Apply button in <noscript>.
func TestFollowUpsScopeFormAppliesItself(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	platform := h.CreateDimension(boss, "Team", "Platform").Values[0]
	own := h.ActiveGoal(sam, "Own platform work", "It matters.")
	h.AssignGoalValue(own, platform)
	h.ActiveGoal(sam, "Own untagged", "It matters.")
	kims := h.ActiveGoal(kim, "Kim's platform work", "It matters.")
	h.AssignGoalValue(kims, platform)
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")

	doc := getPage(t, client, ts.URL+"/follow-ups?group=owner&org=mine")

	form := findByTestID(t, doc, "follow-ups-filters")
	if form.Data != "form" {
		t.Fatalf("the filters are a %s, not a form", form.Data)
	}
	for name, want := range map[string]string{
		"method":        "get",
		"action":        "/follow-ups",
		"hx-get":        "/follow-ups",
		"hx-trigger":    "submit, change",
		"hx-target":     "#follow-ups-body",
		"hx-select":     "#follow-ups-body",
		"hx-select-oob": "#follow-ups-head",
		"hx-swap":       "outerHTML",
		"hx-push-url":   "true",
		"hx-sync":       "this:replace",
		"hx-history":    "false",
	} {
		if got := attrOf(form, name); got != want {
			t.Errorf("scope form %s = %q, want %q", name, got, want)
		}
	}
	// A browser running script, as the parser reads the page, takes what a
	// <noscript> holds as text; without script it would be the Apply button.
	noscripts := findAll(form, func(n *html.Node) bool { return n.Data == "noscript" })
	if len(noscripts) != 1 || noscripts[0].FirstChild == nil {
		t.Fatalf("the scope form has %d <noscript>s, want one holding Apply", len(noscripts))
	}
	without := parsePage(t, noscripts[0].FirstChild.Data)
	apply := buttonNamed(without, "Apply")
	if apply == nil || len(submitButtons(without)) != 1 || attrOf(apply, "type") != "submit" || attrOf(apply, "class") != "btn" {
		t.Errorf("the scope form's <noscript> doesn't hold its one Apply button: %s", renderNode(without))
	}
	if buttonNamed(form, "Apply") != nil || len(submitButtons(form)) != 0 {
		t.Errorf("the scope form offers Apply outside its <noscript>: %s", renderNode(form))
	}
	if head := followUpsElementByID(t, doc, "follow-ups-head"); !hasTestID(head, "follow-ups-attention") {
		t.Errorf("#follow-ups-head lacks the count:\n%s", renderNode(head))
	}
	body := followUpsElementByID(t, doc, "follow-ups-body")
	for _, in := range []string{"follow-ups-groups", "follow-ups-table"} {
		if !hasTestID(body, in) {
			t.Errorf("#follow-ups-body lacks %s", in)
		}
	}
	if within(form, body) || len(findAll(body, func(n *html.Node) bool { return attrOf(n, "aria-label") == "Scope" })) > 0 {
		t.Errorf("#follow-ups-body holds the scope form or its Scope, which a swap would reset")
	}

	values := formSubmission(form)
	values.Set("value", strconv.FormatInt(platform.ID, 10))
	address := attrOf(form, "hx-get") + "?" + values.Encode()
	if want := "/follow-ups?group=owner&org=mine&value=" + strconv.FormatInt(platform.ID, 10); address != want {
		t.Fatalf("choosing Platform GETs %s, want %s", address, want)
	}

	doc = getPage(t, client, ts.URL+address)

	followUpRowIn(t, doc, own)
	if rows := followUpRowsIn(doc); len(rows) != 1 {
		t.Errorf("scoped to Mine and Platform and filtered to owner, the table has %d rows, want 1", len(rows))
	}
}

// A Health chip on the Follow-ups page shows one marker, its drawn shape (the
// dot), and no ■ ▲ ● character beside it (#452).
func TestFollowUpsHealthChipShowsOneShape(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(10 * day)

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	chip := findByTestID(t, followUpRowIn(t, doc, stalled), "follow-up-health")
	marks := findAll(chip, func(n *html.Node) bool { return n != chip && n.Data == "span" && !flatFrameHasClass(n, "sr-only") })
	if len(marks) != 1 || attrOf(marks[0], "class") != "dot" {
		t.Errorf("the row's Health chip doesn't show just its drawn shape: %s", renderNode(chip))
	}
	assertHealthChip(t, chip, "Green")
}

// The Follow-ups filters are the Goal list's chips: the Scope select sits in
// the same always-on Scope chip, and each Dimension's select in a chip whose
// "Team:" key shows once a value is chosen, so it reads "Team: any" or
// "Team: Platform", filled while set. That no rule of the page's own
// restyles the selects inside them (#406) is
// TestFollowUpsFilterChipSelectsKeepTheirStyles's.
func TestFollowUpsFiltersAreFilterChips(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	h.SignIn("sam@example.com")
	team := h.CreateDimension(boss, "Team", "Platform")
	client := signInClient(t, ts.URL, "sam@example.com")

	// filterChip is the chip around a filter's select: the classes of the
	// label it sits in, and the key the label opens with, its classes, whether
	// it's hidden from screen readers and what it reads.
	type filterChip struct{ class, keyClass, keyHidden, key string }
	chipAround := func(sel *html.Node) filterChip {
		label := sel.Parent
		if label == nil || label.Data != "label" {
			return filterChip{}
		}
		key := label.FirstChild
		for key != nil && key.Type != html.ElementNode {
			key = key.NextSibling
		}
		if key == nil || key.Data != "span" {
			return filterChip{class: attrOf(label, "class")}
		}
		return filterChip{attrOf(label, "class"), attrOf(key, "class"), attrOf(key, "aria-hidden"), textOf(key)}
	}
	// teamOption is one of the Team select's options as a person picks it.
	type teamOption struct {
		value, text string
		selected    bool
	}

	platform := strconv.FormatInt(team.Values[0].ID, 10)
	for _, c := range []struct {
		addr string
		// any and chosen are the Team select's "Team: any" and Platform
		// options: unset, "Team: any" is chosen; on Platform, Platform is,
		// which is what puts the chip in its on state (app.css's
		// .filter-chip:has(option:checked:not([value=""]))).
		any, chosen teamOption
	}{
		{"/follow-ups", teamOption{"", "Team: any", true}, teamOption{platform, "Platform", false}},
		{"/follow-ups?value=" + platform, teamOption{"", "Team: any", false}, teamOption{platform, "Platform", true}},
	} {
		addr := c.addr
		doc := getPage(t, client, ts.URL+addr)
		filters := findByTestID(t, doc, "follow-ups-filters")
		if got, want := chipAround(findByTestID(t, filters, "follow-ups-scope")), (filterChip{"filter-chip on", "chip-key", "true", "Scope:"}); got != want {
			t.Errorf("%s: the Scope select isn't in the Scope chip: got %+v, want %+v", addr, got, want)
		}
		value := findByTestID(t, filters, "follow-ups-value")
		if got, want := chipAround(value), (filterChip{"filter-chip", "chip-key chip-key-set", "true", "Team:"}); got != want || attrOf(value, "data-dimension") != "Team" {
			t.Errorf("%s: the Team select isn't in a Team chip: got %+v for dimension %q, want %+v", addr, got, attrOf(value, "data-dimension"), want)
		}
		var options []teamOption
		for _, o := range findAll(value, func(n *html.Node) bool { return n.Data == "option" }) {
			options = append(options, teamOption{attrOf(o, "value"), textOf(o), hasAttr(o, "selected")})
		}
		for _, want := range []teamOption{c.any, c.chosen} {
			if !slices.Contains(options, want) {
				t.Errorf("%s: the Team select has no %+v among %+v", addr, want, options)
			}
		}
	}
}

// orgScopedFollowUpTitles names the Goals the Follow-ups table lists, sorted,
// by matching each row against the Goals it could be.
func orgScopedFollowUpTitles(root *html.Node, goals ...domain.Goal) []string {
	var titles []string
	for _, row := range followUpRowsIn(root) {
		for _, g := range goals {
			if isFollowUpRowFor(row, g) {
				titles = append(titles, g.Title)
			}
		}
	}
	slices.Sort(titles)
	return titles
}

// With no scope in its address the Follow-ups page opens on the viewer's default:
// My org for someone with anyone under them, Mine for everyone else, each
// shown chosen and named in the count line. An explicit Scope is never
// overridden: ?org=all is Everyone, ?org=mine Mine and ?org=<id> that
// person's Org, while a malformed ?org=, or one naming no one, gives the
// default, as does the old ?mine=1, ignored like any unknown parameter.
func TestFollowUpsPageOpensOnTheViewersDefaultScope(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	org := newScopeOrgFixture(h)
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	maxID := strconv.FormatInt(org.Max.ID, 10)

	for _, viewer := range []struct {
		email, defaultValue, defaultLine string
		defaultTitles                    []string
		mine                             string
	}{
		{"mia@example.com", strconv.FormatInt(org.Mia.ID, 10), "2 Goals to review in My org, most urgent first.", []string{"Mia's goal", "Pia's goal"}, "Mia's goal"},
		{"pia@example.com", "mine", "1 Goal to review in Mine, most urgent first.", []string{"Pia's goal"}, "Pia's goal"},
	} {
		client := signInClient(t, ts.URL, viewer.email)
		doc := getPage(t, client, ts.URL+"/follow-ups")
		if line, count := followUpsAttention(t, doc); line != viewer.defaultLine || count != strconv.Itoa(len(viewer.defaultTitles)) {
			t.Errorf("%s's default: the count line = %q, want %q", viewer.email, line, viewer.defaultLine)
		}
		for _, c := range []struct {
			query, value string
			titles       []string
		}{
			{"", viewer.defaultValue, viewer.defaultTitles},
			{"?org=all", "all", []string{"Max's goal", "Mia's goal", "Oz's goal", "Pax's goal", "Pia's goal", "Vera's goal"}},
			{"?org=mine", "mine", []string{viewer.mine}},
			{"?org=" + maxID, maxID, []string{"Max's goal", "Pax's goal"}},
			{"?org=abc", viewer.defaultValue, viewer.defaultTitles},
			{"?org=999999", viewer.defaultValue, viewer.defaultTitles},
			{"?mine=1", viewer.defaultValue, viewer.defaultTitles},
		} {
			doc := getPage(t, client, ts.URL+"/follow-ups"+c.query)
			if got := orgScopedFollowUpTitles(doc, org.goals()...); !slices.Equal(got, c.titles) {
				t.Errorf("%s, /follow-ups%s: rows = %q, want %q", viewer.email, c.query, got, c.titles)
			}
			if _, value, _ := selectChoices(scopeSelectIn(t, doc, "follow-ups-scope")); value != c.value {
				t.Errorf("%s, /follow-ups%s: the Scope select shows %q chosen, want %q", viewer.email, c.query, value, c.value)
			}
		}
	}
}

// Everyone is an explicit ?org=all on the Follow-ups page: the Scope select
// submits it, Show everything links to it, and a page showing Everyone keeps
// it in its group cards' links and Show all, since a bare /follow-ups is the
// viewer's default.
func TestFollowUpsPageNamesEveryoneAsOrgAll(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	newScopeOrgFixture(h)
	lee := h.SignInNamed("lee@example.com", "Lee Lund")
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "mia@example.com")

	doc := getPage(t, client, ts.URL+"/follow-ups?org=all")
	sel := scopeSelectIn(t, doc, "follow-ups-scope")
	if everyone := followUpsOptionIn(t, sel, "all"); textOf(everyone) != "Everyone" || !hasAttr(everyone, "selected") {
		t.Errorf("the Scope select's Everyone doesn't submit org=all: %s", renderNode(sel))
	}
	if href := attrOf(followUpsCardIn(t, doc, "owner"), "href"); href != "/follow-ups?group=owner&org=all" {
		t.Errorf("on Everyone, the owner card links to %q, want it to keep org=all", href)
	}
	doc = getPage(t, client, ts.URL+"/follow-ups?group=owner&org=all")
	if href := attrOf(findByTestID(t, doc, "follow-ups-show-all"), "href"); href != "/follow-ups?org=all" {
		t.Errorf("on Everyone, Show all links to %q, want it to keep org=all", href)
	}

	lees := signInClient(t, ts.URL, lee.Email)
	empty := findByTestID(t, getPage(t, lees, ts.URL+"/follow-ups"), "follow-ups-empty")
	if everything := linkTo(empty, "/follow-ups?org=all"); !strings.Contains(textOf(empty), "No Goals need attention in Mine.") || everything == nil || textOf(everything) != "Show everything" {
		t.Errorf("an empty default doesn't say so with Show everything to Everyone: %s", renderNode(empty))
	}
}

// An Admin's job in Needs an Admin is company-wide: that group lists every
// Ownerless and halted-parent Goal whatever the page's scope, which stays the
// one its address names, the viewer's default with none, for an Admin with no
// one under them too. Its card counts the company's, keeping the page's scope
// and values in its link, so its count and its table agree, and Show all and
// the other cards from there come back to the page's scope.
func TestFollowUpsNeedsAnAdminIsCompanyWide(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	org := newScopeOrgFixture(h)
	boss := h.SignIn("boss@example.com")
	team := h.CreateDimension(boss, "Team", "Core", "Edge")
	core, edge := team.Values[0], team.Values[1]
	h.AssignGoalValue(org.PaxGoal, core)
	h.AssignGoalValue(org.MiaGoal, core)
	h.AssignGoalValue(org.PiaGoal, edge)
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, org.Pax.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)

	admin := signInClient(t, ts.URL, "boss@example.com")
	doc := getPage(t, admin, ts.URL+"/follow-ups?group=admin")
	if got := orgScopedFollowUpTitles(doc, org.goals()...); !slices.Equal(got, []string{"Pax's goal"}) {
		t.Errorf("Needs an Admin for an Admin with no one under them: rows = %q, want Pax's Ownerless Goal", got)
	}
	if _, value, _ := selectChoices(scopeSelectIn(t, doc, "follow-ups-scope")); value != "mine" {
		t.Errorf("Needs an Admin with no scope shows %q chosen, want the Admin's default, Mine", value)
	}
	if line, count := followUpsAttention(t, doc); line != "1 Goal waiting on an Admin, most urgent first." || count != "1" {
		t.Errorf("Needs an Admin's header doesn't say 1 Goal needs an Admin across the company: %q", line)
	}

	mia := signInClient(t, ts.URL, "mia@example.com")
	coreID := strconv.FormatInt(core.ID, 10)
	for _, c := range []struct{ query, count, href string }{
		{"", "1", "/follow-ups?group=admin"},
		{"?org=mine", "1", "/follow-ups?group=admin&org=mine"},
		{"?org=all", "1", "/follow-ups?group=admin&org=all"},
		{"?value=" + coreID, "1", "/follow-ups?group=admin&value=" + coreID},
	} {
		card := followUpsCardIn(t, getPage(t, mia, ts.URL+"/follow-ups"+c.query), "admin")
		if followUpsCardCount(t, card) != c.count || !slices.Contains(followUpsCardChips(card), "Ownerless · 1") {
			t.Errorf("/follow-ups%s: the Needs an Admin card doesn't count the company's 1: %s", c.query, renderNode(card))
		}
		if href := attrOf(card, "href"); href != c.href {
			t.Errorf("/follow-ups%s: the Needs an Admin card links to %q, want %q", c.query, href, c.href)
		}
	}
	doc = getPage(t, mia, ts.URL+"/follow-ups?group=admin&org=mine")
	if got := orgScopedFollowUpTitles(doc, org.goals()...); !slices.Equal(got, []string{"Pax's goal"}) {
		t.Errorf("Needs an Admin in Mia's Mine: rows = %q, want Pax's Ownerless Goal", got)
	}
	if count := followUpsCardCount(t, followUpsCardIn(t, doc, "admin")); count != "1" {
		t.Errorf("Needs an Admin in Mia's Mine: the card counts %s, want 1", count)
	}
	doc = getPage(t, mia, ts.URL+"/follow-ups?value="+strconv.FormatInt(edge.ID, 10))
	if card := followUpsCardIn(t, doc, "admin"); card.Data != "div" || !strings.Contains(textOf(card), "Nothing needs an Admin.") {
		t.Errorf("the Needs an Admin card doesn't keep the page's values: %s", renderNode(card))
	}

	miaID := strconv.FormatInt(org.Mia.ID, 10)
	doc = getPage(t, mia, ts.URL+"/follow-ups?group=admin&org="+miaID)
	if _, value, _ := selectChoices(scopeSelectIn(t, doc, "follow-ups-scope")); value != miaID {
		t.Errorf("Needs an Admin in Mia's org shows %q chosen, want %s", value, miaID)
	}
	if all := attrOf(findByTestID(t, doc, "follow-ups-show-all"), "href"); all != "/follow-ups?org="+miaID {
		t.Errorf("Show all from Needs an Admin links to %q, want it to keep Mia's org", all)
	}
	if href := attrOf(followUpsCardIn(t, doc, "owner"), "href"); href != "/follow-ups?group=owner&org="+miaID {
		t.Errorf("the owner card from Needs an Admin links to %q, want it to keep Mia's org", href)
	}
}

// With nothing for an Admin anywhere, Needs an Admin says so in one line, with
// nowhere wider to link, whatever the page's scope.
func TestFollowUpsNeedsAnAdminWithNothingSaysSo(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, query := range []string{"", "&org=mine", "&org=all"} {
		doc := getPage(t, client, ts.URL+"/follow-ups?group=admin"+query)
		if empty := findByTestID(t, doc, "follow-ups-empty"); textOf(empty) != "Nothing needs an Admin." {
			t.Errorf("group=admin%s: the empty line doesn't say only that nothing needs an Admin: %s", query, renderNode(empty))
		}
		if strings.Contains(textOf(doc), "Show everything") {
			t.Errorf("group=admin%s: the empty line offers Show everything", query)
		}
	}
}

// An Admin whose own scope has nothing flagged still sees, after the empty
// line, the Needs an Admin card counting the company's, the other cards blank;
// anyone else sees only the empty line.
func TestFollowUpsShowsAnAdminTheirCardOnAnEmptyScope(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	h.SignIn("kim@example.com")
	h.ActiveGoal(sam, "Orphaned work", "It matters.")
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	ts := newServer(t, h)

	doc := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/follow-ups")
	empty := findByTestID(t, doc, "follow-ups-empty")
	if !strings.Contains(textOf(empty), "No Goals need attention in Mine.") {
		t.Errorf("the Admin's empty Mine doesn't say so: %s", renderNode(empty))
	}
	card := followUpsCardIn(t, doc, "admin")
	if card.Data != "a" || followUpsCardCount(t, card) != "1" || !slices.Contains(followUpsCardChips(card), "Ownerless · 1") {
		t.Errorf("the Admin's Needs an Admin card doesn't count the company's 1: %s", renderNode(card))
	}
	if !precedes(empty, findByTestID(t, doc, "follow-ups-groups")) {
		t.Errorf("the cards come before the empty line")
	}
	for key, blank := range map[string]string{"owner": "Nothing is waiting on a Check-in.", "followup": "No Action Item is overdue.", "plan": "Every Goal is placed and its dates fit."} {
		if card := followUpsCardIn(t, doc, key); card.Data != "div" || !strings.Contains(textOf(card), blank) {
			t.Errorf("the Admin's %s card isn't blank: %s", key, renderNode(card))
		}
	}
	if tables := findAll(doc, func(n *html.Node) bool { return n.Data == "table" }); len(tables) > 0 {
		t.Errorf("the Admin's empty Mine has a table")
	}
	for _, gone := range []string{"follow-ups-attention", "follow-ups-show-all"} {
		if hasTestID(doc, gone) {
			t.Errorf("the Admin's empty Mine has %s", gone)
		}
	}

	doc = getPage(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+"/follow-ups")
	findByTestID(t, doc, "follow-ups-empty")
	for _, gone := range []string{"follow-ups-groups", "follow-ups-group-"} {
		if found := followUpsWithTestIDPrefix(doc, gone); len(found) > 0 {
			t.Errorf("a non-Admin's empty Mine has %s: %s", gone, renderNode(found[0]))
		}
	}
}

// The sidebar's Follow-ups count is the viewer's default scope, as many as a bare
// /follow-ups lists: My org's for a manager, Mine's, Delegate Goals included, for
// someone with no one under them, an Admin among them.
func TestSidebarFollowUpsCountIsTheViewersDefaultScope(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	org := newScopeOrgFixture(h)
	boss := h.SignIn("boss@example.com")
	h.ActiveGoal(boss, "Boss's goal", "It matters.")
	h.AddDelegate(org.Oz, boss, org.OzGoal.ID)
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)

	for _, c := range []struct {
		email string
		count int
	}{
		{"vera@example.com", 5},
		{"mia@example.com", 2},
		{"pia@example.com", 1},
		{"boss@example.com", 2},
	} {
		client := signInClient(t, ts.URL, c.email)
		if rows := len(followUpRowsIn(getPage(t, client, ts.URL+"/follow-ups"))); rows != c.count {
			t.Errorf("%s: a bare /follow-ups lists %d rows, want %d", c.email, rows, c.count)
		}
		for _, path := range []string{"/home", "/goals", "/follow-ups"} {
			if count := followUpsNavCount(t, getPage(t, client, ts.URL+path)); count != strconv.Itoa(c.count) {
				t.Errorf("%s on %s: the nav's Follow-ups count %q isn't the %d a bare /follow-ups lists", c.email, path, count, c.count)
			}
		}
	}
}

// Scoped to My org, the Follow-ups page keeps the Goals whose Owner is in the
// viewer's org, their own included: for a VP every level below them, for a
// manager only their part, and its count line names it. The header form's one
// Scope select, its first control, offers Everyone, Mine, My org, then each
// other person's org under Other orgs; My org only to a viewer with anyone
// under them. There is no scope toggle beside the count.
func TestFollowUpsPageScopedToMyOrg(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	org := newScopeOrgFixture(h)
	boss := h.SignIn("boss@example.com")
	h.CreateDimension(boss, "Team", "Platform")
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)

	vera := signInClient(t, ts.URL, "vera@example.com")
	doc := getPage(t, vera, ts.URL+"/follow-ups?org=all")
	choices, value, label := selectChoices(scopeSelectIn(t, doc, "follow-ups-scope"))
	if want := []string{
		"Everyone", "Mine", "My org",
		"Other orgs: Max Marsh (max@example.com)", "Other orgs: Mia Moss (mia@example.com)", "Other orgs: Oz Ortiz (oz@example.com)",
		"Other orgs: Pax Penn (pax@example.com)", "Other orgs: Pia Park (pia@example.com)",
	}; !slices.Equal(choices, want) {
		t.Errorf("the Scope select offers %q, want %q", choices, want)
	}
	if value != "all" || label != "Everyone" {
		t.Errorf("for Everyone, the Scope select shows %q %q chosen, want Everyone", value, label)
	}
	var selects []string
	for _, sel := range findAll(findByTestID(t, doc, "follow-ups-filters"), func(n *html.Node) bool { return n.Data == "select" }) {
		selects = append(selects, attrOf(sel, "data-testid"))
	}
	if !slices.Equal(selects, []string{"follow-ups-scope", "follow-ups-value"}) {
		t.Errorf("the header form's selects are %q, want the Scope select first, ahead of the value select", selects)
	}
	toggles := findAll(doc, func(n *html.Node) bool { return flatFrameHasClass(n, "rk-scope") })
	if len(toggles) > 0 || len(followUpsWithTestIDPrefix(doc, "follow-ups-scope-")) > 0 {
		t.Errorf("the Follow-ups page still has a scope toggle")
	}
	if line, count := followUpsAttention(t, doc); line != count+" Goals to review, most urgent first." {
		t.Errorf("for Everyone, the count line changed: %q", line)
	}

	doc = getPage(t, vera, scopeOrgURL(ts.URL, "/follow-ups", org.Vera))
	if got, want := orgScopedFollowUpTitles(doc, org.goals()...), []string{"Max's goal", "Mia's goal", "Pax's goal", "Pia's goal", "Vera's goal"}; !slices.Equal(got, want) {
		t.Errorf("Vera's My org: rows = %q, want %q", got, want)
	}
	if line, count := followUpsAttention(t, doc); line != "5 Goals to review in My org, most urgent first." || count != "5" {
		t.Errorf("Vera's My org: the count line doesn't count 5 Goals in My org: %q", line)
	}
	if _, value, label := selectChoices(scopeSelectIn(t, doc, "follow-ups-scope")); value != strconv.FormatInt(org.Vera.ID, 10) || label != "My org" {
		t.Errorf("scoped to My org, the Scope select shows %q %q chosen", value, label)
	}

	mia := signInClient(t, ts.URL, "mia@example.com")
	doc = getPage(t, mia, scopeOrgURL(ts.URL, "/follow-ups", org.Mia))
	if got, want := orgScopedFollowUpTitles(doc, org.goals()...), []string{"Mia's goal", "Pia's goal"}; !slices.Equal(got, want) {
		t.Errorf("Mia's My org: rows = %q, want %q", got, want)
	}

	pia := signInClient(t, ts.URL, "pia@example.com")
	doc = getPage(t, pia, ts.URL+"/follow-ups")
	if choices, _, _ := selectChoices(scopeSelectIn(t, doc, "follow-ups-scope")); slices.Contains(choices, "My org") {
		t.Errorf("Pia, with no one under her, is offered My org: %q", choices)
	}
}

// Anyone may scope the Follow-ups page to another person's org, picked by Name
// with their email, and the page names whose org it is. A Goal is in an org
// by its Owner: a Delegate in the org doesn't bring in a Goal owned outside
// it, and an Ownerless Goal stays in the org its Departed Owner was in.
func TestFollowUpsPageScopedToAnotherPersonsOrg(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	org := newScopeOrgFixture(h)
	boss := h.SignIn("boss@example.com")
	h.AddDelegate(org.Oz, org.Pax, org.OzGoal.ID)
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, org.Pax.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	pia := signInClient(t, ts.URL, "pia@example.com")
	maxID := strconv.FormatInt(org.Max.ID, 10)

	doc := getPage(t, pia, ts.URL+"/follow-ups")
	orgSel := scopeSelectIn(t, doc, "follow-ups-scope")
	if max := followUpsOptionIn(t, orgSel, maxID); textOf(max) != "Max Marsh (max@example.com)" || hasAttr(max, "selected") {
		t.Errorf("the Scope select doesn't offer Max by Name and email: %s", renderNode(orgSel))
	}

	doc = getPage(t, pia, scopeOrgURL(ts.URL, "/follow-ups", org.Max))
	if got, want := orgScopedFollowUpTitles(doc, org.goals()...), []string{"Max's goal", "Pax's goal"}; !slices.Equal(got, want) {
		t.Errorf("Max's org: rows = %q, want %q (Pax's Ownerless Goal, not Oz's that Pax is a Delegate on)", got, want)
	}
	if line, count := followUpsAttention(t, doc); line != "2 Goals to review in Max Marsh's org, most urgent first." || count != "2" {
		t.Errorf("the count line doesn't name Max's org as its scope: %q", line)
	}
	orgSel = scopeSelectIn(t, doc, "follow-ups-scope")
	if max := followUpsOptionIn(t, orgSel, maxID); textOf(max) != "Max Marsh (max@example.com)" || !hasAttr(max, "selected") || followUpsSelected(orgSel) != 1 {
		t.Errorf("Max's org isn't the one chosen scope: %s", renderNode(orgSel))
	}
}

// Whether the Follow-ups page offers Orgs is the company's answer, not the
// viewer's: with Vera managing Mia, Oz, who has no Manager and no one under
// them, can open a link to Vera's org and sees it scoped and chosen.
func TestFollowUpsPageScopesAnOrgLinkForAViewerOutsideAnyOrg(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	vera := h.SignInNamed("vera@example.com", "Vera Vance")
	mia := h.SignInNamed("mia@example.com", "Mia Moss")
	oz := h.SignInNamed("oz@example.com", "Oz Ortiz")
	h.SetManager(mia, vera)
	veraGoal := h.ActiveGoal(vera, "Vera's goal", "It matters.")
	ozGoal := h.ActiveGoal(oz, "Oz's goal", "It matters.")
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "oz@example.com")

	if got := orgScopedFollowUpTitles(getPage(t, client, ts.URL+"/follow-ups?org=all"), veraGoal, ozGoal); len(got) != 2 {
		t.Fatalf("for Everyone, rows = %q, want both Check-in overdue Goals", got)
	}
	doc := getPage(t, client, scopeOrgURL(ts.URL, "/follow-ups", vera))
	if got, want := orgScopedFollowUpTitles(doc, veraGoal, ozGoal), []string{"Vera's goal"}; !slices.Equal(got, want) {
		t.Errorf("Oz in Vera's org: rows = %q, want %q", got, want)
	}
	if _, value, _ := selectChoices(scopeSelectIn(t, doc, "follow-ups-scope")); value != strconv.FormatInt(vera.ID, 10) {
		t.Errorf("Oz in Vera's org: the Scope select shows %q chosen, want %d", value, vera.ID)
	}
}

// The org lives in the Follow-ups page's address, so its group cards, Show all
// and the scope form keep it, and a reload shows it again; the form keeps no
// Mine of its own, so choosing Everyone from there lists every flagged Goal.
// A ?org= naming no one, or malformed, gives the viewer's default, Mine for
// Oz, the old ?mine=1 beside it changing nothing, and an org with nothing
// needing attention says so.
func TestFollowUpsPageKeepsTheOrgInItsAddress(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	org := newScopeOrgFixture(h)
	lee := h.SignInNamed("lee@example.com", "Lee Lund")
	h.SetManager(lee, org.Vera)
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "oz@example.com")
	orgID := strconv.FormatInt(org.Mia.ID, 10)

	doc := getPage(t, client, ts.URL+"/follow-ups?org="+orgID)
	filters := findByTestID(t, doc, "follow-ups-filters")
	if mine := formFields(filters, "mine"); len(mine) > 0 {
		t.Errorf("the scope form carries a mine field: %s", renderNode(filters))
	}
	if _, value, _ := selectChoices(scopeSelectIn(t, doc, "follow-ups-scope")); value != orgID {
		t.Errorf("Mia's org: the Scope select shows %q chosen, want %s", value, orgID)
	}
	values := formSubmission(filters)
	values.Set("org", "all")
	if got := orgScopedFollowUpTitles(getPage(t, client, ts.URL+"/follow-ups?"+values.Encode()), org.goals()...); len(got) != 6 {
		t.Errorf("choosing Everyone from Mia's org: rows = %q, want all 6", got)
	}
	address := attrOf(followUpsCardIn(t, doc, "owner"), "href")
	if want := "/follow-ups?group=owner&org=" + orgID; address != want {
		t.Fatalf("the owner card links to %s, want %s", address, want)
	}

	doc = getPage(t, client, ts.URL+address)
	if got, want := orgScopedFollowUpTitles(doc, org.goals()...), []string{"Mia's goal", "Pia's goal"}; !slices.Equal(got, want) {
		t.Errorf("grouped in Mia's org: rows = %q, want %q", got, want)
	}
	if all := findByTestID(t, doc, "follow-ups-show-all"); attrOf(all, "href") != "/follow-ups?org="+orgID {
		t.Errorf("Show all drops the org: %s", renderNode(all))
	}
	if values := formSubmission(findByTestID(t, doc, "follow-ups-filters")); values.Get("org") != orgID || values.Get("group") != "owner" {
		t.Errorf("the scope form doesn't carry the org and group: %v", values)
	}

	for _, raw := range []string{"abc", "999999", ""} {
		doc := getPage(t, client, ts.URL+"/follow-ups?org="+raw)
		if got := orgScopedFollowUpTitles(doc, org.goals()...); !slices.Equal(got, []string{"Oz's goal"}) {
			t.Errorf("org=%q: rows = %q, want Oz's default, Mine", raw, got)
		}
		if _, value, label := selectChoices(scopeSelectIn(t, doc, "follow-ups-scope")); value != "mine" || label != "Mine" {
			t.Errorf("org=%q: the Scope select shows %q %q chosen, want Oz's default, Mine", raw, value, label)
		}
		doc = getPage(t, client, ts.URL+"/follow-ups?mine=1&org="+raw)
		if got := orgScopedFollowUpTitles(doc, org.goals()...); !slices.Equal(got, []string{"Oz's goal"}) {
			t.Errorf("org=%q beside the old Mine parameter: rows = %q, want Oz's default, Mine", raw, got)
		}
	}

	doc = getPage(t, client, scopeOrgURL(ts.URL, "/follow-ups", lee))
	if empty := findByTestID(t, doc, "follow-ups-empty"); !strings.Contains(textOf(empty), "No Goals need attention in Lee Lund's org.") {
		t.Errorf("an empty org doesn't say so: %s", renderNode(empty))
	}
}

// The scope parameter is ?org=; the old ?chain= is no scope at all, ignored
// like any unknown parameter, so the page is on the viewer's default, Vera's
// My org, and not Mia's.
func TestFollowUpsPageIgnoresTheOldChainParameter(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	org := newScopeOrgFixture(h)
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	vera := signInClient(t, ts.URL, "vera@example.com")

	unscoped := orgScopedFollowUpTitles(getPage(t, vera, ts.URL+"/follow-ups"), org.goals()...)
	doc := getPage(t, vera, ts.URL+"/follow-ups?chain="+strconv.FormatInt(org.Mia.ID, 10))
	if got := orgScopedFollowUpTitles(doc, org.goals()...); !slices.Equal(got, unscoped) || len(got) != 5 {
		t.Errorf("?chain=: rows = %q, want the unscoped %q", got, unscoped)
	}
	sel := scopeSelectIn(t, doc, "follow-ups-scope")
	if mine := followUpsOptionIn(t, sel, strconv.FormatInt(org.Vera.ID, 10)); textOf(mine) != "My org" || !hasAttr(mine, "selected") || followUpsSelected(sel) != 1 {
		t.Errorf("?chain=: Vera's default isn't the one chosen scope: %s", renderNode(sel))
	}
}

// With no Managers anywhere, as when no directory is synced, the Follow-ups page's
// Scope select offers no org, only Everyone and Mine, and a ?org= link names
// no one, so gives the viewer's default, Mine.
func TestFollowUpsPageOffersNoOrgWithoutManagers(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	h.ActiveGoal(sam, "Sam's", "It matters.")
	h.ActiveGoal(kim, "Kim's", "It matters.")
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	doc := getPage(t, client, ts.URL+"/follow-ups?org="+strconv.FormatInt(kim.ID, 10))
	if choices, _, _ := selectChoices(scopeSelectIn(t, doc, "follow-ups-scope")); !slices.Equal(choices, []string{"Everyone", "Mine"}) {
		t.Errorf("without Managers the Scope select offers %q, want only Everyone and Mine", choices)
	}
	if rows := followUpRowsIn(doc); len(rows) != 1 {
		t.Errorf("without Managers a ?org= scoped the page to %d Goals, want Mine's 1", len(rows))
	}
	if _, value, _ := selectChoices(scopeSelectIn(t, doc, "follow-ups-scope")); value != "mine" {
		t.Errorf("without Managers a ?org= chose %q, want the default Mine", value)
	}
}

// A Follow-ups schedule-conflict chip says the delivery date the way the Goal page
// it links to does, in this year and the next (ticket #276).
func TestFollowUpsScheduleConflictDateReadsAsTheGoalPageDoes(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	client := signInClient(t, ts.URL, "sam@example.com")
	for _, c := range []struct {
		parentDue, due time.Time
		shown          string
	}{
		{time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 28, 0, 0, 0, 0, time.UTC), "Dec 28"},
		{time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 10, 0, 0, 0, 0, time.UTC), "Jan 10, 2027"},
	} {
		parent := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Parent due "+c.shown, c.parentDue))
		child := h.ActiveGoalDue(sam, "Child due "+c.shown, c.due)
		h.RequestLink(sam, child, parent, "")

		chip := followUpChipIn(t, followUpRowIn(t, getPage(t, client, ts.URL+"/follow-ups"), child), "schedule-conflicts")
		if !strings.Contains(textOf(chip), "due "+c.shown+",") {
			t.Errorf("the chip doesn't say due %s: %s", c.shown, renderNode(chip))
		}
		delivery := findByTestID(t, getPage(t, client, goalPageURL(ts.URL, child)), "goal-delivery-date")
		if bold := findAll(delivery, func(n *html.Node) bool { return n.Data == "strong" }); len(bold) != 1 || textOf(bold[0]) != c.shown {
			t.Errorf("the Goal page doesn't show its delivery date as %s, as its Follow-ups chip does: %s", c.shown, renderNode(delivery))
		}
	}
}

// raiseFollowUpActionItem has author, who saves and publishes a Report covering
// goal, raise an Action Item about it, owned by owner and due dueInDays from
// today, so a negative dueInDays is already overdue.
func raiseFollowUpActionItem(t *testing.T, h *testsupport.Harness, author domain.Account, goal domain.Goal, owner domain.Account, text string, dueInDays int) domain.ActionItem {
	t.Helper()
	pub := h.PublishReport(author, h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{
		Name: "MBR · " + text, Mode: domain.ReportModePicked, Picked: []int64{goal.ID},
	}))
	item, err := h.Service.RaiseActionItem(t.Context(), author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, GoalID: goal.ID, Text: text, OwnerID: owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, dueInDays),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem %q: %v", text, err)
	}
	return item
}

// A Green Goal with an Action Item two days past its due date is on the Follow-ups
// page with a chip, drawn like Path to Green overdue's, saying how long, and
// its Health is still Green: an overdue Action Item doesn't change a Goal's
// Health.
func TestFollowUpsPageListsGoalsWithOverdueActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	launch := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Launch in EU", "Expand the market."))
	h.Checkin(sam, launch.ID, domain.HealthGreen, "On track.", "", time.Time{})
	raiseFollowUpActionItem(t, h, ada, launch, sam, "Book the legal review.", -2)

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	row := followUpRowIn(t, doc, launch)
	assertFollowUpChip(t, followUpChipIn(t, row, "action-item-overdue"), "badge st", "Action Item 2 days overdue")
	assertHealthChip(t, findByTestID(t, row, "follow-up-health"), "Green")
}

// A row flagged only for an overdue Action Item has one Fix, See Action
// Items, linking to its Goal page's Action Items.
func TestFollowUpFixOnAnOverdueActionItemIsSeeActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	launch := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Launch in EU", "Expand the market."))
	raiseFollowUpActionItem(t, h, ada, launch, sam, "Book the legal review.", -2)

	for _, who := range []string{"sam@example.com", "ada@example.com"} {
		doc := getPage(t, signInClient(t, ts.URL, who), ts.URL+"/follow-ups?org=all")

		row := followUpRowIn(t, doc, launch)
		if got := followUpKindsIn(row); !slices.Equal(got, []string{"action-item-overdue"}) {
			t.Fatalf("%s: row's signals = %q, want only action-item-overdue", who, got)
		}
		label, href, primary := followUpFixIn(t, row)
		if want := "/goals/" + strconv.FormatInt(launch.ID, 10) + "#action-items"; label != "See Action Items" || href != want || primary {
			t.Errorf("%s: Fix = %q → %q (primary %v), want See Action Items → %q", who, label, href, primary, want)
		}
	}
}

// Action Items overdue is a group of its own, its card after Owner needs to
// update's: filtered to it, the page lists only the Goals an overdue Action
// Item flags. With none, its card is muted and says no Action Item is
// overdue.
func TestFollowUpsPageGroupsOverdueActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)
	launch := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Launch in EU", "Expand the market."))
	client := signInClient(t, ts.URL, "sam@example.com")

	doc := getPage(t, client, ts.URL+"/follow-ups?org=all")

	if card := followUpsCardIn(t, doc, "followup"); card.Data != "div" || !slices.Contains(followUpClassesOf(card), "rk-zero") || !strings.Contains(textOf(card), "No Action Item is overdue.") {
		t.Errorf("with no overdue Action Item, the followup card isn't muted saying so: %s", renderNode(card))
	}

	raiseFollowUpActionItem(t, h, ada, launch, sam, "Book the legal review.", -2)
	doc = getPage(t, client, ts.URL+"/follow-ups?org=all")

	card := followUpsCardIn(t, doc, "followup")
	if card.Data != "a" || attrOf(card, "href") != "/follow-ups?group=followup&org=all" || !strings.HasPrefix(textOf(card), "Action Items overdue ") ||
		followUpsCardCount(t, card) != "1" || !slices.Contains(followUpsCardChips(card), "Action Item overdue · 1") {
		t.Errorf("followup card doesn't link to its group, counting 1 with an Action Item overdue · 1 chip: %s", renderNode(card))
	}
	if chip := followUpChipIn(t, card, "action-item-overdue"); !slices.Equal(followUpClassesOf(chip), []string{"badge", "st"}) {
		t.Errorf("the followup card's chip isn't drawn like the table's: %s", renderNode(chip))
	}
	if owner, plan := followUpsCardIn(t, doc, "owner"), followUpsCardIn(t, doc, "plan"); !precedes(owner, card) || !precedes(card, plan) {
		t.Errorf("the followup card isn't between the owner and plan cards")
	}

	doc = getPage(t, client, ts.URL+"/follow-ups?group=followup&org=all")

	followUpChipIn(t, followUpRowIn(t, doc, launch), "action-item-overdue")
	if hasFollowUpRowIn(doc, silent) {
		t.Errorf("filtered to followup, the page lists %q, flagged only outside it", silent.Title)
	}
	if card := followUpsCardIn(t, doc, "followup"); attrOf(card, "aria-current") != "page" {
		t.Errorf("filtered to followup, its card isn't current: %s", renderNode(card))
	}
}

// A Goal flagged only for an overdue Action Item counts in the sidebar's
// Follow-ups count, as it does on the page.
func TestSidebarFollowUpsCountsOverdueActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)
	launch := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Launch in EU", "Expand the market."))
	client := signInClient(t, ts.URL, "sam@example.com")

	if count := followUpsNavCount(t, getPage(t, client, ts.URL+"/home")); count != "1" {
		t.Fatalf("before the Action Item is overdue, the nav's Follow-ups count is %q, want 1", count)
	}
	raiseFollowUpActionItem(t, h, ada, launch, sam, "Book the legal review.", -2)

	if count := followUpsNavCount(t, getPage(t, client, ts.URL+"/home")); count != "2" {
		t.Errorf("with an overdue Action Item, the nav's Follow-ups count is %q, want 2", count)
	}
}

// The page's scope narrows overdue Action Items like any other row: under
// Mine, a Goal the viewer doesn't Own isn't listed for one, however much the
// viewer owns the item; under Everyone it is.
func TestFollowUpsPageScopesOverdueActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	own := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Own work", "It matters."))
	kims := h.MarkTopLevel(ada, h.ActiveGoal(kim, "Kim's work", "It matters."))
	raiseFollowUpActionItem(t, h, ada, own, kim, "Chase the vendor.", -1)
	raiseFollowUpActionItem(t, h, ada, kims, sam, "Send Kim the numbers.", -3)
	client := signInClient(t, ts.URL, "sam@example.com")

	doc := getPage(t, client, ts.URL+"/follow-ups?org=mine")

	followUpChipIn(t, followUpRowIn(t, doc, own), "action-item-overdue")
	if hasFollowUpRowIn(doc, kims) {
		t.Errorf("under Mine, the page lists %q, which Sam doesn't Own", kims.Title)
	}
	if count := followUpsCardCount(t, followUpsCardIn(t, doc, "followup")); count != "1" {
		t.Errorf("under Mine, the followup card counts %s, want Sam's 1", count)
	}

	doc = getPage(t, client, ts.URL+"/follow-ups?org=all")

	assertFollowUpChip(t, followUpChipIn(t, followUpRowIn(t, doc, kims), "action-item-overdue"), "", "Action Item 3 days overdue")
	followUpChipIn(t, followUpRowIn(t, doc, own), "action-item-overdue")
}

// An Action Item due today, a closed one past its due date, one about the
// whole Report, and one past its due date on a Done Goal flag nothing.
func TestFollowUpsPageIgnoresActionItemsNotOverdueAboutAnActiveGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	goal := func(title string) domain.Goal {
		g := h.MarkTopLevel(ada, h.ActiveGoal(sam, title, "It matters."))
		h.Checkin(sam, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
		return g
	}
	dueToday := goal("Due today")
	raiseFollowUpActionItem(t, h, ada, dueToday, sam, "Do it today.", 0)
	closed := goal("Closed item")
	item := raiseFollowUpActionItem(t, h, ada, closed, sam, "Done already.", -4)
	if _, err := h.Service.CloseActionItem(t.Context(), sam.ID, item.ID, "Handled."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}
	whole := goal("In a Report")
	pub := h.PublishReport(ada, h.SaveReportDefinition(ada, domain.SaveReportDefinitionInput{
		Name: "QBR", Mode: domain.ReportModePicked, Picked: []int64{whole.ID},
	}))
	if _, err := h.Service.RaiseActionItem(t.Context(), ada.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Move the QBR.", OwnerID: sam.ID, DueDate: h.Clock.Now().AddDate(0, 0, -5),
	}); err != nil {
		t.Fatalf("RaiseActionItem about the whole Report: %v", err)
	}
	done := goal("Done goal")
	raiseFollowUpActionItem(t, h, ada, done, sam, "Write the retro.", -2)
	h.EndGoalInCheckin(sam, done.ID, domain.LifecycleDone)
	client := signInClient(t, ts.URL, "sam@example.com")

	doc := getPage(t, client, ts.URL+"/follow-ups?org=all")

	for _, g := range []domain.Goal{dueToday, closed, whole, done} {
		if hasFollowUpRowIn(doc, g) {
			t.Errorf("the page lists %q:\n%s", g.Title, renderNode(followUpRowIn(t, doc, g)))
		}
	}
	if !hasTestID(doc, "follow-ups-empty") {
		t.Errorf("with no overdue Action Item about an Active Goal, the page isn't empty")
	}
	if count := followUpsNavCount(t, doc); count != "" {
		t.Errorf("the nav counts %s Follow-ups", count)
	}
}

// Not Green is the Follow-ups page's first card: it holds every Active Goal whose
// latest Check-in is Red or Yellow, chipped like their Health badges, and the
// sidebar counts them. A Red or Yellow row takes no chip of its own, as its
// Health badge says so already, and its Fix opens the Goal. A Green Goal with
// nothing else wrong isn't listed.
func TestFollowUpsPageListsRedAndYellowGoalsUnderNotGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	later := h.Clock.Now().AddDate(0, 1, 0)
	red := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Stalled recovery", "It matters."))
	h.Checkin(sam, red.ID, domain.HealthRed, "Blocked.", "Escalate.", later)
	yellow := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Slipping launch", "It matters."))
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Slipping.", "Add a team.", later)
	green := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Steady work", "It matters."))
	h.Checkin(sam, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	client := signInClient(t, ts.URL, "sam@example.com")

	doc := getPage(t, client, ts.URL+"/follow-ups?org=all")

	if cards := followUpsWithTestIDPrefix(doc, "follow-ups-group-"); len(cards) == 0 || attrOf(cards[0], "data-testid") != "follow-ups-group-health" {
		t.Errorf("the Not Green card isn't the first card:\n%s", renderNode(doc))
	}
	card := followUpsCardIn(t, doc, "health")
	if !strings.Contains(textOf(card), "Not Green") || followUpsCardCount(t, card) != "2" {
		t.Errorf("the Not Green card doesn't count 2 Goals: %s", renderNode(card))
	}
	for kind, want := range map[string]struct{ class, text string }{
		"red":    {"badge r", "Red · 1"},
		"yellow": {"badge y", "Yellow · 1"},
	} {
		if chip := followUpChipIn(t, card, kind); !slices.Equal(followUpClassesOf(chip), strings.Fields(want.class)) || textOf(chip) != want.text {
			t.Errorf("the Not Green card's %s chip isn't %s saying %s: %s", kind, want.class, want.text, renderNode(chip))
		}
	}
	for _, g := range []domain.Goal{red, yellow} {
		row := followUpRowIn(t, doc, g)
		if kinds := followUpKindsIn(row); len(kinds) > 0 {
			t.Errorf("%q's row chips %q:\n%s", g.Title, kinds, renderNode(row))
		}
		if label, href, _ := followUpFixIn(t, row); label != "Open Goal" || href != "/goals/"+strconv.FormatInt(g.ID, 10) {
			t.Errorf("%q's Fix = %q %q, want Open Goal", g.Title, label, href)
		}
	}
	if hasFollowUpRowIn(doc, green) {
		t.Errorf("%q, Green with nothing wrong, is listed:\n%s", green.Title, renderNode(followUpRowIn(t, doc, green)))
	}

	filtered := getPage(t, client, ts.URL+"/follow-ups?group=health&org=all")
	if rows := followUpRowsIn(filtered); len(rows) != 2 || !hasFollowUpRowIn(filtered, red) || !hasFollowUpRowIn(filtered, yellow) {
		t.Errorf("Not Green lists %d rows, want only %q and %q:\n%s", len(rows), red.Title, yellow.Title, renderNode(filtered))
	}

	if count := followUpsNavCount(t, getPage(t, client, ts.URL+"/home")); count != "2" {
		t.Errorf("the nav's Follow-ups count %q doesn't count the Red and Yellow Goals", count)
	}
}

// A Not Green row reads its Goal's Run off Green beside its Health ("Red · 2
// wks"), on the whole page and in the Not Green group; a Green Goal listed for
// another signal, here Check-in overdue, shows none (CONTEXT.md: Run off
// Green).
func TestFollowUpsNotGreenRowsShowTheRunOffGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	red := h.ActiveGoal(ada, "Launch in EU", "Expand the market.")
	checkinOverdue := h.ActiveGoal(ada, "Cut churn", "Keep customers.")
	// Red on Mondays 5 and 12 January; the Green Check-in on the 5th is
	// Check-in overdue by Wednesday the 14th, when the page is read.
	h.Clock.Set(time.Date(2026, time.January, 5, 9, 0, 0, 0, time.UTC))
	h.Checkin(ada, checkinOverdue.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Checkin(ada, red.ID, domain.HealthRed, "Blocked.", "Escalate.", h.Clock.Now().AddDate(0, 1, 0))
	h.Clock.Set(time.Date(2026, time.January, 12, 9, 0, 0, 0, time.UTC))
	// A month out from a week later moves the back-to-Green date later.
	h.CheckinMovingPath(ada, red.ID, domain.HealthRed, "Still blocked.", "Escalate.", h.Clock.Now().AddDate(0, 1, 0), "Escalation takes longer.")
	h.Clock.Set(time.Date(2026, time.January, 14, 9, 0, 0, 0, time.UTC))
	client := signInClient(t, ts.URL, "ada@example.com")

	doc := getPage(t, client, ts.URL+"/follow-ups?org=all")
	for name, page := range map[string]*html.Node{
		"the Follow-ups page": doc,
		"Not Green":           getPage(t, client, ts.URL+"/follow-ups?group=health&org=all"),
	} {
		row := followUpRowIn(t, page, red)
		runs := findAllByTestID(row, "follow-up-run-off-green")
		if got := textsOf(runs); !slices.Equal(got, []string{"· 2 wks"}) {
			t.Errorf("on %s, %q's row reads its Run off Green %q, want \"· 2 wks\":\n%s", name, red.Title, got, renderNode(row))
			continue
		}
		if health := findByTestID(t, row, "follow-up-health"); runs[0].Parent != health.Parent || !precedes(health, runs[0]) {
			t.Errorf("on %s, %q's Run off Green isn't right after its Health badge:\n%s", name, red.Title, renderNode(row))
		}
	}
	if row := followUpRowIn(t, doc, checkinOverdue); hasTestID(row, "follow-up-run-off-green") {
		t.Errorf("%q, Green and Check-in overdue, shows a Run off Green:\n%s", checkinOverdue.Title, renderNode(row))
	}
}

// A Proposed Goal under Placement and dates is listed, but after every Active
// Goal: nothing has gone wrong with it yet. Lacking a Health doesn't rank it
// above a Green Goal that has gone Check-in overdue.
func TestFollowUpsPageListsGoalsThatArentActiveLast(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	checkinOverdue := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Zero-downtime deploys", "It matters."))
	h.Checkin(sam, checkinOverdue.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(10 * day)
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	proposed := h.CreateGoal(sam, "Article backlog", "It matters.")
	if _, err := h.Service.MarkGoalDated(t.Context(), proposed.Owner.ID, proposed.ID, june.AddDate(0, 1, 0)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	h.RequestLink(sam, proposed, launch, "")

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups?org=all")

	overdueRow, proposedRow := followUpRowIn(t, doc, checkinOverdue), followUpRowIn(t, doc, proposed)
	followUpChipIn(t, overdueRow, "checkin-overdue")
	followUpChipIn(t, proposedRow, "schedule-conflicts")
	if !precedes(overdueRow, proposedRow) {
		t.Errorf("%q is listed before %q", proposed.Title, checkinOverdue.Title)
	}
}

// A Goal with an open Ask for help carries an "Asks for help" chip in Not
// Green naming whom it asks, or saying no one is above its Owner; the Not
// Green card counts the asks. A Yellow Goal that doesn't ask has no such
// chip, and an asking Goal is still counted once.
func TestFollowUpsPageMarksAsksForHelpUnderNotGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	mia := h.SignInNamed("mia@example.com", "Mia Chen")
	lee := h.SignIn("lee@example.com")
	h.SetManager(sam, mia)
	later := h.Clock.Now().AddDate(0, 1, 0)
	red := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Stalled recovery", "It matters."))
	h.AskForHelp(sam, red.ID, domain.HealthRed, "Blocked.", "Need budget.", later)
	alone := h.MarkTopLevel(ada, h.ActiveGoal(lee, "Lonely launch", "It matters."))
	h.AskForHelp(lee, alone.ID, domain.HealthYellow, "Slipping.", "Need a reviewer.", later)
	yellow := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Slipping launch", "It matters."))
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Slipping.", "Add a team.", later)
	client := signInClient(t, ts.URL, "sam@example.com")

	doc := getPage(t, client, ts.URL+"/follow-ups?org=all")

	// asks is g's one Asks for help chip, an outlined chip whose button
	// reads tip and opens what an ask means.
	asks := func(g domain.Goal, tip string) *html.Node {
		t.Helper()
		row := followUpRowIn(t, doc, g)
		if n := len(followUpChipsIn(row, "asks-for-help")); n != 1 {
			t.Errorf("%q's row carries %d Asks for help chips, want 1:\n%s", g.Title, n, renderNode(row))
		}
		chip := followUpChipIn(t, row, "asks-for-help")
		button := followUpTipIn(t, chip)
		if !slices.Equal(followUpClassesOf(chip), []string{"rk-tag"}) || textOf(button) != tip || attrOf(button, "type") != "button" ||
			attrOf(button, "popovertarget") != "follow-up-means-asks-for-help" || attrOf(button, "aria-describedby") != "follow-up-means-asks-for-help" {
			t.Errorf("%q's Asks for help chip isn't an outlined chip with a button reading %q: %s", g.Title, tip, renderNode(chip))
		}
		return chip
	}
	chip := asks(red, "Asks for help")
	person := findAll(chip, func(n *html.Node) bool { return n.Data == "button" && attrOf(n, "title") == mia.Email })
	emails := findAll(chip, func(n *html.Node) bool { return hasAttr(n, "hidden") && runningText(n) == mia.Email })
	if textOf(chip) != "Asks for help of Mia Chen" || len(person) != 1 || textOf(person[0]) != "Mia Chen" || attrOf(person[0], "aria-expanded") != "false" || len(emails) != 1 {
		t.Errorf("%q's chip doesn't ask Mia Chen, her email beside her: %s", red.Title, renderNode(chip))
	}
	chip = asks(alone, "Asks for help · no one above the Owner")
	if textOf(chip) != "Asks for help · no one above the Owner" || len(findAll(chip, func(n *html.Node) bool { return n != chip })) != 1 {
		t.Errorf("%q's chip holds more than its button: %s", alone.Title, renderNode(chip))
	}
	if row := followUpRowIn(t, doc, yellow); len(followUpChipsIn(row, "asks-for-help")) != 0 {
		t.Errorf("%q, which doesn't ask, carries an Asks for help chip:\n%s", yellow.Title, renderNode(row))
	}
	card := followUpsCardIn(t, doc, "health")
	if chip := followUpChipIn(t, card, "asks-for-help"); !slices.Equal(followUpClassesOf(chip), []string{"rk-tag"}) || textOf(chip) != "Asks for help · 2" {
		t.Errorf("the Not Green card's asks chip isn't an rk-tag saying Asks for help · 2: %s", renderNode(chip))
	}
	if count := followUpsCardCount(t, card); count != "3" {
		t.Errorf("the Not Green card counts %s, want 3 Goals", count)
	}

	filtered := getPage(t, client, ts.URL+"/follow-ups?group=health&org=all")
	if rows := followUpRowsIn(filtered); len(rows) != 3 || !hasFollowUpRowIn(filtered, red) || !hasFollowUpRowIn(filtered, alone) || !hasFollowUpRowIn(filtered, yellow) {
		t.Errorf("Not Green lists %d rows, want %q, %q and %q:\n%s", len(rows), red.Title, alone.Title, yellow.Title, renderNode(filtered))
	}

	// The sidebar counts sam's default scope, their own two Goals, each once.
	if count := followUpsNavCount(t, getPage(t, client, ts.URL+"/home")); count != "2" {
		t.Errorf("the nav's Follow-ups count %q doesn't count each of sam's Goals once", count)
	}
}

// An ask is cleared from Follow-ups when its Owner checks in again without it,
// though the Goal stays Not Green.
func TestFollowUpsPageDropsAnAskTheOwnerUnmarks(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	h.SetManager(sam, h.SignIn("mia@example.com"))
	later := h.Clock.Now().AddDate(0, 1, 0)
	goal := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Stalled recovery", "It matters."))
	h.AskForHelp(sam, goal.ID, domain.HealthRed, "Blocked.", "Need budget.", later)
	client := signInClient(t, ts.URL, "sam@example.com")
	if row := followUpRowIn(t, getPage(t, client, ts.URL+"/follow-ups?org=all"), goal); len(followUpChipsIn(row, "asks-for-help")) == 0 {
		t.Fatalf("the asking Goal's row has no Asks for help chip:\n%s", renderNode(row))
	}

	h.Checkin(sam, goal.ID, domain.HealthYellow, "Better.", "Keep going.", later)

	if row := followUpRowIn(t, getPage(t, client, ts.URL+"/follow-ups?org=all"), goal); len(followUpChipsIn(row, "asks-for-help")) != 0 {
		t.Errorf("the row still carries the Asks for help chip after the Owner unmarked it:\n%s", renderNode(row))
	}
}

// A Goal whose latest Check-in is Green while a Milestone is overdue is listed
// under Waiting on a Check-in with a Milestone overdue chip in Check-in
// overdue's style, counted on its card and in the sidebar. Its Fix is Check in
// for its Owner, and Nudge for anyone else, as its Owner answers it in a
// Check-in (CONTEXT.md: Nudge).
func TestFollowUpsPageListsGreenGoalsPastAMilestone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	h.SignIn("kim@example.com")
	g, _ := h.GreenGoalWithOverdueMilestone(sam, "Ship search")
	g = h.MarkTopLevel(ada, g)
	goal := "/goals/" + strconv.FormatInt(g.ID, 10)

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	row := followUpRowIn(t, doc, g)
	assertFollowUpChip(t, followUpChipIn(t, row, "milestone-overdue"), "badge st", "Milestone 1 day overdue")
	assertHealthChip(t, findByTestID(t, row, "follow-up-health"), "Green")
	card := followUpsCardIn(t, doc, "owner")
	if followUpsCardCount(t, card) != "1" || !slices.Contains(followUpsCardChips(card), "Milestone overdue · 1") {
		t.Errorf("Waiting on a Check-in card doesn't count the Goal with a Milestone overdue · 1 chip: %s", renderNode(card))
	}
	if chip := followUpChipIn(t, card, "milestone-overdue"); !slices.Equal(followUpClassesOf(chip), []string{"badge", "st"}) {
		t.Errorf("the card's Milestone overdue chip isn't drawn like the table's: %s", renderNode(chip))
	}
	if count := followUpsNavCount(t, doc); count != "1" {
		t.Errorf("the sidebar's Follow-ups counts %q, want the Goal's 1", count)
	}

	for _, c := range []struct {
		viewer, label, href string
		primary             bool
	}{
		{"sam@example.com", "Check in", goal + "/checkin", true},
		{"kim@example.com", "Nudge", goal + "/nudge", false},
	} {
		doc := getPage(t, signInClient(t, ts.URL, c.viewer), ts.URL+"/follow-ups?org=all")
		label, href, primary := followUpFixIn(t, followUpRowIn(t, doc, g))
		if label != c.label || href != c.href || primary != c.primary {
			t.Errorf("%s sees Fix %q → %s (primary %v), want %q → %s (primary %v)", c.viewer, label, href, primary, c.label, c.href, c.primary)
		}
	}
}

// A Not Green row whose back-to-Green date slipped within its Run off Green
// tags the slip as a Milestone's slipped date reads: the current date, the one
// it last slipped from struck through, and the count once it slipped more than
// once. A row whose date never slipped stays quiet, and the tag isn't a signal:
// the Not Green card counts the Goals as before.
func TestFollowUpsNotGreenRowsTagASlippedBackToGreenDate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	slipped := h.ActiveGoal(ada, "Launch in EU", "Expand the market.")
	steady := h.ActiveGoal(ada, "Cut churn", "Keep customers.")
	h.Clock.Set(time.Date(2026, time.January, 5, 9, 0, 0, 0, time.UTC))
	h.Checkin(ada, slipped.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, time.February, 6, 0, 0, 0, 0, time.UTC))
	h.Checkin(ada, steady.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, time.February, 6, 0, 0, 0, 0, time.UTC))
	h.Clock.Set(time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC))
	h.CheckinMovingPath(ada, slipped.ID, domain.HealthRed, "Still blocked.", "Escalate.", time.Date(2026, time.February, 13, 0, 0, 0, 0, time.UTC), "Escalation takes longer.")
	h.Clock.Set(time.Date(2026, time.January, 7, 9, 0, 0, 0, time.UTC))
	h.CheckinMovingPath(ada, slipped.ID, domain.HealthRed, "Still blocked.", "Escalate.", time.Date(2026, time.March, 3, 0, 0, 0, 0, time.UTC), "The approver is away.")
	client := signInClient(t, ts.URL, "ada@example.com")

	doc := getPage(t, client, ts.URL+"/follow-ups?org=all")

	row := followUpRowIn(t, doc, slipped)
	tag := findByTestID(t, row, "follow-up-back-to-green")
	if want := "Back to Green Mar 3 Feb 13 (2)"; textOf(tag) != want {
		t.Errorf("%q's row tags its back-to-Green date %q, want %q:\n%s", slipped.Title, textOf(tag), want, renderNode(row))
	}
	if got := struckThrough(tag); !slices.Equal(got, []string{"Feb 13"}) || textOf(findByTestID(t, tag, "follow-up-back-to-green-slips")) != "(2)" {
		t.Errorf("%q's tag strikes %q through, want Feb 13 and a count of 2 slips:\n%s", slipped.Title, got, renderNode(tag))
	}
	if !within(tag, findByTestID(t, row, "follow-up-goal")) || !flatFrameHasClass(tag.Parent, "rk-chips") {
		t.Errorf("%q's tag isn't among its chips:\n%s", slipped.Title, renderNode(row))
	}
	if row := followUpRowIn(t, doc, steady); hasTestID(row, "follow-up-back-to-green") {
		t.Errorf("%q, whose back-to-Green date never slipped, tags one:\n%s", steady.Title, renderNode(row))
	}
	if count := followUpsCardCount(t, followUpsCardIn(t, doc, "health")); count != "2" {
		t.Errorf("the Not Green card counts %s, want its 2 Goals", count)
	}
}

// Each signal chip in the Follow-ups table names its signal with a button that,
// with no script, opens a popover saying what the signal means, so a tap
// explains it where it's shown. Chips holding a link or a person name the
// signal with the button and keep the link outside it. Every signal but Red
// and Yellow, which the Health badge says, takes a chip, and each is checked.
func TestFollowUpsSignalChipsOpenWhatTheyMean(t *testing.T) {
	t.Parallel()

	checked := map[string]bool{}
	check := func(t *testing.T, doc *html.Node, g domain.Goal, kinds ...string) {
		t.Helper()
		row := followUpRowIn(t, doc, g)
		for _, kind := range kinds {
			checked[kind] = true
			button := followUpTipIn(t, followUpChipIn(t, row, kind))
			if attrOf(button, "type") != "button" {
				t.Errorf("%q's %s button isn't type=button: %s", g.Title, kind, renderNode(button))
			}
			if textOf(button) == "" {
				t.Errorf("%q's %s button has no name: %s", g.Title, kind, renderNode(button))
			}
			if links := findAll(button, func(n *html.Node) bool { return n.Data == "a" }); len(links) > 0 {
				t.Errorf("%q's %s button holds a link: %s", g.Title, kind, renderNode(button))
			}
			target := attrOf(button, "popovertarget")
			if target == "" {
				t.Errorf("%q's %s button opens no popover: %s", g.Title, kind, renderNode(button))
				continue
			}
			def := followUpSignalDefinition(t, kind)
			pop := followUpPopoverIn(t, doc, target)
			lead := pop.FirstChild
			if got, want := textOf(pop), def.Name+" "+def.Means; got != want || lead == nil || lead.Data != "strong" || textOf(lead) != def.Name {
				t.Errorf("%q's %s popover = %s, want %s in bold then %q", g.Title, kind, renderNode(pop), def.Name, def.Means)
			}
		}
	}

	t.Run("by who acts", func(t *testing.T) {
		h := testsupport.New(t, "ada@example.com")
		ts := newServer(t, h)
		ada := h.SignIn("ada@example.com")
		sam := h.SignIn("sam@example.com")
		kim := h.SignIn("kim@example.com")
		mia := h.SignInNamed("mia@example.com", "Mia Chen")
		h.SetManager(sam, mia)
		june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
		silent := h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue, Unlinked and an overdue Action Item
		h.Clock.Advance(10 * day)
		h.Checkin(sam, launch.ID, domain.HealthGreen, "On track.", "", time.Time{})
		raiseFollowUpActionItem(t, h, ada, silent, sam, "Break the silence.", -1)
		orphaned := h.ActiveGoal(kim, "Orphaned work", "It matters.") // Ownerless and Unlinked
		if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
			t.Fatalf("MarkDeparted: %v", err)
		}
		late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 1, 0)) // Schedule conflict
		h.RequestLink(sam, late, launch, "")
		stalled := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Stalled recovery", "It matters.")) // Asks for help
		h.AskForHelp(sam, stalled.ID, domain.HealthRed, "Blocked.", "Need budget.", june)

		doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups?org=all")

		check(t, doc, silent, "checkin-overdue", "action-item-overdue", "unlinked")
		check(t, doc, orphaned, "ownerless", "unlinked")
		check(t, doc, late, "schedule-conflicts")
		check(t, doc, stalled, "asks-for-help")
	})

	t.Run("by what is late or halted", func(t *testing.T) {
		h := testsupport.New(t)
		ts := newServer(t, h)
		sam := h.SignIn("sam@example.com")
		passed, _ := h.GreenGoalWithOverdueMilestone(sam, "Ship search") // Milestone overdue
		stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")  // Path to Green overdue
		h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", h.Clock.Now().AddDate(0, 0, 1))
		paused := h.ActiveGoal(sam, "Paused outcome", "It mattered.")
		under := h.ActiveChildOf(sam, paused, "Under paused", "Feeds paused.") // Parent On Hold
		if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
			GoalID: paused.ID, AuthorID: sam.ID, Status: "Stopping.", Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Budget freeze.",
		}); err != nil {
			t.Fatalf("SubmitCheckin On Hold: %v", err)
		}
		h.Clock.Advance(3 * day)

		doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

		check(t, doc, passed, "milestone-overdue")
		check(t, doc, stalled, "path-overdue")
		check(t, doc, under, "halted-parents")
	})

	for _, d := range web.FollowUpSignalDefinitions {
		if d.Kind != "red" && d.Kind != "yellow" && !checked[d.Kind] {
			t.Errorf("no %s chip is checked for what it means", d.Kind)
		}
	}
}

// followUpSignalDefinition returns the Follow-ups page's definition of the
// signal of kind.
func followUpSignalDefinition(t *testing.T, kind string) web.FollowUpSignalDefinition {
	t.Helper()
	for _, d := range web.FollowUpSignalDefinitions {
		if d.Kind == kind {
			return d
		}
	}
	t.Fatalf("no signal of kind %q", kind)
	return web.FollowUpSignalDefinition{}
}

// A group card stays a link holding no button, though the table's chips for
// the same signals open what they mean: a button can't sit inside a link.
func TestFollowUpsGroupCardsHoldNoButton(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)
	h.ActiveGoal(kim, "Orphaned work", "It matters.") // Ownerless and Unlinked
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	doc := getPage(t, signInClient(t, ts.URL, "ada@example.com"), ts.URL+"/follow-ups?org=all")

	for _, key := range []string{"owner", "plan", "admin"} {
		card := followUpsCardIn(t, doc, key)
		followUpChipIn(t, card, map[string]string{"owner": "checkin-overdue", "plan": "unlinked", "admin": "ownerless"}[key])
		if buttons := findAll(card, func(n *html.Node) bool { return n.Data == "button" }); card.Data != "a" || len(buttons) > 0 {
			t.Errorf("the %s card is no link or holds a button:\n%s", key, renderNode(card))
		}
	}
}

// The Follow-ups page sits in the flat frame with no card: its group summaries
// are plain blocks and its flagged Goals a full-bleed list, so no element on
// it, a muted empty group's included, takes a card class.
func TestFollowUpsPageHoldsNoCard(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	followUpsCardIn(t, doc, "owner")
	followUpsCardIn(t, doc, "admin")
	if rows := followUpRowsIn(doc); len(rows) != 1 {
		t.Fatalf("the list has %d rows, want 1, so this test checks too little", len(rows))
	}
	for _, n := range findAll(doc, func(n *html.Node) bool { return hasAttr(n, "class") }) {
		for _, class := range followUpClassesOf(n) {
			if strings.Contains(class, "card") {
				t.Errorf("the page has an element with the card class %q: class=%q", class, attrOf(n, "class"))
			}
		}
	}
}
