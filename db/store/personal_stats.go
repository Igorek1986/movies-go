package store

import (
	"context"
	"log"
	"movies-api/db/postgres"
	"sort"
	"strings"
	"time"
)

// ─── Personal viewing stats ("Моя статистика") ─────────────────────────────
// Aggregates existing subjective-status/timecode data into a per-profile
// summary + a GitHub-style contribution calendar. Deliberately reuses the
// List* helpers in library.go/status.go instead of re-deriving the same
// "completed show" / "still watching" logic here (DRY).

type DayActivity struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type ProfileStats struct {
	MoviesWatched    int           `json:"movies_watched"`
	SeriesCompleted  int           `json:"series_completed"`
	SeriesWatching   int           `json:"series_watching"`
	PlannedMovies    int           `json:"planned_movies"`
	PlannedSeries    int           `json:"planned_series"`
	Stopped          int           `json:"stopped"`
	Favorites        int           `json:"favorites"`
	EpisodesWatched  int           `json:"episodes_watched"`
	WatchTimeMinutes int64         `json:"watch_time_minutes"`
	CurrentStreak    int           `json:"current_streak"`
	LongestStreak    int           `json:"longest_streak"`
	FavoriteWeekday  int           `json:"favorite_weekday"` // 0=Mon..6=Sun, -1 = no data
	Calendar         []DayActivity `json:"calendar"`
	TopGenres        []GenreCount  `json:"top_genres"`
	TopActors        []ActorCount  `json:"top_actors"`
	RemainingMinutes int64         `json:"remaining_minutes"`
}

// MovieWatchDetail is a movie's own timecode row summarized for the "Фильмы"
// detail list — a movie always has exactly one timecode row, so this is a
// direct read, not an aggregate.
type MovieWatchDetail struct {
	Percent   float64 `json:"percent"`
	ViewCount int     `json:"view_count"`
}

// SeriesProgress is a TV show's aired/watched episode counts for the
// "Сериалы" detail list.
type SeriesProgress struct {
	Watched int `json:"watched_episodes"`
	Total   int `json:"total_episodes"`
}

type GenreCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type ActorCount struct {
	PersonID    int64  `json:"person_id"`
	Name        string `json:"name"`
	ProfilePath string `json:"profile_path"`
	Count       int    `json:"count"`
}

func isMovieCardID(cardID string) bool {
	return strings.HasSuffix(cardID, "_movie")
}

// GetActivityCalendar returns per-day counts of items (movies/episodes) that
// crossed the watched threshold, for the last `days` days — the data source
// for the contribution heatmap.
func GetActivityCalendar(ctx context.Context, deviceID int64, profileID string, days int) []DayActivity {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT counted_at, COUNT(*) FROM timecodes
		 WHERE device_id = $1 AND profile_id = $2 AND counted_at IS NOT NULL
		   AND counted_at >= CURRENT_DATE - ($3::int * INTERVAL '1 day')
		 GROUP BY counted_at
		 ORDER BY counted_at`,
		deviceID, profileID, days)
	if err != nil {
		log.Printf("store: get activity calendar: %v", err)
		return nil
	}
	defer rows.Close()
	var out []DayActivity
	for rows.Next() {
		var d time.Time
		var c int
		if rows.Scan(&d, &c) == nil {
			out = append(out, DayActivity{Date: d.Format("2006-01-02"), Count: c})
		}
	}
	return out
}

// GetProfileStats computes the full personal stats summary for one device+profile.
func GetProfileStats(ctx context.Context, deviceID int64, profileID string) ProfileStats {
	var s ProfileStats

	for _, id := range ListCompletedCardIDs(ctx, deviceID, profileID, 0) {
		if isMovieCardID(id) {
			s.MoviesWatched++
		} else {
			s.SeriesCompleted++
		}
	}
	s.SeriesWatching = len(ListWatchingCardIDs(ctx, deviceID, profileID, 0))
	for _, id := range ListCardIDsByStatus(ctx, deviceID, profileID, StatusPlanned) {
		if isMovieCardID(id) {
			s.PlannedMovies++
		} else {
			s.PlannedSeries++
		}
	}
	s.Stopped = len(ListCardIDsByStatus(ctx, deviceID, profileID, StatusStopped))
	s.Favorites = len(ListFavoriteCardIDs(ctx, deviceID, profileID))

	if err := postgres.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM timecodes
		 WHERE device_id = $1 AND profile_id = $2 AND counted_at IS NOT NULL AND card_id LIKE '%_tv'`,
		deviceID, profileID).Scan(&s.EpisodesWatched); err != nil {
		log.Printf("store: count episodes watched: %v", err)
	}

	var epMinutes, movieMinutes int64
	if err := postgres.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(ep.duration_sec), 0) / 60 FROM timecodes tc
		JOIN episodes ep ON ep.hash = tc.item
		WHERE tc.device_id = $1 AND tc.profile_id = $2 AND tc.counted_at IS NOT NULL
		  AND tc.card_id LIKE '%_tv'`,
		deviceID, profileID).Scan(&epMinutes); err != nil {
		log.Printf("store: sum episode watch time: %v", err)
	}
	if err := postgres.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(mc.runtime), 0) FROM subjective_statuses ss
		JOIN media_cards mc ON mc.card_id = ss.card_id
		WHERE ss.device_id = $1 AND ss.profile_id = $2 AND ss.status = 'watched'`,
		deviceID, profileID).Scan(&movieMinutes); err != nil {
		log.Printf("store: sum movie watch time: %v", err)
	}
	s.WatchTimeMinutes = epMinutes + movieMinutes

	s.Calendar = GetActivityCalendar(ctx, deviceID, profileID, 371)
	s.CurrentStreak, s.LongestStreak, s.FavoriteWeekday = computeStreaks(s.Calendar)
	s.TopGenres = GetTopGenres(ctx, deviceID, profileID, 5)
	s.TopActors = GetTopActors(ctx, deviceID, profileID, 5)
	s.RemainingMinutes = GetRemainingWatchMinutes(ctx, deviceID, profileID)
	return s
}

