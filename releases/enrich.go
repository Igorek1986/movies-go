package releases

import (
	"fmt"
	"log"
	"movies-api/db/models"
	"movies-api/db/store"
	"movies-api/movies/tmdb"
	"time"
)

// Enrich searches TMDB for a torrent and upserts the media card if found.
// Returns true if enrichment succeeded.
func Enrich(label string, isMovie bool, t *models.TorrentDetails) bool {
	md := FindTMDBID(isMovie, t)
	if md == nil {
		if found := FindTMDB(isMovie, t); found != nil {
			md = tmdb.GetVideoDetails(isMovie, found.ID)
		}
	}
	if md == nil {
		store.CacheTorrent(t.Hash, "", t.Tracker)
		log.Printf("%s: not found in TMDB: %s", label, t.Title)
		return false
	}
	// For movies: reject if torrent predates the official release date.
	// A torrent from before release almost certainly means a wrong TMDB match.
	// TV shows are skipped — first_air_date is the series debut, not the episode air date.
	if isMovie && !t.CreateDate.IsZero() && md.ReleaseDate != "" {
		if releaseDate, err := time.Parse("02.01.2006", md.ReleaseDate); err == nil {
			if t.CreateDate.Before(releaseDate) {
				store.CacheTorrent(t.Hash, "", t.Tracker)
				log.Printf("%s: skip (torrent %s before release %s): %s",
					label, t.CreateDate.Format("2006-01-02"), releaseDate.Format("2006-01-02"), t.Title)
				return false
			}
		}
	}

	md.SetTorrent(t)
	store.UpsertMediaCard(md, t)
	cardID := fmt.Sprintf("%d_%s", md.ID, md.MediaType)
	store.CacheTorrent(t.Hash, cardID, t.Tracker)
	log.Printf("%s: enriched: %s", label, t.Title)
	return true
}
