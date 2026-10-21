package domain

import (
	"context"
	"fmt"
)

// goalImport is an Admin's spreadsheet import in progress: the Admin running
// it, and the Goals it has created so far (ticket #22).
type goalImport struct {
	adminID int64
	created map[int64]bool
}

// ForImportBy returns the Service an Admin's spreadsheet import runs its
// commands through, bound to the same transaction as s. The Admin, adminID,
// makes the creation-time writes to each Goal the import creates — its Kind,
// Milestones and Metrics, and the links between imported Goals — as its Owner
// would; a Goal the import didn't create is still the Owner's alone. Anyone but
// an Admin is refused with ErrNotAuthorized.
func (s *Service) ForImportBy(ctx context.Context, adminID int64) (*Service, error) {
	if err := s.requireAdmin(ctx, adminID); err != nil {
		return nil, err
	}
	imp := *s
	imp.goalImport = &goalImport{adminID: adminID, created: map[int64]bool{}}
	return &imp, nil
}

// importedBy says whether goalID was created by the import s runs for
// actorID.
func (s *Service) importedBy(actorID, goalID int64) bool {
	return s.goalImport != nil && s.goalImport.adminID == actorID && s.goalImport.created[goalID]
}

// requireImportingAdmin refuses anyone but the Admin running the import s runs
// for, and everyone when it runs for none.
func (s *Service) requireImportingAdmin(actorID int64) error {
	if s.goalImport == nil || s.goalImport.adminID != actorID {
		return fmt.Errorf("%w: only an Admin's import may do this", ErrNotAuthorized)
	}
	return nil
}

// recordImportedGoal notes that the import s runs, if any, created goalID.
func (s *Service) recordImportedGoal(goalID int64) {
	if s.goalImport != nil {
		s.goalImport.created[goalID] = true
	}
}

// authorizeGoalOwnerWrite allows actorID to change goalID only if they are its
// Owner (a Delegate and an Admin are refused), or the Admin whose import
// created it; ErrNotFound if no such Goal exists. It guards the commands the
// Goal page offers its Owner alone.
func (s *Service) authorizeGoalOwnerWrite(ctx context.Context, actorID, goalID int64) error {
	if s.importedBy(actorID, goalID) {
		return nil
	}
	return s.RequireGoalOwner(ctx, actorID, goalID)
}
