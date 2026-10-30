package domain

import "time"

// ReportHistoryEntry is one of a Report draft's History entries after the
// Draft: a publication, or another change in the Report log (DESIGN.md § A
// Report's draft page).
type ReportHistoryEntry struct {
	// Detail is what happened: "Published", or the log entry's Detail.
	Detail string
	By     Account
	At     time.Time
	// Publication is the publication, and nil for a log entry.
	Publication *Publication
}

// ReportHistory merges a Report Definition's publications, pubs, with its
// Report log, log, both newest first, into its draft's History, newest
// first. A publication stands for its Report log entry, so the log's
// Published entries are left out. A publication made at the same moment as a
// log entry comes first.
func ReportHistory(pubs []Publication, log []ReportLogEntry) []ReportHistoryEntry {
	out := make([]ReportHistoryEntry, 0, len(pubs)+len(log))
	for len(pubs) > 0 || len(log) > 0 {
		if len(log) > 0 && log[0].Kind == ReportLogPublished {
			log = log[1:]
			continue
		}
		if len(pubs) > 0 && (len(log) == 0 || !log[0].CreatedAt.After(pubs[0].PublishedAt)) {
			p := pubs[0]
			out = append(out, ReportHistoryEntry{Detail: "Published", By: p.PublishedBy, At: p.PublishedAt, Publication: &p})
			pubs = pubs[1:]
			continue
		}
		e := log[0]
		out = append(out, ReportHistoryEntry{Detail: e.Detail, By: e.Actor, At: e.CreatedAt})
		log = log[1:]
	}
	return out
}
