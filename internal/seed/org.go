package seed

// The fake org's shape: three org-wide outcomes, and six teams (a Team
// Dimension, since teams are not built in — ADR 0002), each with two team Goals
// contributing to those outcomes and a handful of projects contributing to the
// team Goals. A few projects contribute to nothing and read as Unaligned. The
// words are fixed here; the random seed decides dates, Owners, and how each
// Goal's history plays out.

// teamDimension is the name of the Dimension the seed defines for teams.
const teamDimension = "Team"

// entry is one Goal in the plan. metric, when set, is an import-format Metric
// without its target date ("Name | unit | up|down | baseline | target"); an
// ongoing entry is an Ongoing Goal and must carry one.
type entry struct {
	title   string
	soWhat  string
	metric  string
	ongoing bool
	// parents are the indexes of the outcomes (for a team Goal) or the team's
	// Goals (for a project) this Goal contributes to.
	parents []int
}

// outcomes are the org's Top-level outcomes, owned by the leadership team.
var outcomes = []struct {
	entry
	owner string
}{
	{entry{title: "Grow net revenue retention", soWhat: "Customers who stay should spend more with us each year; expansion is our cheapest growth.", metric: "Net revenue retention | percent | up | 104 | 112", ongoing: true}, "ceo@example.com"},
	{entry{title: "Be trustworthy at scale", soWhat: "Outages and slow pages cost us renewals; customers need the product to just work.", metric: "Availability | percent | up | 99.5 | 99.95", ongoing: true}, "cto@example.com"},
	{entry{title: "Delight customers in their first week", soWhat: "Most churn is decided in week one; new customers who reach value fast stay.", metric: "Week-1 activation | percent | up | 31 | 45", ongoing: true}, "cpo@example.com"},
}

// team is one team: its lead owns the team Goals, its people own the projects,
// and its unaligned entries are side projects that contribute to nothing.
type team struct {
	name      string
	lead      string
	people    []string
	goals     []entry
	projects  []entry
	unaligned []entry
}

