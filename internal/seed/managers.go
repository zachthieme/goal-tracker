package seed

import (
	"context"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// seedDirectory is the fake company's directory, where its Managers come from
// as a real company's do (ADR 0008): the seed records them with
// Service.SyncDirectory and never sets a Manager itself.
type seedDirectory []domain.DirectoryPerson

// People lists everyone in the directory.
func (d seedDirectory) People(context.Context) ([]domain.DirectoryPerson, error) {
	return d, nil
}

// managerTree is the company's directory, everyone with their Name and Manager:
// the CEO has none, the CTO, CPO and the Admin report to the CEO, each team's
// lead to their executive in teamExecutives, and each of a team's people to its
// lead. An Admin who is already in the tree keeps their place there.
func managerTree(adminEmail string) seedDirectory {
	var dir seedDirectory
	add := func(addr, manager string) {
		dir = append(dir, domain.DirectoryPerson{Email: addr, Name: nameOf(addr), ManagerEmail: manager})
	}
	add(ceoEmail, "")
	add(ctoEmail, ceoEmail)
	add(cpoEmail, ceoEmail)
	for _, t := range teams {
		add(t.lead, teamExecutives[t.name])
		for _, p := range t.people {
			add(p, t.lead)
		}
	}
	for _, p := range dir {
		if strings.EqualFold(p.Email, adminEmail) {
			return dir
		}
	}
	add(adminEmail, ceoEmail)
	return dir
}
