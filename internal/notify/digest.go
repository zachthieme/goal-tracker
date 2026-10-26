// Package notify sends the weekly emails: each Owner's and Delegate's reminder
// of their Check-ins due and the open Action Items they own, and each parent
// Owner's digest of problems among the Goals that contribute to theirs. It reads
// everything through the domain Service and queues its emails in the
// Service's email outbox, which sends them once queued and tries again while a
// send fails, so tests drive it with the fake clock and assert on the
// recording sender behind the outbox.
package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
)

// Notifier composes the weekly emails and queues them. Construct it with New.
type Notifier struct {
	svc *domain.Service
	// baseURL is where the web app is reached, so emails can link into it.
	baseURL string
}

// New returns a Notifier reading from svc and queueing in its email outbox.
// baseURL is the web app's address (e.g. http://localhost:8080) that links in
// emails point at.
func New(svc *domain.Service, baseURL string) *Notifier {
	return &Notifier{svc: svc, baseURL: strings.TrimRight(baseURL, "/")}
}

// WeeklyJob is the scheduled job the weekly emails' runs are recorded under
// (Service.RecordScheduledRun).
const WeeklyJob = "weekly emails"

// SendWeekly sends the week's emails for slot, the time they were due: it
// composes the reminders and the parent digests, then, in one transaction,
// records slot as the weekly emails' run and queues them all in the email
// outbox. A slot already recorded, by an earlier run or another process,
// queues nothing, so a slot's emails are queued once, and a run that fails
// partway queues none and leaves the slot to run again.
func (n *Notifier) SendWeekly(ctx context.Context, slot time.Time) error {
	reminders, err := n.reminders(ctx)
	if err != nil {
		return err
	}
	digests, err := n.digests(ctx)
	if err != nil {
		return err
	}
	return n.svc.WithinTx(ctx, func(tx *domain.Service) error {
		recorded, err := tx.RecordScheduledRun(ctx, WeeklyJob, slot)
		if err != nil || !recorded {
			return err
		}
		return tx.QueueEmails(ctx, append(reminders, digests...))
	})
}

// reminderItem is one entry on a person's reminder: a Goal whose Check-in is
// due, or an open Action Item they own when actionItem is set. asDelegate is
// set when the person is reminded of a Goal as its Delegate rather than its
// Owner.
type reminderItem struct {
	goal       domain.Goal
	freshness  domain.Freshness
	asDelegate bool
	actionItem *domain.ActionItemListing
}

// SendReminders queues an email to each Owner and Delegate the Active Goals whose Check-in
// is due — it would be overdue before next week's reminder — or that are
// Check-in overdue already, each linking to its Check-in page, which shows the
// last Check-in's answers as ghost text. It also lists every open Action Item
// the person owns, overdue first, so the owner of one is emailed even with no
// Check-in due. Someone with no Check-in due and no open Action Item gets no
// email, and nobody who has left the company is emailed. It queues them now,
// outside the schedule; SendWeekly sends them for a week's slot.
func (n *Notifier) SendReminders(ctx context.Context) error {
	msgs, err := n.reminders(ctx)
	if err != nil {
		return err
	}
	return n.svc.QueueEmails(ctx, msgs)
}

// reminders composes the reminders SendReminders describes. It reads the
// Freshness and the Delegates of every Active Goal at once, in the same few
// queries however many Goals there are.
func (n *Notifier) reminders(ctx context.Context) ([]email.Message, error) {
	goals, err := n.svc.ListGoals(ctx)
	if err != nil {
		return nil, err
	}
	var active []domain.Goal
	var ids []int64
	for _, g := range goals {
		if g.Lifecycle == domain.LifecycleActive {
			active = append(active, g)
			ids = append(ids, g.ID)
		}
	}
	freshness, err := n.svc.FreshnessByGoal(ctx, active)
	if err != nil {
		return nil, err
	}
	delegates, err := n.svc.ListDelegatesForGoals(ctx, ids)
	if err != nil {
		return nil, err
	}
	var out byRecipient[reminderItem]
	for _, g := range active {
		f := freshness[g.ID]
		if !f.DueBeforeNextReminder() {
			continue
		}
		out.add(g.Owner, reminderItem{goal: g, freshness: f})
		for _, d := range delegates[g.ID] {
			out.add(d, reminderItem{goal: g, freshness: f, asDelegate: true})
		}
	}
	items, err := n.svc.AllOpenActionItems(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		out.add(items[i].Item.Owner, reminderItem{actionItem: &items[i]})
	}
	return out.messages(reminderSubject, n.reminderBody), nil
}

