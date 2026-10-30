package domain

import "context"

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

// importing says whether s runs an import for actorID.
func (s *Service) importing(actorID int64) bool {
	return s.goalImport != nil && s.goalImport.adminID == actorID
}

// importedBy says whether goalID was created by the import s runs for
// actorID.
func (s *Service) importedBy(actorID, goalID int64) bool {
	return s.importing(actorID) && s.goalImport.created[goalID]
}

// requireImportingAdmin refuses anyone but the Admin running the import s runs
// for, and everyone when it runs for none.
func (s *Service) requireImportingAdmin(actorID int64) error {
	if s.goalImport == nil || s.goalImport.adminID != actorID {
		return Refuse(ErrNotAuthorized, "only an Admin's import may do this")
	}
	return nil
}

// recordImportedGoal notes that the import s runs, if any, created goalID.
func (s *Service) recordImportedGoal(goalID int64) {
	if s.goalImport != nil {
		s.goalImport.created[goalID] = true
	}
}
