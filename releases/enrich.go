package releases

import (
	"fmt"
	"lampa-api/db/models"
	"lampa-api/db/store"
	"log"
)

// Enrich searches TMDB for a torrent and upserts the media card if found.
// Returns true if enrichment succeeded.
func Enrich(label string, isMovie bool, t *models.TorrentDetails) bool {
	md := FindTMDBID(isMovie, t)
	if md == nil {
		md = FindTMDB(isMovie, t)
	}
	if md == nil {
		store.CacheTorrent(t.Hash, "")
		log.Printf("%s: not found in TMDB: %s", label, t.Title)
		return false
	}
	md.SetTorrent(t)
	store.UpsertMediaCard(md, t)
	cardID := fmt.Sprintf("%d_%s", md.ID, md.MediaType)
	store.CacheTorrent(t.Hash, cardID)
	log.Printf("%s: enriched: %s", label, t.Title)
	return true
}
