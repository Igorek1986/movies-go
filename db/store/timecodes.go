package store

import (
	"context"
	"encoding/json"
	"fmt"
	"movies-api/db/models"
	"movies-api/db/postgres"
	"log"
	"math"
	"sort"
	"strings"
	"time"
)

// ─── Role limits ──────────────────────────────────────────────────────────────

type RoleLimits struct {
	MaxDevices   int // 0 = unlimited
	MaxProfiles  int
	MaxTimecodes int
	MaxFavorite  int // per category
}

// LimitsFor returns limits for the given role, reading from DB settings (with 30s cache).
// Falls back to hardcoded defaults if the DB is unavailable.
func LimitsFor(role string) RoleLimits {
	limitsCacheMu.RLock()
	cached := limitsCache
	fresh := time.Since(limitsCacheAt) < limitsCacheTTL
	limitsCacheMu.RUnlock()

	if cached != nil && fresh {
		if lim, ok := cached[role]; ok {
			return lim
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	loaded := loadLimitsFromDB(ctx)

	if loaded != nil {
		limitsCacheMu.Lock()
		limitsCache = loaded
		limitsCacheAt = time.Now()
		limitsCacheMu.Unlock()
		if lim, ok := loaded[role]; ok {
			return lim
		}
	}

	return LimitsForDefaults(role)
}

// ─── Timecode upsert ──────────────────────────────────────────────────────────

type TimecodeRow struct {
	CardID string
	Item   string
	Data   string // JSON: {time, duration, percent}
}

// UpsertTimecodes saves a batch of timecodes for a device+profile.
// Returns count of rows upserted.
func UpsertTimecodes(ctx context.Context, deviceID int64, profileID string, rows []TimecodeRow) int {
	if len(rows) == 0 {
		return 0
	}

	today := time.Now().Format("2006-01-02")
	saved := 0
	for _, r := range rows {
		pct := ParsePercent(r.Data)
		var countedAt *string
		if pct >= 90 {
			countedAt = &today
		}

		_, err := postgres.Pool.Exec(ctx, `
			INSERT INTO timecodes (device_id, profile_id, card_id, item, data, counted_at, view_count)
			VALUES ($1, $2, $3, $4, $5, $6, CASE WHEN $6::date IS NOT NULL THEN 1 ELSE 0 END)
			ON CONFLICT ON CONSTRAINT uq_timecode_unique DO UPDATE
			SET data       = EXCLUDED.data,
			    updated_at = now(),
			    counted_at = CASE
			        WHEN EXCLUDED.counted_at IS NOT NULL
			         AND (timecodes.counted_at IS NULL OR timecodes.counted_at < EXCLUDED.counted_at::date)
			        THEN EXCLUDED.counted_at::date
			        ELSE timecodes.counted_at
			    END,
			    view_count = CASE
			        WHEN EXCLUDED.counted_at IS NOT NULL
			         AND (timecodes.counted_at IS NULL OR timecodes.counted_at < EXCLUDED.counted_at::date)
			        THEN timecodes.view_count + 1
			        ELSE timecodes.view_count
			    END`,
			deviceID, profileID, r.CardID, r.Item, r.Data, countedAt,
		)
		if err != nil {
			log.Printf("store: upsert timecode %s/%s: %v", r.CardID, r.Item, err)
			continue
		}
		saved++
	}
	return saved
}

// TrimToLimit deletes oldest timecodes exceeding the role limit.
func TrimToLimit(ctx context.Context, deviceID int64, profileID string, role string) {
	TrimToLimitCount(ctx, deviceID, profileID, role)
}

// TrimToLimitCount deletes oldest timecodes exceeding the role limit and returns the count deleted.
func TrimToLimitCount(ctx context.Context, deviceID int64, profileID string, role string) int {
	limit := LimitsFor(role).MaxTimecodes
	if limit == 0 {
		return 0
	}
	tag, _ := postgres.Pool.Exec(ctx,
		`DELETE FROM timecodes
		 WHERE id IN (
			SELECT id FROM timecodes
			WHERE device_id = $1 AND profile_id = $2
			ORDER BY updated_at DESC
			OFFSET $3
		 )`,
		deviceID, profileID, limit,
	)
	return int(tag.RowsAffected())
}

// DeleteTimecode deletes a single timecode.
func DeleteTimecode(ctx context.Context, deviceID int64, profileID, cardID, item string) {
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`DELETE FROM timecodes
		 WHERE device_id=$1 AND profile_id=$2 AND card_id=$3 AND item=$4`,
		deviceID, profileID, cardID, item,
	)
}

// CardProgress holds aggregated watch progress for one card.
type CardProgress struct {
	MaxPercent   float64 `json:"max_percent"`
	WatchedItems int     `json:"watched_items"`
	TotalItems   int     `json:"total_items"`
	IsComplete   bool    `json:"is_complete"`
}

// GetCardProgress returns aggregated watch progress for device+profile+card.
func GetCardProgress(ctx context.Context, deviceID int64, profileID, cardID string) CardProgress {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT data FROM timecodes
		 WHERE device_id=$1 AND profile_id=$2 AND card_id=$3`,
		deviceID, profileID, cardID,
	)
	if err != nil {
		return CardProgress{}
	}
	defer rows.Close()

	var p CardProgress
	for rows.Next() {
		var data string
		if rows.Scan(&data) != nil {
			continue
		}
		pct := ParsePercent(data)
		p.TotalItems++
		if pct > p.MaxPercent {
			p.MaxPercent = pct
		}
		if pct >= 90 {
			p.WatchedItems++
		}
	}
	if p.TotalItems > 0 && p.WatchedItems == p.TotalItems {
		p.IsComplete = true
	}
	return p
}

// ─── Card timecodes (web UI) ──────────────────────────────────────────────────

type CardTimecodeRow struct {
	Item        string   `json:"item"`
	Percent     float64  `json:"percent"`
	TimeSec     float64  `json:"time"`
	DurationSec *float64 `json:"duration_sec"`
	ProfileID   string   `json:"profile_id"`
	Special     bool     `json:"special"`
}

// GetCardTimecodes returns all timecodes for device+card across all profiles.
func GetCardTimecodes(ctx context.Context, deviceID int64, cardID string) []CardTimecodeRow {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT item, data, profile_id FROM timecodes
		 WHERE device_id=$1 AND card_id=$2`,
		deviceID, cardID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []CardTimecodeRow
	for rows.Next() {
		var item, data, profileID string
		if rows.Scan(&item, &data, &profileID) != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(data), &m) != nil {
			continue
		}
		pct, _ := m["percent"].(float64)
		timeSec, _ := m["time"].(float64)
		special, _ := m["special"].(bool)
		var durSec *float64
		if d, ok := m["duration"].(float64); ok && d > 0 {
			durSec = &d
		}
		result = append(result, CardTimecodeRow{
			Item:        item,
			Percent:     pct,
			TimeSec:     timeSec,
			DurationSec: durSec,
			ProfileID:   profileID,
			Special:     special,
		})
	}
	return result
}