var teams = []team{
	{
		name:   "Platform",
		lead:   "platform-lead@example.com",
		people: []string{"ada.okafor@example.com", "tomas.berg@example.com", "mei.lin@example.com"},
		goals: []entry{
			{title: "Cut infrastructure cost per request", soWhat: "Our cloud bill grows faster than traffic; every saved cent funds product work.", metric: "Cost per 1k requests | USD | down | 0.42 | 0.30", ongoing: true, parents: []int{1}},
			{title: "Zero-downtime deploys for every service", soWhat: "Deploy windows cause customer-visible blips and slow every team's release cadence.", parents: []int{1}},
		},
		projects: []entry{
			{title: "Migrate queues to managed Kafka", soWhat: "Self-run brokers page us weekly; a managed service removes the toil.", parents: []int{0}},
			{title: "Autoscale the API tier", soWhat: "We pay for peak capacity all day while traffic is idle most nights.", metric: "Idle capacity | percent | down | 45 | 20", parents: []int{0}},
			{title: "Retire the legacy job runner", soWhat: "Batch jobs on the old runner fail silently and block schema changes.", parents: []int{1}},
			{title: "Blue-green deploys for the monolith", soWhat: "The monolith still needs a maintenance window, which customers notice.", parents: []int{1}},
			{title: "Consolidate the logging pipeline", soWhat: "Three log stacks triple the cost and nobody knows which one to search.", parents: []int{0}},
		},
		unaligned: []entry{
			{title: "Evaluate ARM build agents", soWhat: "Builds are slow and ARM agents may be cheaper; worth a spike."},
		},
	},
	{
		name:   "Payments",
		lead:   "payments-lead@example.com",
		people: []string{"noor.haddad@example.com", "luca.romano@example.com", "sam.kowalski@example.com"},
		goals: []entry{
			{title: "Reduce failed payments", soWhat: "Failed renewals are involuntary churn: customers who wanted to stay but lapsed.", metric: "Failed payment rate | percent | down | 3.1 | 1.5", ongoing: true, parents: []int{0}},
			{title: "Launch invoicing for enterprise", soWhat: "Enterprise buyers can't pay by card; without invoices we lose large deals.", parents: []int{0}},
		},
		projects: []entry{
			{title: "Retry soft declines automatically", soWhat: "Many declines are temporary; a smart retry recovers revenue with no customer effort.", metric: "Recovered declines | percent | up | 12 | 35", parents: []int{0}},
			{title: "Add SEPA direct debit", soWhat: "European customers expect bank debits and card-only checkout loses them.", parents: []int{0}},
			{title: "Branded PDF invoices", soWhat: "Procurement teams reject invoices that don't carry our legal details.", parents: []int{1}},
			{title: "Net-30 terms for enterprise accounts", soWhat: "Enterprise finance teams pay on terms; upfront payment stalls signatures.", parents: []int{1}},
			{title: "Redesign dunning emails", soWhat: "Our payment-failed emails read like threats and customers ignore them.", parents: []int{0}},
		},
		unaligned: []entry{
			{title: "Spike: instant payouts", soWhat: "A few partners asked for faster payouts; we want to size the work."},
		},
	},
	{
		name:   "Growth",
		lead:   "growth-lead@example.com",
		people: []string{"ines.duarte@example.com", "kwame.mensah@example.com", "hana.sato@example.com"},
		goals: []entry{
			{title: "Lift trial-to-paid conversion", soWhat: "Most trials end without a decision; converting more of them is our biggest lever.", metric: "Trial-to-paid | percent | up | 8 | 12", ongoing: true, parents: []int{0, 2}},
			{title: "Launch a referral program", soWhat: "Our happiest customers already recommend us; rewarding it should compound.", parents: []int{0}},
		},
		projects: []entry{
			{title: "Redesign the pricing page", soWhat: "Visitors can't tell which plan fits them and leave to ask sales.", parents: []int{0}},
			{title: "In-app upgrade prompts", soWhat: "Trial users hit plan limits with no path to upgrade from where they are.", metric: "Prompt click-through | percent | up | 2 | 6", parents: []int{0}},
			{title: "Referral rewards ledger", soWhat: "Rewards are tracked in a spreadsheet and customers wait weeks for credit.", parents: []int{1}},
			{title: "Lifecycle email journeys", soWhat: "Trials get one welcome email and then silence until the trial ends.", parents: []int{0}},
			{title: "Experiment platform v2", soWhat: "Each test takes a sprint to wire up, so we run too few of them.", parents: []int{0}},
		},
		unaligned: []entry{
			{title: "Refresh brand illustrations", soWhat: "Our illustrations predate the rebrand and look inconsistent on the site."},
		},
	},
	{
		name:   "Mobile",
		lead:   "mobile-lead@example.com",
		people: []string{"olu.adeyemi@example.com", "freya.nilsen@example.com", "diego.alvarez@example.com"},
		goals: []entry{
			{title: "Ship offline mode", soWhat: "Field teams lose work when signal drops; they need the app to work offline.", parents: []int{2}},
			{title: "Raise the app store rating", soWhat: "A sub-4 rating scares off new customers before they ever open the app.", metric: "App store rating | stars | up | 3.9 | 4.5", ongoing: true, parents: []int{2}},
		},
		projects: []entry{
			{title: "Local-first sync engine", soWhat: "The app assumes a live connection for every read and write.", parents: []int{0}},
			{title: "Conflict resolution UI", soWhat: "Offline edits will collide and people need a clear way to pick a winner.", parents: []int{0}},
			{title: "Crash-free sessions to 99.8%", soWhat: "Crashes are the top complaint in one-star reviews.", metric: "Crash-free sessions | percent | up | 99.1 | 99.8", parents: []int{1}},
			{title: "Faster cold start on Android", soWhat: "Low-end Android phones take eight seconds to open the app.", metric: "Cold start p90 | seconds | down | 8 | 3", parents: []int{1}},
			{title: "Accessible navigation audit", soWhat: "Screen-reader users can't reach half the app's screens.", parents: []int{1}},
		},
		unaligned: []entry{
			{title: "Tablet layout exploration", soWhat: "Some customers use tablets on site; we don't know what they'd need."},
		},
	},
	{
		name:   "Data",
		lead:   "data-lead@example.com",
		people: []string{"yara.nasser@example.com", "ben.fischer@example.com", "anika.rao@example.com"},
		goals: []entry{
			{title: "Trustworthy revenue metrics", soWhat: "Finance and sales report different ARR numbers and leadership trusts neither.", parents: []int{0}},
			{title: "Self-serve analytics for every team", soWhat: "Every question waits in the data team's queue for days.", metric: "Weekly dashboard users | people | up | 40 | 150", ongoing: true, parents: []int{0}},
		},
		projects: []entry{
			{title: "Single source of truth for ARR", soWhat: "ARR is computed in four places with four different rules.", parents: []int{0}},
			{title: "Warehouse freshness under one hour", soWhat: "Dashboards show yesterday's data, so teams make calls on stale numbers.", metric: "Warehouse lag | hours | down | 26 | 1", parents: []int{1}},
			{title: "Dashboard catalog", soWhat: "Nobody can find the dashboard that already answers their question.", parents: []int{1}},
			{title: "Data contracts for event streams", soWhat: "Producers change event shapes and silently break downstream reports.", parents: []int{0}},
			{title: "PII tagging in the warehouse", soWhat: "We can't answer a deletion request because we don't know where PII lives.", parents: []int{0}},
		},
		unaligned: []entry{
			{title: "Prototype a natural-language query assistant", soWhat: "Might let non-analysts ask questions directly; unproven."},
		},
	},
	{
		name:   "Support",
		lead:   "support-lead@example.com",
		people: []string{"grace.oduya@example.com", "matteo.ricci@example.com", "leah.cohen@example.com"},
		goals: []entry{
			{title: "Resolve tickets faster", soWhat: "Customers wait more than a day for answers and it shows up in renewals.", metric: "Median time to resolution | hours | down | 26 | 12", ongoing: true, parents: []int{1, 2}},
			{title: "Launch a self-serve help center", soWhat: "Half our tickets ask questions a good article would answer.", parents: []int{2}},
		},
		projects: []entry{
			{title: "Macro library for the top 50 issues", soWhat: "Agents rewrite the same answers from scratch every day.", parents: []int{0}},
			{title: "Help center search", soWhat: "Customers can't find articles because search matches titles only.", parents: []int{1}},
			{title: "Chatbot deflection for billing questions", soWhat: "Billing questions are a third of volume and mostly routine.", metric: "Billing tickets deflected | percent | up | 0 | 30", parents: []int{0}},
			{title: "Escalation routing to engineering", soWhat: "Bugs bounce between teams for days before reaching the right engineer.", parents: []int{0}},
			{title: "Help center article backlog", soWhat: "The top fifty ticket topics have no article to point customers to.", parents: []int{1}},
		},
	},
}
