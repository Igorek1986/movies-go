package releases

import (
	"fmt"
	"movies-api/db/models"
	"movies-api/db/store"
	"movies-api/movies/tmdb"
	"log"
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
	md.SetTorrent(t)
	store.UpsertMediaCard(md, t)
	cardID := fmt.Sprintf("%d_%s", md.ID, md.MediaType)
	store.CacheTorrent(t.Hash, cardID, t.Tracker)
	log.Printf("%s: enriched: %s", label, t.Title)
	return true
}
