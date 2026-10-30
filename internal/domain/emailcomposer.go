package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/email"
)

// AppLinks builds every link into the web app that an email carries, from the
// one base URL people reach it at. Construct it with NewAppLinks; the zero
// value builds links relative to the web app. Each method is one link, so a
// web test can check every one reaches a registered route.
type AppLinks struct {
	base string
}

// NewAppLinks returns the AppLinks for the web app at baseURL (e.g.
// http://localhost:8080), any trailing slash trimmed. An empty baseURL leaves
// links relative.
func NewAppLinks(baseURL string) AppLinks {
	return AppLinks{base: strings.TrimRight(baseURL, "/")}
}

// Goal links to a Goal's page.
func (l AppLinks) Goal(goalID int64) string {
	return fmt.Sprintf("%s/goals/%d", l.base, goalID)
}

// GoalComments links to the Comments on a Goal's page.
func (l AppLinks) GoalComments(goalID int64) string {
	return l.Goal(goalID) + "#comments"
}

// Checkin links to a Goal's Check-in page, beside which the latest Check-in's
// answers show; openDates opens its dates, for a Schedule conflict.
func (l AppLinks) Checkin(goalID int64, openDates bool) string {
	link := l.Goal(goalID) + "/checkin"
	if openDates {
		link += "?open=dates"
	}
	return link
}

// PublicationGoal links to a Goal's block in the publication pubID of the
// Report defID.
func (l AppLinks) PublicationGoal(defID, pubID, goalID int64) string {
	return fmt.Sprintf("%s#goal-%d", l.publication(defID, pubID), goalID)
}

// ActionItem links to an Action Item among its publication's Action Items,
// whether or not it is about a Goal.
func (l AppLinks) ActionItem(item ActionItem) string {
	return l.publication(item.DefinitionID, item.PublicationID) + "#action-items"
}

// PendingLinks links to the link requests waiting on the signed-in person:
// Home, whose Needs you lists them under Requests.
func (l AppLinks) PendingLinks() string {
	return l.base + "/home"
}

// SetPassword links to the page that sets a password with the set-password
// link's token.
func (l AppLinks) SetPassword(token string) string {
	return l.base + "/set-password?token=" + token
}

// publication is the page of the publication pubID of the Report defID.
func (l AppLinks) publication(defID, pubID int64) string {
	return fmt.Sprintf("%s/reports/%d/publications/%d", l.base, defID, pubID)
}

// EmailComposer builds every email the app sends — who it goes to, its subject
// and its body — with its links from AppLinks. The Service's commands and the
// weekly emails (Service.QueueWeeklyEmails) both compose through it.
type EmailComposer struct {
	links AppLinks
	// loc is the company's timezone, the calendar a publication's date is
	// read in.
	loc *time.Location
}

// ActionItem is the email of item to its Owner: intro, saying who gave it to
// them, then what it is, its Report (named report) and Goal, its due date, and
// a link to it.
func (c EmailComposer) ActionItem(item ActionItem, report, intro string) email.Message {
	var b strings.Builder
	fmt.Fprintf(&b, "%s in the Report %s:\n\n%s\n\n", intro, report, item.Text)
	if item.GoalTitle != "" {
		fmt.Fprintf(&b, "Goal: %s\n", item.GoalTitle)
	}
	fmt.Fprintf(&b, "Due: %s\n\n", item.DueDate.Format(dateFormat))
	fmt.Fprintf(&b, "%s\n", c.links.ActionItem(item))
	return email.Message{
		To:        item.Owner.Email,
		AccountID: item.Owner.ID,
		Subject:   "Action Item: " + strings.Join(strings.Fields(item.Text), " "),
		Body:      b.String(),
	}
}

// CommentAlerts are the emails of the new Comment cm on goal to goal's Owner
// and to others (everyone in its thread, for a reply), one each, never to its
// author or anyone who has left the org. pub is the published Report cm was
// made on, or nil for a Comment on the Goal itself.
func (c EmailComposer) CommentAlerts(cm Comment, goal Goal, pub *Publication, others []Account) []email.Message {
	kind, verb := "Comment", "commented on"
	if cm.ThreadID != 0 {
		kind, verb = "Reply", "replied about"
	}
	var subject, body string
	if pub == nil {
		subject = fmt.Sprintf("%s on %s", kind, goal.Title)
		body = fmt.Sprintf("%s %s the Goal %q:\n\n%s\n\nRead and reply at %s\n",
			cm.Author.LongLabel(), verb, goal.Title, cm.Body, c.links.GoalComments(goal.ID))
	} else {
		report := pub.Name
		subject = fmt.Sprintf("%s on %s in %s", kind, goal.Title, report)
		body = fmt.Sprintf("%s %s the Goal %q in the Report %s, published %s:\n\n%s\n\nRead and reply at %s\n",
			cm.Author.LongLabel(), verb, goal.Title, report,
			orgDate(pub.PublishedAt, c.loc).Format(dateFormat), cm.Body, c.links.PublicationGoal(pub.DefinitionID, pub.ID, goal.ID))
	}

	var out []email.Message
	sent := map[int64]bool{cm.Author.ID: true}
	for _, a := range append([]Account{goal.Owner}, others...) {
		if sent[a.ID] || a.Departed {
			continue
		}
		sent[a.ID] = true
		out = append(out, email.Message{To: a.Email, AccountID: a.ID, Subject: subject, Body: body})
	}
	return out
}

