package seed

// The company's fixed shape, for seed_test's checks that the Manager tree
// covers it.

// TeamForTest is one team's name and people.
type TeamForTest struct {
	Name   string
	People []string
}

// TeamsForTest lists every team.
func TeamsForTest() []TeamForTest {
	out := make([]TeamForTest, len(teams))
	for i, t := range teams {
		out[i] = TeamForTest{Name: t.name, People: t.people}
	}
	return out
}

// TeamExecutivesForTest is the executive each team's lead reports to, by team
// name.
func TeamExecutivesForTest() map[string]string { return teamExecutives }

// RoleNamesForTest is the Name of each role address.
func RoleNamesForTest() map[string]string { return roleNames }