// SetCardTimecode upserts a timecode with given percent, preserving duration if known.
func SetCardTimecode(ctx context.Context, deviceID int64, profileID, cardID, item string, percent float64) error {
	percent = max(0, min(100, percent))

	// Read existing to preserve duration
	var existingData string
	postgres.Pool.QueryRow(ctx, //nolint:errcheck
		`SELECT data FROM timecodes
		 WHERE device_id=$1 AND profile_id=$2 AND card_id=$3 AND item=$4`,
		deviceID, profileID, cardID, item,
	).Scan(&existingData)

	var duration float64
	if existingData != "" {
		var m map[string]any
		if json.Unmarshal([]byte(existingData), &m) == nil {
			duration, _ = m["duration"].(float64)
		}
	}
	if duration == 0 {
		var runtimeMin int
		postgres.Pool.QueryRow(ctx, //nolint:errcheck
			`SELECT COALESCE(runtime, 0) FROM media_cards WHERE card_id = $1`, cardID,
		).Scan(&runtimeMin)
		if runtimeMin > 0 {
			duration = float64(runtimeMin) * 60
		}
	}

	timeSec := 0.0
	if duration > 0 {
		timeSec = duration * percent / 100
	}
	newData, _ := json.Marshal(map[string]any{
		"time":     timeSec,
		"duration": duration,
		"percent":  percent,
	})

	today := time.Now().Format("2006-01-02")
	var countedAt *string
	if percent >= 90 {
		countedAt = &today
	}

	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO timecodes (device_id, profile_id, card_id, item, data, counted_at, view_count)
		VALUES ($1, $2, $3, $4, $5, $6, CASE WHEN $6::date IS NOT NULL THEN 1 ELSE 0 END)
		ON CONFLICT ON CONSTRAINT uq_timecode_unique DO UPDATE
		SET data       = EXCLUDED.data,
		    updated_at = now(),
		    counted_at = COALESCE(timecodes.counted_at, EXCLUDED.counted_at),
		    view_count = timecodes.view_count + CASE WHEN EXCLUDED.counted_at IS NOT NULL AND timecodes.counted_at IS NULL THEN 1 ELSE 0 END`,
		deviceID, profileID, cardID, item, string(newData), countedAt,
	)
	return err
}

// SetCardTimecodeWatched upserts a timecode with percent=100 and a specific watched date.
// Use this for MyShows sync to preserve the original watched date ordering.
func SetCardTimecodeWatched(ctx context.Context, deviceID int64, profileID, cardID, item, watchedDate string) error {
	date := watchedDate
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	data, _ := json.Marshal(map[string]any{"time": 0, "duration": 0, "percent": 100})
	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO timecodes (device_id, profile_id, card_id, item, data, counted_at, view_count, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6::date, 1, $6::date)
		ON CONFLICT ON CONSTRAINT uq_timecode_unique DO UPDATE
		SET data       = EXCLUDED.data,
		    updated_at = $6::date,
		    counted_at = $6::date,
		    view_count = GREATEST(timecodes.view_count, 1)`,
		deviceID, profileID, cardID, item, string(data), date,
	)
	return err
}

