package seed

import "strings"

// The fake org's shape: three org-wide outcomes, and six teams (a Team
// Dimension, since teams are not built in — ADR 0002), each with two team Goals
// contributing to those outcomes and a handful of projects contributing to the
// team Goals. A few projects contribute to nothing and read as Unlinked. The
// words are fixed here; the random seed decides dates, Owners, and how each
// Goal's history plays out.

// teamDimension is the name of the Dimension the seed defines for teams.
const teamDimension = "Team"

// roleNames are the Names of the people the org reaches by a role address —
// the Admin, the leadership team, and the team leads — whose emails don't
// spell out a name.
var roleNames = map[string]string{
	"admin@example.com":         "Robin Ellis",
	"ceo@example.com":           "Dana Whitfield",
	"cto@example.com":           "Priya Raman",
	"cpo@example.com":           "Marcus Bell",
	"platform-lead@example.com": "Jonas Lindqvist",
	"payments-lead@example.com": "Amara Nwosu",
	"growth-lead@example.com":   "Elena Petrova",
	"mobile-lead@example.com":   "Kenji Watanabe",
	"data-lead@example.com":     "Sofia Marquez",
	"support-lead@example.com":  "Rahul Iyer",
}

// nameOf is the Name the org's sign-in would supply for addr (CONTEXT.md:
// Name): a role address's from roleNames, otherwise the name the address spells
// out, ada.okafor@example.com being Ada Okafor.
func nameOf(addr string) string {
	if name, ok := roleNames[addr]; ok {
		return name
	}
	local, _, _ := strings.Cut(addr, "@")
	parts := strings.Split(local, ".")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// entry is one Goal in the plan. metric, when set, is an import-format Metric
// without its target date ("Name | unit | up|down | baseline | target"); an
// ongoing entry is an Ongoing Goal and must carry one.
type entry struct {
	title        string
	whyItMatters string
	metric       string
	ongoing      bool
	// parents are the indexes of the outcomes (for a team Goal) or the team's
	// Goals (for a project) this Goal contributes to.
	parents []int
}

// outcomes are the org-wide outcomes the team Goals contribute to, owned by the
// leadership team. The Admin marks them Top-level Goals, so they are never
// Unlinked.
var outcomes = []struct {
	entry
	owner string
}{
	{entry{title: "Grow net revenue retention", whyItMatters: "Customers who stay should spend more with us each year; expansion is our cheapest growth.", metric: "Net revenue retention | percent | up | 104 | 112", ongoing: true}, "ceo@example.com"},
	{entry{title: "Be trustworthy at scale", whyItMatters: "Outages and slow pages cost us renewals; customers need the product to just work.", metric: "Availability | percent | up | 99.5 | 99.95", ongoing: true}, "cto@example.com"},
	{entry{title: "Delight customers in their first week", whyItMatters: "Most churn is decided in week one; new customers who reach value fast stay.", metric: "Week-1 activation | percent | up | 31 | 45", ongoing: true}, "cpo@example.com"},
}

// team is one team: its lead owns the team Goals, its people own the projects,
// and its unlinked entries are side projects that contribute to nothing.
type team struct {
	name     string
	lead     string
	people   []string
	goals    []entry
	projects []entry
	unlinked []entry
}

// The leadership team's addresses: the CEO is at the top of the Manager tree,
// with the CTO and CPO reporting to them.
const (
	ceoEmail = "ceo@example.com"
	ctoEmail = "cto@example.com"
	cpoEmail = "cpo@example.com"
)

// teamExecutives is the executive each team's lead reports to, by team name.
var teamExecutives = map[string]string{
	"Platform": ctoEmail,
	"Mobile":   ctoEmail,
	"Data":     ctoEmail,
	"Payments": cpoEmail,
	"Growth":   cpoEmail,
	"Support":  cpoEmail,
}

var teams = []team{
	{
		name:   "Platform",
		lead:   "platform-lead@example.com",
		people: []string{"ada.okafor@example.com", "tomas.berg@example.com", "mei.lin@example.com"},
		goals: []entry{
			{title: "Cut infrastructure cost per request", whyItMatters: "Our cloud bill grows faster than traffic; every saved cent funds product work.", metric: "Cost per 1k requests | USD | down | 0.42 | 0.30", ongoing: true, parents: []int{1}},
			{title: "Zero-downtime deploys for every service", whyItMatters: "Deploy windows cause customer-visible blips and slow every team's release cadence.", parents: []int{1}},
		},
		projects: []entry{
			{title: "Migrate queues to managed Kafka", whyItMatters: "Self-run brokers page us weekly; a managed service removes the toil.", parents: []int{0}},
			{title: "Autoscale the API tier", whyItMatters: "We pay for peak capacity all day while traffic is idle most nights.", metric: "Idle capacity | percent | down | 45 | 20", parents: []int{0}},
			{title: "Retire the legacy job runner", whyItMatters: "Batch jobs on the old runner fail silently and block schema changes.", parents: []int{1}},
			{title: "Blue-green deploys for the monolith", whyItMatters: "The monolith still needs a maintenance window, which customers notice.", parents: []int{1}},
			{title: "Consolidate the logging pipeline", whyItMatters: "Three log stacks triple the cost and nobody knows which one to search.", parents: []int{0}},
		},
		unlinked: []entry{
			{title: "Evaluate ARM build agents", whyItMatters: "Builds are slow and ARM agents may be cheaper; worth a spike."},
		},
	},
	{
		name:   "Payments",
		lead:   "payments-lead@example.com",
		people: []string{"noor.haddad@example.com", "luca.romano@example.com", "sam.kowalski@example.com"},
		goals: []entry{
			{title: "Reduce failed payments", whyItMatters: "Failed renewals are involuntary churn: customers who wanted to stay but lapsed.", metric: "Failed payment rate | percent | down | 3.1 | 1.5", ongoing: true, parents: []int{0}},
			{title: "Launch invoicing for enterprise", whyItMatters: "Enterprise buyers can't pay by card; without invoices we lose large deals.", parents: []int{0}},
		},
		projects: []entry{
			{title: "Retry soft declines automatically", whyItMatters: "Many declines are temporary; a smart retry recovers revenue with no customer effort.", metric: "Recovered declines | percent | up | 12 | 35", parents: []int{0}},
			{title: "Add SEPA direct debit", whyItMatters: "European customers expect bank debits and card-only checkout loses them.", parents: []int{0}},
			{title: "Branded PDF invoices", whyItMatters: "Procurement teams reject invoices that don't carry our legal details.", parents: []int{1}},
			{title: "Net-30 terms for enterprise accounts", whyItMatters: "Enterprise finance teams pay on terms; upfront payment stalls signatures.", parents: []int{1}},
			{title: "Redesign dunning emails", whyItMatters: "Our payment-failed emails read like threats and customers ignore them.", parents: []int{0}},
		},
		unlinked: []entry{
			{title: "Spike: instant payouts", whyItMatters: "A few partners asked for faster payouts; we want to size the work."},
		},
	},
	{
		name:   "Growth",
		lead:   "growth-lead@example.com",
		people: []string{"ines.duarte@example.com", "kwame.mensah@example.com", "hana.sato@example.com"},
		goals: []entry{
			{title: "Lift trial-to-paid conversion", whyItMatters: "Most trials end without a decision; converting more of them is our biggest lever.", metric: "Trial-to-paid | percent | up | 8 | 12", ongoing: true, parents: []int{0, 2}},
			{title: "Launch a referral program", whyItMatters: "Our happiest customers already recommend us; rewarding it should compound.", parents: []int{0}},
		},
		projects: []entry{
			{title: "Redesign the pricing page", whyItMatters: "Visitors can't tell which plan fits them and leave to ask sales.", parents: []int{0}},
			{title: "In-app upgrade prompts", whyItMatters: "Trial users hit plan limits with no path to upgrade from where they are.", metric: "Prompt click-through | percent | up | 2 | 6", parents: []int{0}},
			{title: "Referral rewards ledger", whyItMatters: "Rewards are tracked in a spreadsheet and customers wait weeks for credit.", parents: []int{1}},
			{title: "Lifecycle email journeys", whyItMatters: "Trials get one welcome email and then silence until the trial ends.", parents: []int{0}},
			{title: "Experiment platform v2", whyItMatters: "Each test takes a sprint to wire up, so we run too few of them.", parents: []int{0}},
		},
		unlinked: []entry{
			{title: "Refresh brand illustrations", whyItMatters: "Our illustrations predate the rebrand and look inconsistent on the site."},
		},
	},
	{
		name:   "Mobile",
		lead:   "mobile-lead@example.com",
		people: []string{"olu.adeyemi@example.com", "freya.nilsen@example.com", "diego.alvarez@example.com"},
		goals: []entry{
			{title: "Ship offline mode", whyItMatters: "Field teams lose work when signal drops; they need the app to work offline.", parents: []int{2}},
			{title: "Raise the app store rating", whyItMatters: "A sub-4 rating scares off new customers before they ever open the app.", metric: "App store rating | stars | up | 3.9 | 4.5", ongoing: true, parents: []int{2}},
		},
		projects: []entry{
			{title: "Local-first sync engine", whyItMatters: "The app assumes a live connection for every read and write.", parents: []int{0}},
			{title: "Conflict resolution UI", whyItMatters: "Offline edits will collide and people need a clear way to pick a winner.", parents: []int{0}},
			{title: "Crash-free sessions to 99.8%", whyItMatters: "Crashes are the top complaint in one-star reviews.", metric: "Crash-free sessions | percent | up | 99.1 | 99.8", parents: []int{1}},
			{title: "Faster cold start on Android", whyItMatters: "Low-end Android phones take eight seconds to open the app.", metric: "Cold start p90 | seconds | down | 8 | 3", parents: []int{1}},
			{title: "Accessible navigation audit", whyItMatters: "Screen-reader users can't reach half the app's screens.", parents: []int{1}},
		},
		unlinked: []entry{
			{title: "Tablet layout exploration", whyItMatters: "Some customers use tablets on site; we don't know what they'd need."},
		},
	},
	{
		name:   "Data",
		lead:   "data-lead@example.com",
		people: []string{"yara.nasser@example.com", "ben.fischer@example.com", "anika.rao@example.com"},
		goals: []entry{
			{title: "Trustworthy revenue metrics", whyItMatters: "Finance and sales report different ARR numbers and leadership trusts neither.", parents: []int{0}},
			{title: "Self-serve analytics for every team", whyItMatters: "Every question waits in the data team's queue for days.", metric: "Weekly dashboard users | people | up | 40 | 150", ongoing: true, parents: []int{0}},
		},
		projects: []entry{
			{title: "Single source of truth for ARR", whyItMatters: "ARR is computed in four places with four different rules.", parents: []int{0}},
			{title: "Warehouse freshness under one hour", whyItMatters: "Dashboards show yesterday's data, so teams make calls on stale numbers.", metric: "Warehouse lag | hours | down | 26 | 1", parents: []int{1}},
			{title: "Dashboard catalog", whyItMatters: "Nobody can find the dashboard that already answers their question.", parents: []int{1}},
			{title: "Data contracts for event streams", whyItMatters: "Producers change event shapes and silently break downstream reports.", parents: []int{0}},
			{title: "PII tagging in the warehouse", whyItMatters: "We can't answer a deletion request because we don't know where PII lives.", parents: []int{0}},
		},
		unlinked: []entry{
			{title: "Prototype a natural-language query assistant", whyItMatters: "Might let non-analysts ask questions directly; unproven."},
		},
	},
	{
		name:   "Support",
		lead:   "support-lead@example.com",
		people: []string{"grace.oduya@example.com", "matteo.ricci@example.com", "leah.cohen@example.com"},
		goals: []entry{
			{title: "Resolve tickets faster", whyItMatters: "Customers wait more than a day for answers and it shows up in renewals.", metric: "Median time to resolution | hours | down | 26 | 12", ongoing: true, parents: []int{1, 2}},
			{title: "Launch a self-serve help center", whyItMatters: "Half our tickets ask questions a good article would answer.", parents: []int{2}},
		},
		projects: []entry{
			{title: "Macro library for the top 50 issues", whyItMatters: "Agents rewrite the same answers from scratch every day.", parents: []int{0}},
			{title: "Help center search", whyItMatters: "Customers can't find articles because search matches titles only.", parents: []int{1}},
			{title: "Chatbot deflection for billing questions", whyItMatters: "Billing questions are a third of volume and mostly routine.", metric: "Billing tickets deflected | percent | up | 0 | 30", parents: []int{0}},
			{title: "Escalation routing to engineering", whyItMatters: "Bugs bounce between teams for days before reaching the right engineer.", parents: []int{0}},
			{title: "Help center article backlog", whyItMatters: "The top fifty ticket topics have no article to point customers to.", parents: []int{1}},
		},
	},
}
