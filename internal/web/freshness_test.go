package web_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Freshness signals page is the Stale list: every Stale Goal with how long
// since its last update, and every Goal whose Path to Green is overdue.
// Fresh Goals aren't listed.
func TestFreshnessPageListsStaleGoalsAndOverduePaths(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * 24 * time.Hour)
	fresh := h.ActiveGoal(sam, "Fresh work", "It matters.")
	h.Checkin(sam, fresh.ID, domain.HealthGreen, "On track.", "", time.Time{})
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))

	client := signInClient(t, ts.URL, "sam@example.com")
	page := getBody(t, client, ts.URL+"/freshness")

	stale := pageElement(t, page, "ul", "stale-goals")
	if !strings.Contains(stale, navTo(silent.ID)) || !strings.Contains(stale, "10 days") {
		t.Errorf("Stale list does not show %q silent for 10 days; section:\n%s", silent.Title, stale)
	}
	for _, g := range []domain.Goal{fresh, stalled} {
		if strings.Contains(stale, navTo(g.ID)) {
			t.Errorf("Stale list includes %q, which checked in today", g.Title)
		}
	}

	overdue := pageElement(t, page, "ul", "overdue-paths")
	if !strings.Contains(overdue, navTo(stalled.ID)) || !strings.Contains(overdue, "2026-01-05") {
		t.Errorf("overdue Paths to Green do not show %q due back at Green by 2026-01-05; section:\n%s", stalled.Title, overdue)
	}
	if strings.Contains(overdue, navTo(silent.ID)) || strings.Contains(overdue, navTo(fresh.ID)) {
		t.Errorf("overdue Paths to Green list a Goal with no Path to Green; section:\n%s", overdue)
	}
}

// A Goal's page flags it when it is Stale or its Path to Green is overdue, and
// beside the Rolled-up Health says how many Active children are Stale without
// changing the color. A fresh Goal shows no freshness flag.
func TestGoalPageFlagsFreshness(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Org outcome", "It matters.")
	h.ActiveChildOf(sam, parent, "Silent child", "It matters.")
	h.Clock.Advance(10 * 24 * time.Hour)
	fresh := h.ActiveChildOf(sam, parent, "Fresh child", "It matters.")
	h.Checkin(sam, fresh.ID, domain.HealthGreen, "On track.", "", time.Time{})
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthYellow, "Slipping.", "Cut scope.", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))

	client := signInClient(t, ts.URL, "sam@example.com")
	pageOf := func(g domain.Goal) string {
		return getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, g.ID))
	}

	parentPage := pageOf(parent)
	if !strings.Contains(parentPage, `data-testid="goal-stale"`) {
		t.Errorf("Stale Goal's page does not flag it; body:\n%s", parentPage)
	}
	if !strings.Contains(parentPage, `data-testid="goal-rollup-health">Green`) {
		t.Errorf("Stale children changed the Rolled-up Health color; body:\n%s", parentPage)
	}
	if !strings.Contains(parentPage, `data-testid="goal-rollup-stale">1 of 2 Stale`) {
		t.Errorf("Rolled-up Health does not say 1 of 2 children are Stale; body:\n%s", parentPage)
	}

	stalledPage := pageOf(stalled)
	if !strings.Contains(stalledPage, `data-testid="goal-path-overdue"`) || !strings.Contains(stalledPage, "2026-01-05") {
		t.Errorf("page of a Goal past its Path to Green target does not flag it; body:\n%s", stalledPage)
	}

	freshPage := pageOf(fresh)
	if strings.Contains(freshPage, `data-testid="goal-stale"`) || strings.Contains(freshPage, `data-testid="goal-path-overdue"`) {
		t.Errorf("fresh Green Goal's page carries a freshness flag")
	}
}

// The Goal list marks a Stale Goal and a Goal whose Path to Green is overdue on
// its own row, as it marks an Ownerless one, so neither hides until someone
// opens the Goal. A fresh Goal's row carries no mark.
func TestGoalListMarksStaleAndOverdueGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * 24 * time.Hour)
	fresh := h.ActiveGoal(sam, "Fresh work", "It matters.")
	h.Checkin(sam, fresh.ID, domain.HealthGreen, "On track.", "", time.Time{})
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))

	client := signInClient(t, ts.URL, "sam@example.com")
	list := getBody(t, client, ts.URL+"/goals")
	row := func(g domain.Goal) string {
		at := strings.Index(list, navTo(g.ID))
		if at < 0 {
			t.Fatalf("Goal list has no row for %q; body:\n%s", g.Title, list)
		}
		start := strings.LastIndex(list[:at], "<tr")
		end := strings.Index(list[at:], "</tr>")
		return list[start : at+end]
	}

	if r := row(silent); !strings.Contains(r, `data-testid="stale"`) {
		t.Errorf("Stale Goal's row is not marked Stale; row:\n%s", r)
	}
	if r := row(stalled); !strings.Contains(r, `data-testid="path-overdue"`) || strings.Contains(r, `data-testid="stale"`) {
		t.Errorf("row of a Goal past its Path to Green target is not marked overdue (and only that); row:\n%s", r)
	}
	if r := row(fresh); strings.Contains(r, `data-testid="stale"`) || strings.Contains(r, `data-testid="path-overdue"`) {
		t.Errorf("fresh Goal's row carries a freshness mark; row:\n%s", r)
	}
}