// MarkSpecialTimecode saves a timecode with percent=100 and special=true.
func MarkSpecialTimecode(ctx context.Context, deviceID int64, profileID, cardID, item string) error {
	data, _ := json.Marshal(map[string]any{"time": 0, "duration": 0, "percent": 100, "special": true})
	today := time.Now().Format("2006-01-02")
	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO timecodes (device_id, profile_id, card_id, item, data, counted_at, view_count)
		VALUES ($1, $2, $3, $4, $5, $6, 1)
		ON CONFLICT ON CONSTRAINT uq_timecode_unique DO UPDATE
		SET data = EXCLUDED.data, updated_at = now(),
		    counted_at = COALESCE(timecodes.counted_at, EXCLUDED.counted_at)`,
		deviceID, profileID, cardID, item, string(data), today,
	)
	return err
}

// UnmarkSpecialTimecode resets a special-marked timecode to percent=0.
func UnmarkSpecialTimecode(ctx context.Context, deviceID int64, profileID, cardID, item string) error {
	data, _ := json.Marshal(map[string]any{"time": 0, "duration": 0, "percent": 0})
	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO timecodes (device_id, profile_id, card_id, item, data, counted_at, view_count)
		VALUES ($1, $2, $3, $4, $5, NULL, 0)
		ON CONFLICT ON CONSTRAINT uq_timecode_unique DO UPDATE
		SET data = EXCLUDED.data, updated_at = now(), counted_at = NULL`,
		deviceID, profileID, cardID, item, string(data),
	)
	return err
}

// DeleteCardTimecodes removes all timecodes for device+card.
func DeleteCardTimecodes(ctx context.Context, deviceID int64, cardID string) {
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`DELETE FROM timecodes WHERE device_id=$1 AND card_id=$2`,
		deviceID, cardID,
	)
}