// GetMovieWatchDetails batch-loads percent/view_count for a set of movie
// card_ids — each has exactly one timecode row (no per-episode item), so this
// is a plain lookup, not an aggregate.
func GetMovieWatchDetails(ctx context.Context, deviceID int64, profileID string, cardIDs []string) map[string]MovieWatchDetail {
	out := make(map[string]MovieWatchDetail, len(cardIDs))
	if len(cardIDs) == 0 {
		return out
	}
	rows, err := postgres.Pool.Query(ctx, `
		SELECT card_id, COALESCE((data::jsonb->>'percent')::numeric, 0), view_count
		FROM timecodes
		WHERE device_id = $1 AND profile_id = $2 AND card_id = ANY($3)`,
		deviceID, profileID, cardIDs)
	if err != nil {
		log.Printf("store: get movie watch details: %v", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var cardID string
		var d MovieWatchDetail
		if rows.Scan(&cardID, &d.Percent, &d.ViewCount) == nil {
			out[cardID] = d
		}
	}
	return out
}

// GetSeriesProgressBatch batch-loads aired/watched episode counts for a set
// of TV show card_ids — same LATERAL-join shape as UnwatchedTVShowProgress
// (see unwatched.go), but batched and without its "status = watching"
// restriction: the caller already picked which card_ids to ask about.
func GetSeriesProgressBatch(ctx context.Context, deviceID int64, profileID string, cardIDs []string) map[string]SeriesProgress {
	out := make(map[string]SeriesProgress, len(cardIDs))
	if len(cardIDs) == 0 {
		return out
	}
	cutoff := AiredCutoffDate(ctx)
	//nolint:gosec // cutoff comes from AiredCutoffDate (admin setting only), not user input
	rows, err := postgres.Pool.Query(ctx, `
		WITH watched_hashes AS (
			SELECT tc.item AS hash
			FROM timecodes tc
			WHERE tc.device_id = $1 AND tc.profile_id = $2 AND tc.card_id = ANY($4)
			  AND ((tc.data::jsonb->>'percent')::numeric >= $3
			       OR (tc.data::jsonb->>'special')::boolean IS TRUE)
		)
		SELECT mc.card_id,
		       COUNT(*) FILTER (WHERE e.air_date IS NOT NULL AND e.air_date <= `+cutoff+`) AS aired,
		       COUNT(*) FILTER (WHERE e.air_date IS NOT NULL AND e.air_date <= `+cutoff+`
		                          AND wh.hash IS NOT NULL) AS watched
		FROM media_cards mc
		JOIN episodes e ON e.tmdb_show_id = mc.tmdb_id AND NOT e.is_special
		LEFT JOIN watched_hashes wh ON wh.hash = e.hash
		WHERE mc.card_id = ANY($4) AND mc.media_type = 'tv'
		GROUP BY mc.card_id`,
		deviceID, profileID, WatchedThreshold(ctx), cardIDs)
	if err != nil {
		log.Printf("store: get series progress batch: %v", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var cardID string
		var p SeriesProgress
		if rows.Scan(&cardID, &p.Total, &p.Watched) == nil {
			out[cardID] = p
		}
	}
	return out
}

// GetRemainingWatchMinutes estimates catch-up time for shows the profile is
// actively "Смотрю" — sum of aired-but-unwatched episode durations (falling
// back to the show's episode_run_time when a specific episode's duration is
// unknown). Deliberately excludes "Буду смотреть" (not started yet).
func GetRemainingWatchMinutes(ctx context.Context, deviceID int64, profileID string) int64 {
	watchingIDs := ListWatchingCardIDs(ctx, deviceID, profileID, 0)
	if len(watchingIDs) == 0 {
		return 0
	}
	cutoff := AiredCutoffDate(ctx)
	//nolint:gosec // cutoff comes from AiredCutoffDate (admin setting only), not user input
	var minutes int64
	if err := postgres.Pool.QueryRow(ctx, `
		WITH watched_hashes AS (
			SELECT tc.item AS hash
			FROM timecodes tc
			WHERE tc.device_id = $1 AND tc.profile_id = $2 AND tc.card_id = ANY($4)
			  AND ((tc.data::jsonb->>'percent')::numeric >= $3
			       OR (tc.data::jsonb->>'special')::boolean IS TRUE)
		)
		SELECT COALESCE(SUM(COALESCE(e.duration_sec, mc.episode_run_time * 60, 0)), 0) / 60
		FROM media_cards mc
		JOIN episodes e ON e.tmdb_show_id = mc.tmdb_id AND NOT e.is_special
		LEFT JOIN watched_hashes wh ON wh.hash = e.hash
		WHERE mc.card_id = ANY($4)
		  AND e.air_date IS NOT NULL AND e.air_date <= `+cutoff+`
		  AND wh.hash IS NULL`,
		deviceID, profileID, WatchedThreshold(ctx), watchingIDs).Scan(&minutes); err != nil {
		log.Printf("store: get remaining watch minutes: %v", err)
	}
	return minutes
}

// watchedCardsCTE is shared by GetTopGenres/GetTopActors — cards the profile
// has actually watched something of (movie or at least one episode), one row
// per card_id regardless of how many episodes/rewatches contributed.
const watchedCardsCTE = `
	WITH watched_cards AS (
		SELECT DISTINCT card_id FROM timecodes
		WHERE device_id = $1 AND profile_id = $2 AND counted_at IS NOT NULL
	)`

// GetTopGenres ranks genres by number of distinct watched cards carrying them.
func GetTopGenres(ctx context.Context, deviceID int64, profileID string, limit int) []GenreCount {
	rows, err := postgres.Pool.Query(ctx, watchedCardsCTE+`
		SELECT g->>'name' AS name, COUNT(*) AS cnt
		FROM watched_cards wc
		JOIN media_cards mc ON mc.card_id = wc.card_id
		CROSS JOIN LATERAL json_array_elements(COALESCE(mc.genres::json, '[]'::json)) g
		WHERE mc.genres IS NOT NULL AND mc.genres::text <> 'null' AND g->>'name' IS NOT NULL
		GROUP BY 1
		ORDER BY cnt DESC
		LIMIT $3`,
		deviceID, profileID, limit)
	if err != nil {
		log.Printf("store: get top genres: %v", err)
		return nil
	}
	defer rows.Close()
	var out []GenreCount
	for rows.Next() {
		var g GenreCount
		if rows.Scan(&g.Name, &g.Count) == nil {
			out = append(out, g)
		}
	}
	return out
}

// GetTopActors ranks main-cast actors (billing order < 10) by number of
// distinct watched cards they appear in.
func GetTopActors(ctx context.Context, deviceID int64, profileID string, limit int) []ActorCount {
	rows, err := postgres.Pool.Query(ctx, watchedCardsCTE+`
		SELECT cc.person_id, MAX(cc.person_name), MAX(COALESCE(cc.profile_path, '')), COUNT(*) AS cnt
		FROM watched_cards wc
		JOIN media_card_cast cc ON cc.card_id = wc.card_id
		WHERE cc."order" < 10
		GROUP BY cc.person_id
		ORDER BY cnt DESC, MAX(cc.popularity) DESC
		LIMIT $3`,
		deviceID, profileID, limit)
	if err != nil {
		log.Printf("store: get top actors: %v", err)
		return nil
	}
	defer rows.Close()
	var out []ActorCount
	for rows.Next() {
		var a ActorCount
		if rows.Scan(&a.PersonID, &a.Name, &a.ProfilePath, &a.Count) == nil {
			out = append(out, a)
		}
	}
	return out
}

// computeStreaks derives current/longest day-streaks and the most active
// weekday from a calendar's day list — pure function, no DB access.
func computeStreaks(cal []DayActivity) (current, longest, favoriteWeekday int) {
	favoriteWeekday = -1
	if len(cal) == 0 {
		return 0, 0, -1
	}

	dates := make(map[string]bool, len(cal))
	var weekdayCount [7]int // Mon=0..Sun=6
	for _, d := range cal {
		dates[d.Date] = true
		if t, err := time.Parse("2006-01-02", d.Date); err == nil {
			wd := (int(t.Weekday()) + 6) % 7
			weekdayCount[wd] += d.Count
		}
	}
	best := 0
	for i, c := range weekdayCount {
		if c > best {
			best = c
			favoriteWeekday = i
		}
	}

	today := time.Now()
	todayKey := today.Format("2006-01-02")
	start := today
	if !dates[todayKey] {
		start = today.AddDate(0, 0, -1)
	}
	for d := start; dates[d.Format("2006-01-02")]; d = d.AddDate(0, 0, -1) {
		current++
	}

	sortedDates := make([]string, 0, len(dates))
	for d := range dates {
		sortedDates = append(sortedDates, d)
	}
	sort.Strings(sortedDates)
	var prev time.Time
	run := 0
	for i, ds := range sortedDates {
		t, err := time.Parse("2006-01-02", ds)
		if err != nil {
			continue
		}
		if i > 0 && t.Sub(prev).Hours() == 24 {
			run++
		} else {
			run = 1
		}
		if run > longest {
			longest = run
		}
		prev = t
	}
	return
}
