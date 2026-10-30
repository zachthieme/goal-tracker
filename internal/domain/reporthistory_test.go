package domain_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// A draft's History merges its publications with its Report log, newest
// first. A publication stands for the log's Published entry, which is left
// out; one made at the same moment as another log entry comes first; and a
// log entry links to no publication.
func TestReportHistoryMergesPublicationsWithTheReportLogNewestFirst(t *testing.T) {
	t.Parallel()

	boss := domain.Account{ID: 1, Email: "boss@example.com"}
	ada := domain.Account{ID: 2, Email: "ada@example.com", Name: "Ada Okafor"}
	at := func(day, hour int) time.Time { return time.Date(2026, 3, day, hour, 0, 0, 0, time.UTC) }
	second := domain.Publication{ID: 12, DefinitionID: 7, Name: "MBR", PublishedBy: ada, PublishedAt: at(3, 9)}
	first := domain.Publication{ID: 11, DefinitionID: 7, Name: "MBR", PublishedBy: boss, PublishedAt: at(2, 9)}
	log := []domain.ReportLogEntry{
		{ID: 6, Actor: ada, Kind: domain.ReportLogPublished, Detail: "Published", CreatedAt: at(3, 9)},
		{ID: 5, Actor: boss, Kind: domain.ReportLogNarrative, Detail: "Narrative: Misses notes changed", CreatedAt: at(3, 9)},
		{ID: 4, Actor: boss, Kind: domain.ReportLogEdited, Detail: "Title changed", CreatedAt: at(2, 15)},
		{ID: 3, Actor: boss, Kind: domain.ReportLogPublished, Detail: "Published", CreatedAt: at(2, 9)},
		{ID: 2, Actor: boss, Kind: domain.ReportLogDelegate, Detail: "Report Delegate added: Ada Okafor", CreatedAt: at(1, 9)},
		{ID: 1, Actor: boss, Kind: domain.ReportLogCreated, Detail: "Created", CreatedAt: at(1, 9)},
	}

	got := domain.ReportHistory([]domain.Publication{second, first}, log)

	want := []domain.ReportHistoryEntry{
		{Detail: "Published", By: ada, At: at(3, 9), Publication: &second},
		{Detail: "Narrative: Misses notes changed", By: boss, At: at(3, 9)},
		{Detail: "Title changed", By: boss, At: at(2, 15)},
		{Detail: "Published", By: boss, At: at(2, 9), Publication: &first},
		{Detail: "Report Delegate added: Ada Okafor", By: boss, At: at(1, 9)},
		{Detail: "Created", By: boss, At: at(1, 9)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReportHistory =\n%+v\nwant\n%+v", got, want)
	}
}

// A publication with no Published entry in the Report log, such as one from
// before the log, still shows in the History in its place.
func TestReportHistoryKeepsAPublicationTheLogHasNoEntryFor(t *testing.T) {
	t.Parallel()

	boss := domain.Account{ID: 1, Email: "boss@example.com"}
	at := func(day int) time.Time { return time.Date(2026, 3, day, 9, 0, 0, 0, time.UTC) }
	newer := domain.Publication{ID: 2, DefinitionID: 7, PublishedBy: boss, PublishedAt: at(4)}
	older := domain.Publication{ID: 1, DefinitionID: 7, PublishedBy: boss, PublishedAt: at(1)}
	log := []domain.ReportLogEntry{{ID: 1, Actor: boss, Kind: domain.ReportLogEdited, Detail: "Title changed", CreatedAt: at(2)}}

	got := domain.ReportHistory([]domain.Publication{newer, older}, log)

	want := []domain.ReportHistoryEntry{
		{Detail: "Published", By: boss, At: at(4), Publication: &newer},
		{Detail: "Title changed", By: boss, At: at(2)},
		{Detail: "Published", By: boss, At: at(1), Publication: &older},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReportHistory =\n%+v\nwant\n%+v", got, want)
	}
	if got := domain.ReportHistory(nil, nil); len(got) != 0 {
		t.Errorf("ReportHistory of nothing = %+v, want none", got)
	}
}
