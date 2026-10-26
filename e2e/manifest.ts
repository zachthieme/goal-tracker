// Manifest is the seed's picks for the specs: the Goals and people each spec
// acts on, chosen by role. cmd/seed writes one beside each template database
// (global-setup.ts), judged at startAt, the instant each test's app starts.
// It mirrors seed.Manifest in internal/seed/manifest.go: a role with no pick
// is null, and a list with none is empty. A new role is added there, here and
// in TestE2EScenarioSituations together.
export type Manifest = {
  // quietGoal is an Active Goal whose Owner can check in Green with only a
  // status: Green last time, no Metric, no accepted child Goals, no overdue
  // Planned Milestone, and no Highlight or Draft Highlight yet.
  quietGoal: ManifestGoal | null;
  // draftlessGoal is an Active Goal with no Draft Highlight, whose Owner is
  // still here.
  draftlessGoal: ManifestGoal | null;
  // metricGoal is an Active Goal with a Metric and a Check-in, no Draft
  // Highlight and no overdue Planned Milestone, whose Owner is still here.
  metricGoal: ManifestGoal | null;
  // metricParentGoal is an Active Goal with a Metric, a Check-in and accepted
  // child Goals, whose Owner is still here.
  metricParentGoal: ManifestGoal | null;
  // noChangeGoal is a Goal its Owner can press No change on: Active, checked
  // in before, no accepted child Goals, no overdue Planned Milestone, and no
  // back-to-Green date passed on its latest Check-in.
  noChangeGoal: ManifestGoal | null;
  // dueNoChangeGoal is a noChangeGoal with a 7-day cadence.
  dueNoChangeGoal: ManifestGoal | null;
  // accountIDs is every Account's ID by email, for the log lines that name a
  // person by account_id.
  accountIDs: Record<string, number>;
  findingTrouble: FindingTrouble;
  understandingTrouble: UnderstandingTrouble;
  watchingGoals: WatchingGoals;
  weeklyCheckin: WeeklyCheckin;
  badNewsChain: BadNewsChain | null;
  statingAGoal: StatingAGoal;
};

// ManifestGoal is a picked Goal and its Owner. name is the Owner's Name, ""
// when they have none.
export type ManifestGoal = { id: number; title: string; email: string; name: string };

// ManifestPerson is a picked person. name is their Name, "" when they have
// none.
export type ManifestPerson = { email: string; name: string };

// FindingTrouble is scenarios/01-finding-trouble.spec.ts's picks.
export type FindingTrouble = {
  // checkinOverdue is a Check-in overdue Platform Goal the leader doesn't
  // own, whose Owner is still here.
  checkinOverdue: ManifestGoal | null;
  // departing is an Active Platform Goal whose Owner the Admin departs: one
  // who owns no Top-level and no Check-in overdue Active Goal, and is neither
  // the leader nor an Admin.
  departing: ManifestGoal | null;
  // overdue is an Active Platform Goal to put past its Path to Green: not
  // Check-in overdue, no Active child Goals, no overdue Planned Milestone.
  overdue: ManifestGoal | null;
  // tiered are the first two Active Platform Goals.
  tiered: ManifestGoal[];
  // unlinked is an Active Goal, not Top-level, contributing to no Goal.
  unlinked: ManifestGoal | null;
  // twoReasons is a Check-in overdue Goal due later than a Goal it
  // contributes to, with its Team.
  twoReasons: (ManifestGoal & { team: string }) | null;
  // ownerless are departing's Owner's Active Goals.
  ownerless: ManifestGoal[];
  // redWithChildren is a Red Goal, not Check-in overdue, contributing to a
  // Goal and with Active child Goals.
  redWithChildren: ManifestGoal | null;
  // redSlipped is a Red Goal whose delivery date has slipped.
  redSlipped: ManifestGoal | null;
  // lifecycles is every Goal's Lifecycle by Goal ID.
  lifecycles: Record<number, string>;
};

// UnderstandingTrouble is scenarios/02-understanding-trouble.spec.ts's picks.
export type UnderstandingTrouble = {
  // platformReport are the Goals a "Team is any of Platform" rule lists
  // against a baseline 30 days back: every Platform Goal less those Done or
  // Cancelled before then.
  platformReport: ReportGoal[];
  // slipped is a Green Dated Platform Goal its Owner can check in on
  // plainly, to slip.
  slipped: ReportGoal | null;
  // turning is another Platform Goal its Owner can check in on plainly,
  // Yellow first if one is, to turn.
  turning: ReportGoal | null;
  // moved is a third Platform Goal, to move to another Team.
  moved: ReportGoal | null;
};