// reminderSubject titles a reminder by what it holds: Check-ins, Action Items,
// or both.
func reminderSubject(items []reminderItem) string {
	var checkins, actionItems bool
	for _, it := range items {
		if it.actionItem != nil {
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

// dateFormat is how calendar dates read in emails.
const dateFormat = "2006-01-02"

// reminderBody lists the Goals needing a Check-in, then the open Action Items
// the person owns. With no hover in an email, the Owner a Delegate writes for
// reads Name (email) at their first mention and by Name after that
// (CONTEXT.md: Name).
func (n *Notifier) reminderBody(items []reminderItem) string {
	var b strings.Builder
	var people domain.Mentions
	var checkins []reminderItem
	var actionItems []*domain.ActionItemListing
	for _, it := range items {
		if it.actionItem != nil {
			actionItems = append(actionItems, it.actionItem)
		} else {
			checkins = append(checkins, it)
		}
	}
	if len(checkins) > 0 {
		b.WriteString("These Goals need a Check-in:\n\n")
	}
	for _, it := range checkins {
		title := it.goal.Title
		if it.asDelegate {
			title += ", as Delegate for " + people.Of(it.goal.Owner)
		}
		state := "Check-in due"
		if it.freshness.CheckinOverdue {
			state = "Check-in overdue"
		}
		fmt.Fprintf(&b, "- %s: %s, %s\n  %s\n", title, state, it.freshness.LastCheckinAge(), n.checkinURL(it.goal.ID))
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
		fmt.Fprintf(&b, "- %s (%s): %s\n  %s\n", l.Item.Text, where, due, n.actionItemURL(l.Item))
	}
	return b.String()
}

// actionItemURL links to an Action Item where it is listed: the page of the
// Goal it is about, or else the publication it was raised on.
func (n *Notifier) actionItemURL(item domain.ActionItem) string {
	if item.GoalID != 0 {
		return n.goalURL(item.GoalID) + "#action-items"
	}
	return fmt.Sprintf("%s/reports/%d/publications/%d#action-items", n.baseURL, item.DefinitionID, item.PublicationID)
}

// goalURL links to a Goal's page.
func (n *Notifier) goalURL(goalID int64) string {
	return fmt.Sprintf("%s/goals/%d", n.baseURL, goalID)
}

// checkinURL links to a Goal's Check-in page, which shows the latest
// Check-in's answers as ghost text.
func (n *Notifier) checkinURL(goalID int64) string {
	return fmt.Sprintf("%s/goals/%d/checkin", n.baseURL, goalID)
}

// SendDigests queues an email to each parent Owner, a digest of what needs
// their attention: the link requests waiting on them, and problems among the
// Goals that contribute to theirs. Someone with nothing to report gets no
// email. It queues them now, outside the schedule; SendWeekly sends them for a
// week's slot.
func (n *Notifier) SendDigests(ctx context.Context) error {
	msgs, err := n.digests(ctx)
	if err != nil {
		return err
	}
	return n.svc.QueueEmails(ctx, msgs)
}

// digests composes the digests SendDigests describes, one email for each the
// domain reads (Service.WeeklyDigests).
func (n *Notifier) digests(ctx context.Context) ([]email.Message, error) {
	digests, err := n.svc.WeeklyDigests(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]email.Message, 0, len(digests))
	for _, d := range digests {
		out = append(out, email.Message{
			To:        d.Recipient.Email,
			AccountID: d.Recipient.ID,
			Subject:   "Your weekly digest",
			Body:      n.digestBody(d),
		})
	}
	return out, nil
}

// digestBody lists the link requests and problems awaiting the parent Owner,
// each child's Owner reading Name (email) at their first mention and by Name
// after that (CONTEXT.md: Name).
func (n *Notifier) digestBody(d domain.WeeklyDigest) string {
	var b strings.Builder
	var people domain.Mentions
	if len(d.PendingLinks) > 0 {
		fmt.Fprintf(&b, "Link requests waiting on you (%s/links):\n\n", n.baseURL)
		for _, l := range d.PendingLinks {
			fmt.Fprintf(&b, "- %s, owned by %s, asks to contribute to %s\n", l.Child.Title, people.Of(l.Child.Owner), l.Parent.Title)
		}
		b.WriteString("\n")
	}
	if len(d.Problems) > 0 {
		b.WriteString("Updates on Goals contributing to yours:\n\n")
		for _, p := range d.Problems {
			fmt.Fprintf(&b, "- %s, owned by %s, contributing to %s: %s\n  %s\n",
				p.Child.Title, people.Of(p.Child.Owner), p.Parent.Title, strings.Join(problemReasons(p), "; "), n.goalURL(p.Child.ID))
		}
	}
	return b.String()
}

// problemReasons says what went wrong with a child, in the digest's order: its
// Health got worse, its Date Slips, its Check-in went overdue, it is
// Ownerless, and its halted parents.
func problemReasons(p domain.WeeklyDigestProblem) []string {
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

// byRecipient collects the items bound for each recipient, in the order
// recipients were first seen, so each person gets one email however many items
// they have. It never collects for someone who has left the company.
type byRecipient[T any] struct {
	order []domain.Account
	items map[string][]T
}

func (o *byRecipient[T]) add(to domain.Account, item T) {
	if to.Departed {
		return
	}
	if o.items == nil {
		o.items = map[string][]T{}
	}
	if _, ok := o.items[to.Email]; !ok {
		o.order = append(o.order, to)
	}
	o.items[to.Email] = append(o.items[to.Email], item)
}

// messages is one email to each recipient, whose subject and body render
// their items.
func (o *byRecipient[T]) messages(subjectOf, body func([]T) string) []email.Message {
	out := make([]email.Message, 0, len(o.order))
	for _, to := range o.order {
		items := o.items[to.Email]
		out = append(out, email.Message{
			To:        to.Email,
			AccountID: to.ID,
			Subject:   subjectOf(items),
			Body:      body(items),
		})
	}
	return out
}
