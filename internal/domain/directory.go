package domain

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// DirectoryPerson is one person as the org's directory lists them: their email,
// their Name, and their Manager's email, empty for none (ADR 0008).
type DirectoryPerson struct {
	Email        string
	Name         string
	ManagerEmail string
}

// Directory is the org's directory, where Managers come from (ADR 0008). The
// first one reads Authentik's users API; another provider, or SCIM, is another
// implementation, and nothing above it changes.
type Directory interface {
	// People lists everyone in the directory, or fails without listing any.
	People(ctx context.Context) ([]DirectoryPerson, error)
}

// DirectorySyncResult is what one sync of the directory found: how many
// people it read, and the cycles among the Managers it stored, each as the
// people in it in Manager order.
type DirectorySyncResult struct {
	People int
	Cycles [][]Account
}

// SyncDirectory reads everyone from dir and records each person the way the
// spreadsheet import does: an Account, matched whatever its case and created
// if there is none, with their Name from the directory when it gives one, and
// their Manager, whose Account is created too if needed (ADR 0008). A person
// with no Manager email has none. It never marks anyone Departed, and someone
// the directory doesn't list keeps the Manager they had. Cycles are stored as
// the directory gives them, and returned. Nothing changes unless it all does:
// a directory that can't be read, or a person that can't be recorded, leaves
// every Account as it was.
func (s *Service) SyncDirectory(ctx context.Context, dir Directory) (DirectorySyncResult, error) {
	people, err := dir.People(ctx)
	if err != nil {
		return DirectorySyncResult{}, fmt.Errorf("read the directory: %w", err)
	}
	var out DirectorySyncResult
	err = s.WithinTx(ctx, func(tx *Service) error {
		for _, p := range people {
			if strings.TrimSpace(p.Email) == "" {
				continue
			}
			if err := tx.recordDirectoryPerson(ctx, p); err != nil {
				return err
			}
			out.People++
		}
		cycles, err := tx.managerCycles(ctx)
		out.Cycles = cycles
		return err
	})
	if err != nil {
		return DirectorySyncResult{}, err
	}
	return out, nil
}

// recordDirectoryPerson gives p an Account, their Name, and their Manager.
func (s *Service) recordDirectoryPerson(ctx context.Context, p DirectoryPerson) error {
	acc, err := s.EnsureAccount(ctx, p.Email)
	if err != nil {
		return err
	}
	if name := strings.TrimSpace(p.Name); name != "" && name != acc.Name {
		if err := s.SetName(ctx, acc.ID, name); err != nil {
			return err
		}
	}
	var managerID *int64
	if m := strings.TrimSpace(p.ManagerEmail); m != "" {
		manager, err := s.EnsureAccount(ctx, m)
		if err != nil {
			return err
		}
		managerID = &manager.ID
	}
	if !sameManager(acc.ManagerID, managerID) {
		if err := s.queries.SetAccountManager(ctx, db.SetAccountManagerParams{ManagerID: managerID, ID: acc.ID}); err != nil {
			return fmt.Errorf("set manager: %w", err)
		}
	}
	return nil
}

func sameManager(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// managerCycles finds the cycles among everyone's Managers, a self-reference
// included, each as its people in Manager order.
func (s *Service) managerCycles(ctx context.Context) ([][]Account, error) {
	rows, err := s.queries.ListManagedAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list managers: %w", err)
	}
	accounts := make(map[int64]Account, len(rows))
	for _, r := range rows {
		accounts[r.ID] = accountFromRow(r)
	}
	// Each person has at most one Manager, so a walk up from anyone either
	// ends at someone with none or enters a cycle.
	const (
		unseen = iota
		walking
		done
	)
	state := make(map[int64]int, len(rows))
	var cycles [][]Account
	for _, r := range rows {
		var path []int64
		for id := r.ID; ; {
			acc, managed := accounts[id]
			if !managed || state[id] == done {
				break
			}
			if state[id] == walking {
				var cycle []Account
				for _, member := range path[slices.Index(path, id):] {
					cycle = append(cycle, accounts[member])
				}
				cycles = append(cycles, cycle)
				break
			}
			state[id] = walking
			path = append(path, id)
			id = *acc.ManagerID
		}
		for _, id := range path {
			state[id] = done
		}
	}
	return cycles, nil
}
