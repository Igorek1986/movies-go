package releases

import (
	"testing"
	"time"

	"movies-api/db/models"
)

// Regression test for a bug where filterByYear always discarded every dated
// candidate: it sliced TMDB's ISO "YYYY-MM-DD" ReleaseDate as [6:] (correct
// only for the DD.MM.YYYY format used elsewhere, after FixDate), so Atoi
// always failed, year was silently 0, and Abs(0-torrYear)>1 was true for
// any real torrent year — removing every candidate that actually had a
// release date instead of just the wrong-year ones.
//
// filterByYear reads e.Year (set by tmdb.fixEntity from the raw ISO date),
// not e.ReleaseDate — cases below must set Year or they silently no-op as
// "dateless" and pass through regardless of tolerance (this bit the test
// itself once: it only set ReleaseDate, so every case fell through as
// dateless and passed without ever exercising the filter).
func TestFilterByYear(t *testing.T) {
	near := &models.Entity{ID: 1, Year: "1997"}
	far := &models.Entity{ID: 2, Year: "2026"}
	noDate := &models.Entity{ID: 3, Year: ""}

	got := filterByYear(true, []*models.Entity{near, far, noDate}, 1997)

	want := map[int64]bool{1: true, 3: true}
	if len(got) != len(want) {
		t.Fatalf("filterByYear() = %d candidates, want %d", len(got), len(want))
	}
	for _, e := range got {
		if !want[e.ID] {
			t.Errorf("filterByYear() kept unexpected candidate id=%d (Year=%q)", e.ID, e.Year)
		}
	}
}

// filterByYear falls back from ±1 to ±2 only when the strict pass keeps no
// dated candidate at all ("Мечта (1941)" — correct match is 1943, id=68271,
// but a strict ±1 filter dropped it, leaving only a dateless stub to win by
// default — see dev/rutracker.md).
func TestFilterByYearFallbackToTwo(t *testing.T) {
	twoOff := &models.Entity{ID: 1, Year: "1943"}
	noDate := &models.Entity{ID: 2, Year: ""}

	got := filterByYear(true, []*models.Entity{twoOff, noDate}, 1941)

	want := map[int64]bool{1: true, 2: true}
	if len(got) != len(want) {
		t.Fatalf("filterByYear() = %d candidates, want %d", len(got), len(want))
	}
}

// When a ±1 candidate already exists, a same-titled but unrelated film 2
// years off must never be let in to compete via the fallback — otherwise
// widening the tolerance risks picking the wrong film among same-titled
// releases from different years (see 18a3f09: user-flagged risk, e.g.
// "Месть" has ~19 unrelated TMDB movies 1960–2026, many exactly 2 years
// apart).
func TestFilterByYearNoFallbackWhenStrictHasMatch(t *testing.T) {
	oneOff := &models.Entity{ID: 1, Year: "1998"}
	twoOff := &models.Entity{ID: 2, Year: "1999"} // unrelated same-titled film, 2 years off

	got := filterByYear(true, []*models.Entity{oneOff, twoOff}, 1997)

	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("filterByYear() = %v, want only id=1 (strict ±1 match present, ±2 must not be offered)", got)
	}
}

// movieYearDist alone can't separate two unrelated films sharing both a
// title and a release year — e.g. TMDB ids 1368337 and 1698863, both titled
// "The Odyssey" (2026), released 12 days apart. movieDateDist is the
// tie-break FindTMDB falls back to once year-distance ties: exact release
// date vs. the torrent's own discovery date, since a torrent almost always
// surfaces at or shortly after its real release.
func TestMovieDateDist(t *testing.T) {
	nolan := &models.Entity{ID: 1368337, ReleaseDate: "15.07.2026", VoteCount: 3826}
	other := &models.Entity{ID: 1698863, ReleaseDate: "03.07.2026", VoteCount: 571}

	// Torrent discovered 2026-07-05 — 10 days after Nolan's release, 2 days
	// after the other film's — the closer one is the real match.
	torrCreate := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)

	if d := movieDateDist(nolan, torrCreate); d != 10 {
		t.Errorf("movieDateDist(nolan) = %d, want 10", d)
	}
	if d := movieDateDist(other, torrCreate); d != 2 {
		t.Errorf("movieDateDist(other) = %d, want 2", d)
	}

	// Missing torrent date or unparseable/empty release date must not crash
	// or falsely win a tie — sentinel keeps it indifferent to the outcome.
	if d := movieDateDist(nolan, time.Time{}); d < 1000 {
		t.Errorf("movieDateDist() with zero torrCreateDate = %d, want sentinel", d)
	}
	if d := movieDateDist(&models.Entity{ID: 3, ReleaseDate: ""}, torrCreate); d < 1000 {
		t.Errorf("movieDateDist() with empty ReleaseDate = %d, want sentinel", d)
	}

	// A zero-vote TMDB stub must never win a date tie-break, no matter how
	// close its (arbitrary, unconfirmed) date lands — verified live: a
	// torrent dated well after both real "Odyssey" releases let a bare stub
	// ("The Odyssey of N", 0 votes, no overview, release date 05.08.2026)
	// beat two real, popular same-titled films purely on date proximity.
	stub := &models.Entity{ID: 1743113, ReleaseDate: "05.08.2026", VoteCount: 0}
	farTorrCreate := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	if d := movieDateDist(stub, farTorrCreate); d < 1000 {
		t.Errorf("movieDateDist() for a zero-vote stub = %d, want sentinel", d)
	}
	// A real (voted) candidate still gets a usable distance even when far off.
	if d := movieDateDist(nolan, farTorrCreate); d != 59 {
		t.Errorf("movieDateDist(nolan, far date) = %d, want 59 (still a real film, just a weak match)", d)
	}
}