// Nudges are a Nudge's emails, one to each of to: who asked (sender), the
// Goal's line in the weekly reminder's words (a Delegate's naming the Owner
// they check in for) with the first of Check-in overdue, Path to Green overdue
// and Milestone overdue that holds, or else Schedule conflict, a line for each
// of the Goal's Schedule conflicts in the Goal page banner's words, and the
// link to check in, opening its dates when there is a conflict. With no hover
// in an email, people read Name (email) at their first mention.
func (c EmailComposer) Nudges(sender Account, goal Goal, f Freshness, conflicts []ScheduleConflict, to []Account) []email.Message {
	out := make([]email.Message, 0, len(to))
	for _, r := range to {
		out = append(out, email.Message{
			To:        r.Email,
			AccountID: r.ID,
			Subject:   "Check-in requested: " + goal.Title,
			Body:      c.nudgeBody(sender, goal, f, conflicts, r.ID != goal.Owner.ID),
		})
	}
	return out
}

func (c EmailComposer) nudgeBody(sender Account, goal Goal, f Freshness, conflicts []ScheduleConflict, asDelegate bool) string {
	var people Mentions
	var b strings.Builder
	fmt.Fprintf(&b, "%s asked for a Check-in on this Goal.\n\n", people.Of(sender))
	title := goal.Title
	if asDelegate {
		title += ", as Delegate for " + people.Of(goal.Owner)
	}
	switch {
	case f.CheckinOverdue:
		fmt.Fprintf(&b, "%s: Check-in overdue, %s\n", title, f.LastCheckinAge())
	case f.PathToGreenOverdue:
		fmt.Fprintf(&b, "%s: Path to Green overdue since %s\n", title, f.PathTargetDate.Format(dateFormat))
	case f.MilestoneOverdue:
		fmt.Fprintf(&b, "%s: Milestone %q overdue since %s\n", title, f.OverdueMilestone, f.OverdueMilestoneDate.Format(dateFormat))
	default:
		fmt.Fprintf(&b, "%s: Schedule conflict\n", title)
	}
	for _, sc := range conflicts {
		fmt.Fprintf(&b, "Schedule conflict: %s (due %s) contributes to %s (due %s)\n",
			sc.Child.Title, sc.Child.DeliveryDate.Format(dateFormat), sc.Parent.Title, sc.Parent.DeliveryDate.Format(dateFormat))
	}
	fmt.Fprintln(&b, c.links.Checkin(goal.ID, len(conflicts) > 0))
	return b.String()
}

// ReminderItem is one entry on a person's weekly reminder, To: a Goal whose
// Check-in is due, or an open Action Item they own when ActionItem is set.
// AsDelegate is set when they are reminded of the Goal as its Delegate rather
// than its Owner.
type ReminderItem struct {
	To         Account
	Goal       Goal
	Freshness  Freshness
	AsDelegate bool
	ActionItem *ActionItemListing
}

// Reminders are the weekly reminders of items: one email to each person the
// items are for, in the order they are first met, however many items they
// have, and none to anyone who has left the company.
func (c EmailComposer) Reminders(items []ReminderItem) []email.Message {
	var order []Account
	byEmail := map[string][]ReminderItem{}
	for _, it := range items {
		if it.To.Departed {
			continue
		}
		if _, ok := byEmail[it.To.Email]; !ok {
			order = append(order, it.To)
		}
		byEmail[it.To.Email] = append(byEmail[it.To.Email], it)
	}
	out := make([]email.Message, 0, len(order))
	for _, to := range order {
		mine := byEmail[to.Email]
		out = append(out, email.Message{
			To:        to.Email,
			AccountID: to.ID,
			Subject:   reminderSubject(mine),
			Body:      c.reminderBody(mine),
		})
	}
	return out
}

// reminderSubject titles a reminder by what it holds: Check-ins, Action Items,
// or both.
func reminderSubject(items []ReminderItem) string {
	var checkins, actionItems bool
	for _, it := range items {
		if it.ActionItem != nil {
			actionItems = true
		} else {
			checkins = true
		}
	}
	switch {
	case checkins && actionItems:
		return "Your Check-ins and Action Items this week"
	case actionItems:
		return "Your Action Items this week"
	default:
		return "Your Check-ins this week"
	}
}

