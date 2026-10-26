package domain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// A rejection or removal whose time doesn't parse makes the history an error
// naming the raw value, not a history reading it as the zero time.
func TestLinkPairHistoryFromRefusesAnUnparseableTime(t *testing.T) {
	t.Parallel()
	good := "2026-03-01T09:00:00Z"
	for _, tc := range []struct {
		name       string
		rejections []db.ListLinkPairRejectionsRow
		removals   []db.ListLinkPairRemovalsRow
	}{
		{"rejection", []db.ListLinkPairRejectionsRow{{ID: 1, RejectedAt: "yesterday"}}, []db.ListLinkPairRemovalsRow{{ID: 1, RemovedAt: good}}},
		{"removal", []db.ListLinkPairRejectionsRow{{ID: 1, RejectedAt: good}}, []db.ListLinkPairRemovalsRow{{ID: 1, RemovedAt: "yesterday"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := linkPairHistoryFrom(false, tc.rejections, tc.removals)
			if err == nil {
				t.Fatal("linkPairHistoryFrom returned a history, want an error")
			}
			if !strings.Contains(err.Error(), `"yesterday"`) {
				t.Errorf("error %q doesn't name the raw value", err)
			}
		})
	}
}

// linkPairJudgement is one Undo's judgement of a linkPairHistory: whether the
// pair has moved on since the thing it undoes, at t, with id among its own
// kind of record.
type linkPairJudgement struct {
	name string
	// rejectionsOwn says whether the judgement's own kind of record is a
	// rejection; the other kind is then a removal.
	rejectionsOwn bool
	refusal       string
	judge         func(h linkPairHistory, id int64, t time.Time) error
}

// Each Undo that would bring a link back refuses once the pair has moved on
// since: a record of its own kind is later by id, whatever its time; one of
// the other kind is since when it is no earlier than t. Restoring a removal
// leaves "the link exists now" to RestoreLink. Undoing a decline has no kind
// of its own, so it judges both kinds by time.
func TestLinkPairHistoryJudgesWhetherThePairHasMovedOn(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	const id = 5
	requestRestore := linkPairJudgement{"restore a rejected request", true,
		"this link has been requested again since it was rejected",
		func(h linkPairHistory, id int64, t time.Time) error { return h.requestRestoreRefusal(id, t) }}
	linkRestore := linkPairJudgement{"restore a removed link", false,
		"this link has been requested again since it was removed",
		func(h linkPairHistory, id int64, t time.Time) error { return h.linkRestoreRefusal(id, t) }}
	declineUndo := func(rejectionsOwn bool) linkPairJudgement {
		return linkPairJudgement{"undo a decline", rejectionsOwn,
			"this link has been requested since the suggestion was declined",
			func(h linkPairHistory, _ int64, t time.Time) error { return h.declineUndoRefusal(t) }}
	}
	type record struct {
		own bool
		id  int64
		at  time.Time
	}
	for _, tc := range []struct {
		history string
		linked  bool
		record  *record
		// Whether restoring a request, restoring a link, and undoing a
		// decline each refuse.
		request, link, decline bool
	}{
		{history: "nothing since"},
		{history: "the link exists now", linked: true, request: true, decline: true},
		{history: "other kind earlier", record: &record{false, 1, at.Add(-time.Hour)}},
		{history: "other kind at the same time", record: &record{false, 1, at}, request: true, link: true, decline: true},
		{history: "other kind later", record: &record{false, 1, at.Add(time.Hour)}, request: true, link: true, decline: true},
		{history: "own kind, higher id, earlier", record: &record{true, id + 1, at.Add(-time.Hour)}, request: true, link: true},
		{history: "own kind, lower id, later", record: &record{true, id - 1, at.Add(time.Hour)}, decline: true},
	} {
		for _, j := range []struct {
			judgement linkPairJudgement
			refused   bool
		}{
			{requestRestore, tc.request},
			{linkRestore, tc.link},
			{declineUndo(true), tc.decline},
			{declineUndo(false), tc.decline},
		} {
			h := linkPairHistory{linked: tc.linked}
			kind := "removals"
			if j.judgement.rejectionsOwn {
				kind = "rejections"
			}
			if r := tc.record; r != nil {
				rec := linkPairRecord{id: r.id, at: r.at}
				if r.own == j.judgement.rejectionsOwn {
					h.rejections = append(h.rejections, rec)
				} else {
					h.removals = append(h.removals, rec)
				}
			}
			t.Run(j.judgement.name+"/own kind "+kind+"/"+tc.history, func(t *testing.T) {
				t.Parallel()
				err := j.judgement.judge(h, id, at)
				if !j.refused {
					if err != nil {
						t.Fatalf("refused with %v, want allowed", err)
					}
					return
				}
				if err == nil {
					t.Fatal("allowed, want refused")
				}
				if !errors.Is(err, ErrValidation) || Reason(err) != j.judgement.refusal {
					t.Errorf("refused with %v, want ErrValidation %q", err, j.judgement.refusal)
				}
			})
		}
	}
}