// ReportGoal is a Goal as a Report's block shows it. owner is the Owner's
// Name, or their email when they have none; status, path_to_green and health
// are its latest Check-in's, "" when it has none.
export type ReportGoal = {
  id: number;
  title: string;
  owner: string;
  email: string;
  lifecycle: string;
  why_it_matters: string;
  delivery_date: string;
  status: string;
  path_to_green: string;
  milestones: number;
  metrics: number;
  health: string;
};

// WatchingGoals is scenarios/02-watching-goals.spec.ts's picks.
export type WatchingGoals = {
  // growthReport are the Growth Goals: inReport those a "Team is any of
  // Growth" rule lists against a baseline 30 days back, and finishedEarlier
  // those Done or Cancelled before then.
  growthReport: { inReport: ManifestGoal[]; finishedEarlier: ManifestGoal[] };
  // picked are Active, checked-in Goals on a Team other than Growth that the
  // manager neither owns nor is a Delegate on.
  picked: {
    // troubled is Yellow or Red, with a Path to Green.
    troubled: WatchedGoal | null;
    // slipped is Yellow or Red, and has slipped its delivery date.
    slipped: WatchedGoal | null;
    // green has no Active child Goals and no overdue Planned Milestone.
    green: WatchedGoal | null;
    // parent has Active child Goals, none of them another pick.
    parent: WatchedGoal | null;
    // children are parent's Active child Goals.
    children: ManifestGoal[];
  };
};

// WatchedGoal is a picked Goal with its latest Check-in's Health and Path to
// Green, and slipped_from, the delivery date its first slip moved it from, ""
// when it hasn't slipped.
export type WatchedGoal = { id: number; title: string; health: string; path_to_green: string; slipped_from: string };

// WeeklyCheckin is scenarios/03-weekly-checkin.spec.ts's picks.
export type WeeklyCheckin = {
  // weekOwners could be the week's Owner, most Active Goals first: at least
  // two Active Goals, exactly one with a Metric, and none a No change would
  // be refused on. Never the leader. Which of their Goals are due is read
  // from Home.
  weekOwners: (ManifestPerson & { goals: { id: number; title: string; metric: boolean }[] })[];
  delegateCover: {
    // goal is Active, last checked in more than its cadence and a day ago,
    // with no accepted child Goals, overdue Planned Milestone or Delegate,
    // and an Owner with a Name: the longest unchecked first.
    goal: ManifestGoal | null;
    // delegate is the first other person by ID, neither Departed nor an
    // Admin, with a Name.
    delegate: ManifestPerson | null;
  };
};

// BadNewsChain is scenarios/04-reporting-bad-news.spec.ts's chain: a project,
// the team Goal it contributes to, and an org outcome the team Goal
// contributes to whose other Active children are all Green. Each name is the
// Owner's Name, or their email when they have none.
export type BadNewsChain = {
  // project carries its Planned Milestones dated after today, soonest
  // first, and its Team, the Dimension value a Report rule matches.
  project: BadNewsGoal & { milestones: { id: number; name: string; target_date: string }[]; team: string };
  team: BadNewsGoal;
  orgOutcome: BadNewsGoal;
};

export type BadNewsGoal = { id: number; title: string; email: string; name: string; delivery_date: string };

// StatingAGoal is scenarios/05-stating-a-goal.spec.ts's picks.
export type StatingAGoal = {
  // teamGoals are the leads' Active Goals with a Team, by ID, each with its
  // projectOwners: the people, neither a lead nor the leadership, who own an
  // Active Goal contributing to a lead's Goal, those under this one first,
  // then by email. Which shows Rolled-up Health Green, or none, is read from
  // the page.
  teamGoals: (ManifestGoal & { team: string; projectOwners: ManifestPerson[] })[];
  // projectOwner is the first project Owner by email.
  projectOwner: ManifestPerson | null;
};

// requirePick is a role's pick, or an error naming the role when the manifest
// has none.
export function requirePick<T>(role: string, pick: T | null | undefined): T {
  if (pick === null || pick === undefined) throw new Error(`the seed's manifest has no pick for ${role}`);
  return pick;
}
