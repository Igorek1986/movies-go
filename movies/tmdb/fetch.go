package tmdb

import (
	"movies-api/db/models"
	"strconv"
)

// FetchVideoDetails fetches fresh data from TMDB API, always bypassing the local DB cache.
// Used for periodic card refresh tasks.
func FetchVideoDetails(isMovie bool, id int64) *models.Entity {
	mediaType := "tv"
	if isMovie {
		mediaType = "movie"
	}
	ids := strconv.FormatInt(id, 10)
	endpoint := mediaType + "/" + ids

	appendKey := "content_ratings,keywords,credits"
	if isMovie {
		appendKey = "release_dates,keywords,credits"
	}
	var ent *models.Entity
	if err := readPageTmdb(endpoint, map[string]string{"language": "ru", "append_to_response": appendKey}, &ent); err != nil || ent == nil {
		return nil
	}
	fixEntity(ent)
	ent.Titles = alternativeTitles(isMovie, id)
	return ent
}
