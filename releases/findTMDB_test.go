package releases

import (
	"testing"

	"movies-api/db/models"
)

// Regression test for a bug where filterByYear always discarded every dated
// candidate: it sliced TMDB's ISO "YYYY-MM-DD" ReleaseDate as [6:] (correct
// only for the DD.MM.YYYY format used elsewhere, after FixDate), so Atoi
// always failed, year was silently 0, and Abs(0-torrYear)>1 was true for
// any real torrent year — removing every candidate that actually had a
// release date instead of just the wrong-year ones.
func TestFilterByYear(t *testing.T) {
	near := &models.Entity{ID: 1, ReleaseDate: "1997-11-14"}
	far := &models.Entity{ID: 2, ReleaseDate: "2026-07-15"}
	noDate := &models.Entity{ID: 3, ReleaseDate: ""}

	got := filterByYear(true, []*models.Entity{near, far, noDate}, 1997)

	want := map[int64]bool{1: true, 3: true}
	if len(got) != len(want) {
		t.Fatalf("filterByYear() = %d candidates, want %d", len(got), len(want))
	}
	for _, e := range got {
		if !want[e.ID] {
			t.Errorf("filterByYear() kept unexpected candidate id=%d (ReleaseDate=%q)", e.ID, e.ReleaseDate)
		}
	}
}