// ExportTimecodes returns {card_id: {item: data_json}} — Lampac-compatible format.
func ExportTimecodes(ctx context.Context, deviceID int64, profileID string) map[string]map[string]string {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT card_id, item, data FROM timecodes
		 WHERE device_id=$1 AND profile_id=$2`,
		deviceID, profileID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := map[string]map[string]string{}
	for rows.Next() {
		var cardID, item, data string
		if err := rows.Scan(&cardID, &item, &data); err == nil {
			if result[cardID] == nil {
				result[cardID] = map[string]string{}
			}
			result[cardID][item] = data
		}
	}
	return result
}

// ─── Watch history ────────────────────────────────────────────────────────────

type HistoryEntry struct {
	CardID        string  `json:"card_id"`
	TmdbID        int64   `json:"tmdb_id"`
	MediaType     string  `json:"media_type"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	PosterPath    string  `json:"poster_path"`
	Year          string  `json:"year"`
	ReleaseDate   string  `json:"release_date"`
	LastWatched   string  `json:"last_watched"`
	MaxPercent    float64 `json:"max_percent"`
	Progress      float64 `json:"progress"`
	WatchedItems  int     `json:"watched_items"`
	TotalItems    int     `json:"total_items"`
	TotalEpisodes int     `json:"total_episodes"`
	IsComplete    bool    `json:"is_complete"`
}

type HistoryFilter struct {
	DeviceID   int64  // 0 = fall back to UserID
	ProfileID  string // "" = all profiles of device
	UserID     int64  // used when DeviceID == 0
	MediaType  string // "", "movie", "tv"
	InProgress bool   // only items with percent < 90
	Search     string // substring match on title / original_title
	Sort       string // "watched"(default),"release","progress_asc","progress_desc"
	Page       int
	PerPage    int
}

type HistoryCounts struct {
	All        int `json:"all"`
	Movies     int `json:"movies"`
	TV         int `json:"tv"`
	InProgress int `json:"in_progress"`
}

