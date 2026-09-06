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
	return s
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