// reminderBody lists the Goals needing a Check-in, then the open Action Items
// the person owns. With no hover in an email, the Owner a Delegate writes for
// reads Name (email) at their first mention and by Name after that
// (CONTEXT.md: Name).
func (c EmailComposer) reminderBody(items []ReminderItem) string {
	var b strings.Builder
	var people Mentions
	var checkins []ReminderItem
	var actionItems []*ActionItemListing
	for _, it := range items {
		if it.ActionItem != nil {
			actionItems = append(actionItems, it.ActionItem)
		} else {
			checkins = append(checkins, it)
		}
	}
	if len(checkins) > 0 {
		b.WriteString("These Goals need a Check-in:\n\n")
	}
	for _, it := range checkins {
		title := it.Goal.Title
		if it.AsDelegate {
			title += ", as Delegate for " + people.Of(it.Goal.Owner)
		}
		state := "Check-in due"
		if it.Freshness.CheckinOverdue {
			state = "Check-in overdue"
		}
		fmt.Fprintf(&b, "- %s: %s, %s\n  %s\n", title, state, it.Freshness.LastCheckinAge(), c.links.Checkin(it.Goal.ID, false))
	}
	if len(actionItems) > 0 {
		if len(checkins) > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Action Items you own:\n\n")
	}
	for _, l := range actionItems {
		where := l.ReportName
		if l.GoalTitle != "" {
			where += ", about " + l.GoalTitle
		}
		due := "due " + l.Item.DueDate.Format(dateFormat)
		if l.Overdue {
			due = "overdue since " + l.Item.DueDate.Format(dateFormat)
		}
		fmt.Fprintf(&b, "- %s (%s): %s\n  %s\n", l.Item.Text, where, due, c.links.ActionItem(l.Item))
	}
	return b.String()
}

// WeeklyDigests are the parent digests, one email for each of digests
// (Service.WeeklyDigests).
func (c EmailComposer) WeeklyDigests(digests []WeeklyDigest) []email.Message {
	out := make([]email.Message, 0, len(digests))
	for _, d := range digests {
		out = append(out, email.Message{
			To:        d.Recipient.Email,
			AccountID: d.Recipient.ID,
			Subject:   "Your weekly digest",
			Body:      c.weeklyDigestBody(d),
		})
	}
	return out
}

// weeklyDigestBody lists the link requests and problems awaiting the parent
// Owner, each child's Owner reading Name (email) at their first mention and by
// Name after that (CONTEXT.md: Name).
func (c EmailComposer) weeklyDigestBody(d WeeklyDigest) string {
	var b strings.Builder
	var people Mentions
	if len(d.PendingLinks) > 0 {
		fmt.Fprintf(&b, "Link requests waiting on you (%s):\n\n", c.links.PendingLinks())
		for _, l := range d.PendingLinks {
			fmt.Fprintf(&b, "- %s, owned by %s, asks to contribute to %s\n", l.Child.Title, people.Of(l.Child.Owner), l.Parent.Title)
		}
		b.WriteString("\n")
	}
	if len(d.Problems) > 0 {
		b.WriteString("Updates on Goals contributing to yours:\n\n")
		for _, p := range d.Problems {
			fmt.Fprintf(&b, "- %s, owned by %s, contributing to %s: %s\n  %s\n",
				p.Child.Title, people.Of(p.Child.Owner), p.Parent.Title, strings.Join(weeklyDigestReasons(p), "; "), c.links.Goal(p.Child.ID))
		}
	}
	return b.String()
}

// weeklyDigestReasons says what went wrong with a child, in the digest's
// order: its Health got worse, its Date Slips, its Check-in went overdue, it
// is Ownerless, and its halted parents.
func weeklyDigestReasons(p WeeklyDigestProblem) []string {
	var reasons []string
	if p.WorsenedTo != "" {
		reasons = append(reasons, "went "+p.WorsenedTo)
	}
	for _, sl := range p.DateSlips {
		what := "its delivery date"
		switch {
		case sl.BackToGreen:
			what = "its back-to-Green date"
		case sl.MilestoneID != 0:
			what = "a Milestone"
		}
		reasons = append(reasons, fmt.Sprintf("slipped %s from %s to %s (%s)",
			what, sl.OldDate.Format(dateFormat), sl.NewDate.Format(dateFormat), sl.Reason))
	}
	if p.WentCheckinOverdue {
		reasons = append(reasons, "Check-in went overdue, "+p.Freshness.LastCheckinAge())
	}
	if p.Ownerless {
		// The digest has already introduced the Owner on the child's line.
		reasons = append(reasons, "is Ownerless: "+p.Child.Owner.Label()+" has left the company")
	}
	for _, hp := range p.HaltedParents {
		reasons = append(reasons, fmt.Sprintf("contributes to %s, which is %s", hp.Parent.Title, hp.Parent.Lifecycle))
	}
	return reasons
}
