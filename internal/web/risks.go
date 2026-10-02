package web

import (
	"context"
	"net/http"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleRisks answers "what's going wrong?" for leadership: every Goal the
// freshness and graph signals flag, by problem type.
func (s *Server) handleRisks(w http.ResponseWriter, r *http.Request, current domain.Account) {
	v, err := s.loadRisks(r.Context())
	if err != nil {
		http.Error(w, "could not read risks", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, risksPage(&current, v))
}

// risksView is what the Risks page shows, one list per problem type.
type risksView struct {
	Stale        []domain.GoalFreshness
	OverduePaths []domain.GoalFreshness
	// Ownerless are the Active Goals whose Owner has left the org.
	Ownerless []domain.Goal
	Unaligned []domain.Goal
	// ScheduleConflicts and HaltedParents are listed by their child, the Goal
	// that needs to change.
	ScheduleConflicts []domain.ScheduleConflict
	HaltedParents     []domain.HaltedParent
}

// riskType is one of the Risks page's sections: the anchor its summary tile
// links to, its name, and how many Goals it lists.
type riskType struct {
	Anchor, Name string
	Count        int
}

// Types are the Risks page's sections in the page's order.
func (v risksView) Types() []riskType {
	return []riskType{
		{"stale", "Stale", len(v.Stale)},
		{"path-overdue", "Path to Green overdue", len(v.OverduePaths)},
		{"ownerless", "Ownerless", len(v.Ownerless)},
		{"unaligned", "Unaligned", len(v.Unaligned)},
		{"schedule-conflicts", "Schedule conflicts", len(v.ScheduleConflicts)},
		{"halted-parents", "Parent On Hold or Cancelled", len(v.HaltedParents)},
	}
}

// Empty are the sections with no Goals, in the page's order.
func (v risksView) Empty() []riskType {
	var empty []riskType
	for _, rt := range v.Types() {
		if rt.Count == 0 {
			empty = append(empty, rt)
		}
	}
	return empty
}

// Flagged counts the Goals the Risks page lists, each once however many
// sections it is in.
func (v risksView) Flagged() int {
	flagged := map[int64]bool{}
	for _, list := range [][]domain.GoalFreshness{v.Stale, v.OverduePaths} {
		for _, gf := range list {
			flagged[gf.Goal.ID] = true
		}
	}
	for _, list := range [][]domain.Goal{v.Ownerless, v.Unaligned} {
		for _, g := range list {
			flagged[g.ID] = true
		}
	}
	for _, c := range v.ScheduleConflicts {
		flagged[c.Child.ID] = true
	}
	for _, hp := range v.HaltedParents {
		flagged[hp.Child.ID] = true
	}
	return len(flagged)
}

// loadRisks reads every Goal the org's signals flag.
func (s *Server) loadRisks(ctx context.Context) (risksView, error) {
	fresh, err := s.svc.FreshnessSignals(ctx)
	if err != nil {
		return risksView{}, err
	}
	goals, err := s.svc.ListGoals(ctx)
	if err != nil {
		return risksView{}, err
	}
	graph, err := s.svc.GraphSignals(ctx)
	if err != nil {
		return risksView{}, err
	}
	v := risksView{
		Stale:             fresh.Stale,
		OverduePaths:      fresh.OverduePaths,
		Unaligned:         graph.Unaligned,
		ScheduleConflicts: graph.ScheduleConflicts,
		HaltedParents:     graph.HaltedParents,
	}
	for _, g := range goals {
		if g.Ownerless && g.Lifecycle == domain.LifecycleActive {
			v.Ownerless = append(v.Ownerless, g)
		}
	}
	return v, nil
}
