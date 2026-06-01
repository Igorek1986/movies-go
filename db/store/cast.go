package store

import (
	"context"
	"log"
	"math/rand"
	"movies-api/db/models"
	"movies-api/db/postgres"
	"sort"
	"time"
)

const castTopN = 20

// CastRow is a single actor entry for a card.
type CastRow struct {
	PersonID    int64
	PersonName  string
	Character   string
	ProfilePath string
	Order       int
}

// PopularActor is used for catalog actor category rows.
type PopularActor struct {
	PersonID    int64
	PersonName  string
	ProfilePath string
}

// UpsertCast replaces the cast for a card with up to castTopN entries sorted by order.
func UpsertCast(ctx context.Context, cardID string, cast []models.CastEntry) {
	if len(cast) == 0 {
		return
	}
	entries := make([]models.CastEntry, len(cast))
	copy(entries, cast)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Order < entries[j].Order })
	if len(entries) > castTopN {
		entries = entries[:castTopN]
	}

	if _, err := postgres.Pool.Exec(ctx, `DELETE FROM media_card_cast WHERE card_id = $1`, cardID); err != nil {
		log.Printf("store: delete cast %s: %v", cardID, err)
		return
	}
	for _, c := range entries {
		var pp *string
		if c.ProfilePath != "" {
			pp = &c.ProfilePath
		}
		var ch *string
		if c.Character != "" {
			ch = &c.Character
		}
		if _, err := postgres.Pool.Exec(ctx, `
			INSERT INTO media_card_cast (card_id, person_id, person_name, character, profile_path, popularity, "order")
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (card_id, person_id) DO UPDATE SET
				person_name  = EXCLUDED.person_name,
				character    = EXCLUDED.character,
				profile_path = EXCLUDED.profile_path,
				popularity   = EXCLUDED.popularity,
				"order"      = EXCLUDED."order"`,
			cardID, c.ID, c.Name, ch, pp, c.Popularity, c.Order,
		); err != nil {
			log.Printf("store: insert cast %s person %d: %v", cardID, c.ID, err)
		}
	}
}

// GetCardCast returns stored cast for a card ordered by position.
func GetCardCast(ctx context.Context, cardID string) []CastRow {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := postgres.Pool.Query(ctx, `
		SELECT person_id, person_name, COALESCE(character,''), COALESCE(profile_path,''), "order"
		FROM media_card_cast
		WHERE card_id = $1
		ORDER BY "order"`, cardID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []CastRow
	for rows.Next() {
		var r CastRow
		if err := rows.Scan(&r.PersonID, &r.PersonName, &r.Character, &r.ProfilePath, &r.Order); err != nil {
			continue
		}
		result = append(result, r)
	}
	return result
}

// GetPopularActors returns the top limit actors by popularity.
// If ruOnly is true, restricts to actors from Russian-language cards.
// Returns a pool 5× larger than needed so the caller can pick randomly.
func GetPopularActors(ctx context.Context, limit int, ruOnly bool) []PopularActor {
	if limit <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pool := limit * 5
	if pool < 10 {
		pool = 10
	}

	var q string
	if ruOnly {
		q = `
			SELECT mc.person_id, mc.person_name, COALESCE(mc.profile_path,'')
			FROM media_card_cast mc
			JOIN media_cards m ON mc.card_id = m.card_id
			WHERE m.original_language = 'ru'
			GROUP BY mc.person_id, mc.person_name, mc.profile_path
			ORDER BY MAX(mc.popularity) DESC
			LIMIT $1`
	} else {
		q = `
			SELECT person_id, person_name, COALESCE(profile_path,'')
			FROM media_card_cast
			GROUP BY person_id, person_name, profile_path
			ORDER BY MAX(popularity) DESC
			LIMIT $1`
	}

	rows, err := postgres.Pool.Query(ctx, q, pool)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []PopularActor
	for rows.Next() {
		var a PopularActor
		if err := rows.Scan(&a.PersonID, &a.PersonName, &a.ProfilePath); err != nil {
			continue
		}
		result = append(result, a)
	}
	return result
}

// PickRandomActors picks n random actors from a pool (Fisher-Yates on a copy).
func PickRandomActors(pool []PopularActor, n int) []PopularActor {
	if n <= 0 || len(pool) == 0 {
		return nil
	}
	p := make([]PopularActor, len(pool))
	copy(p, pool)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := len(p) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		p[i], p[j] = p[j], p[i]
	}
	if n > len(p) {
		n = len(p)
	}
	return p[:n]
}