// A Stale Goal's banner ends with a "Check in now" link to its Check-in form
// for those who may check in on it, its Owner and its Delegates, and for no
// one else, not even an Admin (#86).
func TestStaleBannerOffersCheckInNowToOwnerAndDelegates(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	h.SignIn("pat@example.com")
	h.SignIn("admin@example.com")
	goal := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.AddDelegate(sam, dee, goal.ID)
	h.Clock.Advance(10 * 24 * time.Hour)

	for _, viewer := range []struct {
		email string
		sees  bool
	}{
		{"sam@example.com", true},
		{"dee@example.com", true},
		{"pat@example.com", false},
		{"admin@example.com", false},
	} {
		page := getBody(t, signInClient(t, ts.URL, viewer.email), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
		banner := pageElement(t, page, "p", "goal-stale")
		has := strings.Contains(banner, `data-testid="stale-checkin-now"`)
		if has != viewer.sees {
			t.Errorf("%s sees Check in now in the Stale banner: %v, want %v; banner:\n%s", viewer.email, has, viewer.sees, banner)
			continue
		}
		if !has {
			continue
		}
		link := tagAround(t, banner, `data-testid="stale-checkin-now"`)
		if got, want := attr(link, "href"), fmt.Sprintf("/goals/%d/checkin", goal.ID); got != want {
			t.Errorf("%s's Check in now leads to %q, want %q", viewer.email, got, want)
		}
		if !strings.Contains(banner, ">Check in now</a>") {
			t.Errorf("%s's Stale banner link does not read Check in now; banner:\n%s", viewer.email, banner)
		}
	}
}

// Stale reads as a warning about freshness, never as a tag (#86). In every
// theme the Stale chip is a grey fill in a dashed edge, and a tag is an
// outline with no fill, so the two never share a fill. The chip's text reaches
// 4.5:1 on its fill and its edge 3:1 on the cards it sits on. The Stale and Path
// to Green overdue banners are a card surface in the same dashed edge, which
// reaches 3:1 on the canvas around them and the surface inside. A tag's text
// reaches 4.5:1 on the canvas and the cards it sits on.
func TestStaleIsADashedGreyChipAndTagsAreOutlined(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	light := tokens(tokenBlock(t, css, ":root"))
	chipLight := computed(t, css, light, ".badge", ".st")
	for p, want := range map[string]string{"background": "#E9EAEC", "color": "#3B4048"} {
		if chipLight[p] != want {
			t.Errorf("light: the Stale chip's %s is %s, want %s", p, chipLight[p], want)
		}
	}

	themes := darkThemes(t, css)
	themes["light"] = light
	for name, theme := range themes {
		canvas := resolve(t, theme, "var(--color-canvas)")
		cards := []string{resolve(t, theme, "var(--color-surface)"), resolve(t, theme, "var(--color-surface-hover)")}

		chip := computed(t, css, theme, ".badge", ".st")
		tag := computed(t, css, theme, ".tag")
		if chip["border-style"] != "dashed" || chip["border-width"] != "1px" {
			t.Errorf("%s: the Stale chip's edge is %s %s, want 1px dashed", name, chip["border-width"], chip["border-style"])
		}
		if bg := tag["background"]; bg != "none" && bg != "transparent" {
			t.Errorf("%s: a tag is filled with %s, want an outline with no fill", name, bg)
		}
		if tag["border-style"] != "solid" {
			t.Errorf("%s: a tag's edge is %q, want a solid outline unlike the Stale chip's dashes", name, tag["border-style"])
		}
		if ratio := contrast(t, chip["color"], chip["background"]); ratio < 4.5 {
			t.Errorf("%s: the Stale chip's text %s on %s is %.2f:1, under 4.5:1", name, chip["color"], chip["background"], ratio)
		}
		for _, bg := range cards {
			if ratio := contrast(t, chip["border-color"], bg); ratio < 3 {
				t.Errorf("%s: the Stale chip's edge %s on the card %s is %.2f:1, under 3:1", name, chip["border-color"], bg, ratio)
			}
		}
		for _, bg := range append([]string{canvas}, cards...) {
			if ratio := contrast(t, tag["color"], bg); ratio < 4.5 {
				t.Errorf("%s: a tag's text %s on %s is %.2f:1, under 4.5:1", name, tag["color"], bg, ratio)
			}
		}

		banner := computed(t, css, theme, ".alert", ".st", ".alert.st")
		if surface := resolve(t, theme, "var(--color-surface)"); banner["background"] != surface {
			t.Errorf("%s: the Stale banner's fill is %s, want the card surface %s", name, banner["background"], surface)
		}
		if banner["border-style"] != "dashed" || banner["border-color"] != chip["border-color"] {
			t.Errorf("%s: the Stale banner's edge is %s %s, want dashed %s like the chip's", name, banner["border-style"], banner["border-color"], chip["border-color"])
		}
		if ratio := contrast(t, banner["color"], banner["background"]); ratio < 4.5 {
			t.Errorf("%s: the Stale banner's text %s on %s is %.2f:1, under 4.5:1", name, banner["color"], banner["background"], ratio)
		}
		for _, bg := range []string{canvas, banner["background"]} {
			if ratio := contrast(t, banner["border-color"], bg); ratio < 3 {
				t.Errorf("%s: the Stale banner's edge %s on %s is %.2f:1, under 3:1", name, banner["border-color"], bg, ratio)
			}
		}
	}
}