// GetHistoryFiltered loads watch history for a device+profile (or all user devices),
// computes tab counts on the full set, then applies filters, sort, and pagination.
func GetHistoryFiltered(ctx context.Context, f HistoryFilter) ([]HistoryEntry, HistoryCounts, int) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PerPage < 1 {
		f.PerPage = 24
	}

	const baseSelect = `
		SELECT t.card_id,
		       MAX(t.updated_at)                                                                                                    AS last_watched,
		       MAX((t.data::jsonb->>'percent')::float)                                                                              AS max_pct,
		       COUNT(*)                                                                                                             AS total_items,
		       COUNT(*) FILTER (WHERE (t.data::jsonb->>'percent')::float >= 90 OR (t.data::jsonb->>'special')::boolean IS TRUE)     AS watched_items,
		       COALESCE(
		         (SELECT COUNT(*)::int FROM episodes e2
		          WHERE e2.tmdb_show_id = (SELECT mc2.tmdb_id FROM media_cards mc2 WHERE mc2.card_id = t.card_id)
		            AND NOT e2.is_special
		          	AND e2.air_date IS NOT NULL AND e2.air_date <= CURRENT_DATE),
		         MAX(mc.number_of_episodes), 0)                                                                                     AS total_episodes,
		       COALESCE(MAX(mc.tmdb_id), 0),
		       COALESCE(MAX(mc.media_type), ''),
		       COALESCE(MAX(mc.title), ''),
		       COALESCE(MAX(mc.original_title), ''),
		       COALESCE(MAX(mc.poster_path), ''),
		       MAX(mc.release_date)::text,
		       MAX(mc.first_air_date)::text
		FROM timecodes t
		LEFT JOIN media_cards mc ON mc.card_id = t.card_id`

	var rows interface {
		Next() bool
		Scan(...any) error
		Close()
		Err() error
	}

	if f.DeviceID > 0 {
		profileCond := ""
		args := []any{f.DeviceID}
		if f.ProfileID != "" {
			profileCond = " AND t.profile_id = $2"
			args = append(args, f.ProfileID)
		}
		r, err := postgres.Pool.Query(ctx, baseSelect+`
		WHERE t.device_id = $1`+profileCond+`
		  AND t.card_id ~ '^[0-9]+_(movie|tv)$'
		GROUP BY t.card_id
		ORDER BY last_watched DESC`, args...)
		if err != nil {
			return nil, HistoryCounts{}, 0
		}
		rows = r
	} else {
		r, err := postgres.Pool.Query(ctx, baseSelect+`
		JOIN devices d ON d.id = t.device_id
		WHERE d.user_id = $1
		  AND t.card_id ~ '^[0-9]+_(movie|tv)$'
		GROUP BY t.card_id
		ORDER BY last_watched DESC`, f.UserID)
		if err != nil {
			return nil, HistoryCounts{}, 0
		}
		rows = r
	}
	defer rows.Close()

	var all []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		var lastWatched time.Time
		var releaseDate, firstAirDate *string
		if err := rows.Scan(
			&e.CardID, &lastWatched, &e.MaxPercent, &e.TotalItems, &e.WatchedItems, &e.TotalEpisodes,
			&e.TmdbID, &e.MediaType, &e.Title, &e.OriginalTitle,
			&e.PosterPath, &releaseDate, &firstAirDate,
		); err != nil {
			continue
		}
		e.LastWatched = lastWatched.Format(time.RFC3339)
		if e.MediaType == "tv" {
			denom := e.TotalEpisodes
			if denom == 0 {
				denom = e.TotalItems // fallback: episodes with any timecode
			}
			if denom > 0 {
				e.Progress = float64(e.WatchedItems) * 100 / float64(denom)
				e.IsComplete = e.WatchedItems >= denom
			}
		} else {
			e.Progress = e.MaxPercent
			e.IsComplete = e.MaxPercent >= 90
		}
		if releaseDate != nil && len(*releaseDate) >= 4 {
			e.ReleaseDate = *releaseDate
			e.Year = (*releaseDate)[:4]
		} else if firstAirDate != nil && len(*firstAirDate) >= 4 {
			e.ReleaseDate = *firstAirDate
			e.Year = (*firstAirDate)[:4]
		}
		all = append(all, e)
	}

	// Counts from full unfiltered set
	var counts HistoryCounts
	counts.All = len(all)
	for _, e := range all {
		if e.MediaType == "movie" {
			counts.Movies++
		}
		if e.MediaType == "tv" {
			counts.TV++
		}
		if !e.IsComplete {
			counts.InProgress++
		}
	}

	// Apply media_type / in_progress / search filters
	searchLow := strings.ToLower(f.Search)
	filtered := all[:0:len(all)]
	for _, e := range all {
		if f.MediaType != "" && e.MediaType != f.MediaType {
			continue
		}
		if f.InProgress && e.IsComplete {
			continue
		}
		if searchLow != "" &&
			!strings.Contains(strings.ToLower(e.Title), searchLow) &&
			!strings.Contains(strings.ToLower(e.OriginalTitle), searchLow) {
			continue
		}
		filtered = append(filtered, e)
	}

	// Sort (default order from SQL is last_watched DESC)
	switch f.Sort {
	case "release":
		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].ReleaseDate > filtered[j].ReleaseDate
		})
	case "progress_asc":
		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].MaxPercent < filtered[j].MaxPercent
		})
	case "progress_desc":
		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].MaxPercent > filtered[j].MaxPercent
		})
	}

	totalFiltered := len(filtered)
	start := (f.Page - 1) * f.PerPage
	if start >= totalFiltered {
		return nil, counts, totalFiltered
	}
	end := start + f.PerPage
	if end > totalFiltered {
		end = totalFiltered
	}
	return filtered[start:end], counts, totalFiltered
}

// ─── Profiles ─────────────────────────────────────────────────────────────────

type ProfileInfo struct {
	ProfileID      string         `json:"profile_id"`
	Name           string         `json:"name"`
	Icon           string         `json:"icon"`
	Child          bool           `json:"child"`
	ChildBirthYear *int           `json:"child_birth_year,omitempty"`
	Params         map[string]any `json:"params"`
	TimecodesCount int            `json:"timecodes_count"`
}