// ListActorCategory returns a paginated list of cards featuring the given actor.
func ListActorCategory(ctx context.Context, personID int64, page, perPage int) ([]MediaRow, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	offset := (page - 1) * perPage
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var total int
	if err := postgres.Pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM media_cards m
		JOIN media_card_cast mc ON m.card_id = mc.card_id
		WHERE mc.person_id = $1
		  AND m.poster_path IS NOT NULL AND m.poster_path <> ''`,
		personID).Scan(&total); err != nil {
		return nil, 0
	}

	qrows, err := postgres.Pool.Query(ctx, `
		SELECT m.tmdb_id, m.media_type, m.title, m.original_title,
			m.overview, m.poster_path, m.backdrop_path,
			m.release_date::text, m.first_air_date::text, m.last_air_date::text,
			m.vote_average, m.vote_count, m.original_language, m.adult, m.status,
			m.number_of_seasons, m.seasons, m.last_ep_season, m.last_ep_number, m.updated_at,
			m.best_video_quality, m.latest_torrent_date,
			m.certification_ru, m.certification_us
		FROM media_cards m
		JOIN media_card_cast mc ON m.card_id = mc.card_id
		WHERE mc.person_id = $1
		  AND m.poster_path IS NOT NULL AND m.poster_path <> ''
		ORDER BY m.vote_average DESC NULLS LAST, m.vote_count DESC NULLS LAST
		LIMIT $2 OFFSET $3`,
		personID, perPage, offset)
	if err != nil {
		return nil, 0
	}
	defer qrows.Close()

	var rows []MediaRow
	for qrows.Next() {
		var r MediaRow
		if err := qrows.Scan(
			&r.TmdbID, &r.MediaType, &r.Title, &r.OriginalTitle,
			&r.Overview, &r.PosterPath, &r.BackdropPath,
			&r.ReleaseDate, &r.FirstAirDate, &r.LastAirDate,
			&r.VoteAverage, &r.VoteCount, &r.OriginalLanguage, &r.Adult, &r.Status,
			&r.NumberOfSeasons, &r.Seasons, &r.LastEpSeason, &r.LastEpNumber, &r.UpdatedAt,
			&r.VideoQuality, &r.LatestTorrentDate,
			&r.CertificationRU, &r.CertificationUS,
		); err != nil {
			log.Printf("store: scan actor row: %v", err)
			continue
		}
		rows = append(rows, r)
	}
	return rows, total
}

// ─── Crew (directors) ─────────────────────────────────────────────────────────

// UpsertCrew saves directors for a card, replacing any existing entries.
func UpsertCrew(ctx context.Context, cardID string, crew []models.CrewEntry) {
	var directors []models.CrewEntry
	for _, c := range crew {
		if c.Job == "Director" {
			directors = append(directors, c)
		}
	}
	if len(directors) == 0 {
		return
	}
	if _, err := postgres.Pool.Exec(ctx,
		`DELETE FROM media_card_crew WHERE card_id = $1`, cardID); err != nil {
		log.Printf("store: delete crew %s: %v", cardID, err)
		return
	}
	for _, d := range directors {
		var pp *string
		if d.ProfilePath != "" {
			pp = &d.ProfilePath
		}
		if _, err := postgres.Pool.Exec(ctx, `
			INSERT INTO media_card_crew (card_id, person_id, person_name, profile_path, job, popularity)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (card_id, person_id, job) DO UPDATE SET
				person_name  = EXCLUDED.person_name,
				profile_path = EXCLUDED.profile_path,
				popularity   = EXCLUDED.popularity`,
			cardID, d.ID, d.Name, pp, d.Job, d.Popularity,
		); err != nil {
			log.Printf("store: insert crew %s person %d: %v", cardID, d.ID, err)
		}
	}
}

// GetPopularDirectors returns top directors by popularity from the DB.
func GetPopularDirectors(ctx context.Context, limit int) []PopularActor {
	if limit <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool := limit * 5
	if pool < 10 {
		pool = 10
	}
	rows, err := postgres.Pool.Query(ctx, `
		SELECT person_id, person_name, COALESCE(profile_path,'')
		FROM media_card_crew
		WHERE job = 'Director'
		GROUP BY person_id, person_name, profile_path
		ORDER BY MAX(popularity) DESC
		LIMIT $1`, pool)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []PopularActor
	for rows.Next() {
		var a PopularActor
		if err := rows.Scan(&a.PersonID, &a.PersonName, &a.ProfilePath); err != nil {
			continue
		}
		result = append(result, a)
	}
	return result
}

// ListDirectorCategory returns paginated cards directed by the given person.
func ListDirectorCategory(ctx context.Context, personID int64, page, perPage int) ([]MediaRow, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	offset := (page - 1) * perPage
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var total int
	if err := postgres.Pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM media_cards m
		JOIN media_card_crew mc ON m.card_id = mc.card_id
		WHERE mc.person_id = $1 AND mc.job = 'Director'
		  AND m.poster_path IS NOT NULL AND m.poster_path <> ''`,
		personID).Scan(&total); err != nil {
		return nil, 0
	}

	qrows, err := postgres.Pool.Query(ctx, `
		SELECT m.tmdb_id, m.media_type, m.title, m.original_title,
			m.overview, m.poster_path, m.backdrop_path,
			m.release_date::text, m.first_air_date::text, m.last_air_date::text,
			m.vote_average, m.vote_count, m.original_language, m.adult, m.status,
			m.number_of_seasons, m.seasons, m.last_ep_season, m.last_ep_number, m.updated_at,
			m.best_video_quality, m.latest_torrent_date,
			m.certification_ru, m.certification_us
		FROM media_cards m
		JOIN media_card_crew mc ON m.card_id = mc.card_id
		WHERE mc.person_id = $1 AND mc.job = 'Director'
		  AND m.poster_path IS NOT NULL AND m.poster_path <> ''
		ORDER BY m.vote_average DESC NULLS LAST, m.vote_count DESC NULLS LAST
		LIMIT $2 OFFSET $3`,
		personID, perPage, offset)
	if err != nil {
		return nil, 0
	}
	defer qrows.Close()

	var rows []MediaRow
	for qrows.Next() {
		var r MediaRow
		if err := qrows.Scan(
			&r.TmdbID, &r.MediaType, &r.Title, &r.OriginalTitle,
			&r.Overview, &r.PosterPath, &r.BackdropPath,
			&r.ReleaseDate, &r.FirstAirDate, &r.LastAirDate,
			&r.VoteAverage, &r.VoteCount, &r.OriginalLanguage, &r.Adult, &r.Status,
			&r.NumberOfSeasons, &r.Seasons, &r.LastEpSeason, &r.LastEpNumber, &r.UpdatedAt,
			&r.VideoQuality, &r.LatestTorrentDate,
			&r.CertificationRU, &r.CertificationUS,
		); err != nil {
			log.Printf("store: scan director row: %v", err)
			continue
		}
		rows = append(rows, r)
	}
	return rows, total
}