func ListProfiles(ctx context.Context, deviceID int64) []ProfileInfo {
	// UNION: named profiles + "orphan" profiles (have timecodes but no profiles entry)
	rows, err := postgres.Pool.Query(ctx, `
		SELECT lp.profile_id, lp.name, COALESCE(lp.icon,''), lp.child, lp.child_birth_year, lp.params::text,
		       COUNT(t.id) AS tc_count
		FROM profiles lp
		LEFT JOIN timecodes t ON t.device_id = lp.device_id
		                      AND t.profile_id = lp.profile_id
		WHERE lp.device_id = $1
		GROUP BY lp.id

		UNION ALL

		SELECT t.profile_id, t.profile_id, '', false, NULL::smallint, '{}',
		       COUNT(t.id) AS tc_count
		FROM timecodes t
		WHERE t.device_id = $1
		  AND t.profile_id NOT IN (
		      SELECT profile_id FROM profiles WHERE device_id = $1
		  )
		GROUP BY t.profile_id

		ORDER BY name`,
		deviceID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []ProfileInfo
	for rows.Next() {
		var p ProfileInfo
		var paramsRaw []byte
		err := rows.Scan(&p.ProfileID, &p.Name, &p.Icon, &p.Child, &p.ChildBirthYear, &paramsRaw, &p.TimecodesCount)
		if err != nil {
			continue
		}
		if len(paramsRaw) > 0 {
			json.Unmarshal(paramsRaw, &p.Params) //nolint:errcheck
		}
		if p.Params == nil {
			p.Params = map[string]any{}
		}
		result = append(result, p)
	}
	return result
}

func CountProfiles(ctx context.Context, deviceID int64) int {
	var n int
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM profiles WHERE device_id=$1`, deviceID).Scan(&n) //nolint:errcheck
	return n
}

func CreateProfile(ctx context.Context, deviceID int64, profileID, name, icon string) (*models.Profile, error) {
	lp := &models.Profile{
		DeviceID:       deviceID,
		ProfileID: profileID,
		Name:           name,
		Icon:           icon,
		Params:         "{}",
	}
	err := postgres.Pool.QueryRow(ctx, `
		INSERT INTO profiles (device_id, profile_id, name, icon)
		VALUES ($1, $2, $3, NULLIF($4,''))
		RETURNING id`,
		deviceID, profileID, name, icon,
	).Scan(&lp.ID)
	if err != nil {
		return nil, err
	}
	// migrate default-profile timecodes
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`UPDATE timecodes SET profile_id=$1
		 WHERE device_id=$2 AND profile_id=''`,
		profileID, deviceID,
	)
	return lp, nil
}

// GetProfileChildInfo returns child flag and birth year for a profile. Used for content filtering.
func GetProfileChildInfo(ctx context.Context, deviceID int64, profileID string) (child bool, birthYear *int) {
	postgres.Pool.QueryRow(ctx, //nolint:errcheck
		`SELECT child, child_birth_year FROM profiles WHERE device_id=$1 AND profile_id=$2`,
		deviceID, profileID,
	).Scan(&child, &birthYear)
	return
}

func UpdateProfile(ctx context.Context, deviceID int64, profileID string, name, icon *string, child *bool, childBirthYear *int, params map[string]any) error {
	sets := []string{}
	args := []any{}
	n := 1

	if name != nil {
		sets = append(sets, fmt.Sprintf("name=$%d", n))
		args = append(args, *name)
		n++
	}
	if icon != nil {
		sets = append(sets, fmt.Sprintf("icon=NULLIF($%d,'')", n))
		args = append(args, *icon)
		n++
	}
	if child != nil {
		sets = append(sets, fmt.Sprintf("child=$%d", n))
		args = append(args, *child)
		n++
	}
	if childBirthYear != nil {
		if *childBirthYear == 0 {
			sets = append(sets, "child_birth_year=NULL")
		} else {
			sets = append(sets, fmt.Sprintf("child_birth_year=$%d", n))
			args = append(args, *childBirthYear)
			n++
		}
	}
	if params != nil {
		b, _ := json.Marshal(params)
		sets = append(sets, fmt.Sprintf("params=$%d", n))
		args = append(args, string(b))
		n++
	}
	if len(sets) == 0 {
		return fmt.Errorf("nothing to update")
	}
	args = append(args, deviceID, profileID)
	_, err := postgres.Pool.Exec(ctx,
		fmt.Sprintf("UPDATE profiles SET %s WHERE device_id=$%d AND profile_id=$%d",
			strings.Join(sets, ","), n, n+1),
		args...,
	)
	return err
}

func DeleteProfile(ctx context.Context, deviceID int64, profileID string) error {
	_, err := postgres.Pool.Exec(ctx,
		`DELETE FROM timecodes WHERE device_id=$1 AND profile_id=$2`,
		deviceID, profileID,
	)
	if err != nil {
		return err
	}
	_, err = postgres.Pool.Exec(ctx,
		`DELETE FROM profiles WHERE device_id=$1 AND profile_id=$2`,
		deviceID, profileID,
	)
	return err
}

func MigrateDefaultTimecodes(ctx context.Context, deviceID int64, profileID string) error {
	_, err := postgres.Pool.Exec(ctx,
		`UPDATE timecodes SET profile_id=$2 WHERE device_id=$1 AND profile_id=''`,
		deviceID, profileID,
	)
	return err
}

func ClearProfileTimecodes(ctx context.Context, deviceID int64, profileID string) error {
	_, err := postgres.Pool.Exec(ctx,
		`DELETE FROM timecodes WHERE device_id=$1 AND profile_id=$2`,
		deviceID, profileID,
	)
	return err
}

// ─── Favorite ─────────────────────────────────────────────────────────────────

func GetFavorite(ctx context.Context, deviceID int64, profileID string) any {
	var raw *string
	err := postgres.Pool.QueryRow(ctx,
		`SELECT favorite FROM profiles WHERE device_id=$1 AND profile_id=$2`,
		deviceID, profileID,
	).Scan(&raw)
	if err != nil || raw == nil {
		return nil
	}
	var v any
	json.Unmarshal([]byte(*raw), &v) //nolint:errcheck
	return v
}

func SaveFavorite(ctx context.Context, deviceID int64, profileID string, favorite any) error {
	b, err := json.Marshal(favorite)
	if err != nil {
		return err
	}
	fav := string(b)

	_, err = postgres.Pool.Exec(ctx, `
		INSERT INTO profiles (device_id, profile_id, name, favorite)
		VALUES ($1, $2, '', $3)
		ON CONFLICT ON CONSTRAINT uq_profile
		DO UPDATE SET favorite = EXCLUDED.favorite`,
		deviceID, profileID, fav,
	)
	return err
}

// UpsertProfileName auto-saves profile name when it's sent with a timecode.
func UpsertProfileName(ctx context.Context, deviceID int64, profileID, name string) {
	if profileID == "" || name == "" {
		return
	}
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`INSERT INTO profiles (device_id, profile_id, name)
		 VALUES ($1, $2, $3)
		 ON CONFLICT ON CONSTRAINT uq_profile
		 DO UPDATE SET name = EXCLUDED.name`,
		deviceID, profileID, name,
	)
}

// MaybeUpdateRuntimeFromPlayer updates runtime metadata when the player reports a duration
// that is unknown (0) or differs by more than 5% from the stored value.
// For movies: updates runtime (minutes). For TV: updates episode_run_time (minutes).
func MaybeUpdateRuntimeFromPlayer(cardID, mediaType string, durationSec float64) {
	if durationSec < 60 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var storedMin int
	var col string
	if mediaType == "movie" {
		col = "runtime"
	} else {
		col = "episode_run_time"
	}
	if err := postgres.Pool.QueryRow(ctx,
		`SELECT COALESCE(`+col+`, 0) FROM media_cards WHERE card_id = $1`, cardID,
	).Scan(&storedMin); err != nil {
		return
	}

	storedSec := float64(storedMin) * 60
	if storedMin > 0 {
		if math.Abs(durationSec-storedSec)/storedSec <= 0.05 {
			return
		}
	}

	newMin := int(math.Round(durationSec / 60))
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`UPDATE media_cards SET `+col+` = $1, updated_at = now() WHERE card_id = $2`,
		newMin, cardID,
	)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func ParsePercent(dataJSON string) float64 {
	var m map[string]any
	if err := json.Unmarshal([]byte(dataJSON), &m); err != nil {
		return 0
	}
	switch v := m["percent"].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}
