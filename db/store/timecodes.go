package store

import (
	"context"
	"encoding/json"
	"fmt"
	"lampa-api/db/models"
	"lampa-api/db/postgres"
	"log"
	"strings"
	"time"
)

// ─── Role limits ──────────────────────────────────────────────────────────────

type RoleLimits struct {
	MaxDevices    int // 0 = unlimited
	MaxProfiles   int
	MaxTimecodes  int
	MaxFavorite   int // per category
}

func LimitsFor(role string) RoleLimits {
	switch role {
	case "premium":
		return RoleLimits{MaxDevices: 8, MaxProfiles: 5, MaxTimecodes: 1000, MaxFavorite: 500}
	case "super":
		return RoleLimits{MaxDevices: 0, MaxProfiles: 0, MaxTimecodes: 0, MaxFavorite: 0}
	default: // simple
		return RoleLimits{MaxDevices: 3, MaxProfiles: 1, MaxTimecodes: 200, MaxFavorite: 100}
	}
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
		pct := parsePercent(r.Data)
		var countedAt *string
		if pct >= 90 {
			countedAt = &today
		}

		_, err := postgres.Pool.Exec(ctx, `
			INSERT INTO timecodes (device_id, lampa_profile_id, card_id, item, data, counted_at, view_count)
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
	limit := LimitsFor(role).MaxTimecodes
	if limit == 0 {
		return
	}
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`DELETE FROM timecodes
		 WHERE id IN (
			SELECT id FROM timecodes
			WHERE device_id = $1 AND lampa_profile_id = $2
			ORDER BY updated_at DESC
			OFFSET $3
		 )`,
		deviceID, profileID, limit,
	)
}

// DeleteTimecode deletes a single timecode.
func DeleteTimecode(ctx context.Context, deviceID int64, profileID, cardID, item string) {
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`DELETE FROM timecodes
		 WHERE device_id=$1 AND lampa_profile_id=$2 AND card_id=$3 AND item=$4`,
		deviceID, profileID, cardID, item,
	)
}

// ExportTimecodes returns {card_id: {item: data_json}} — Lampac-compatible format.
func ExportTimecodes(ctx context.Context, deviceID int64, profileID string) map[string]map[string]string {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT card_id, item, data FROM timecodes
		 WHERE device_id=$1 AND lampa_profile_id=$2`,
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
	LastWatched   string  `json:"last_watched"`
	MaxPercent    float64 `json:"max_percent"`
	Progress      float64 `json:"progress"`
	IsComplete    bool    `json:"is_complete"`
}

func GetWatchHistory(ctx context.Context, deviceID int64, profileID string, page, limit int) ([]HistoryEntry, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	rows, err := postgres.Pool.Query(ctx, `
		SELECT t.card_id,
		       MAX(t.updated_at) AS last_watched,
		       MAX((t.data::jsonb->>'percent')::float) AS max_pct
		FROM timecodes t
		WHERE t.device_id = $1 AND t.lampa_profile_id = $2
		  AND t.card_id ~ '^[0-9]+_(movie|tv)$'
		GROUP BY t.card_id
		ORDER BY last_watched DESC`,
		deviceID, profileID,
	)
	if err != nil {
		return nil, 0
	}
	defer rows.Close()

	type agg struct {
		lastWatched time.Time
		maxPct      float64
	}
	byCard := map[string]agg{}
	var order []string
	for rows.Next() {
		var cardID string
		var lastWatched time.Time
		var maxPct float64
		if err := rows.Scan(&cardID, &lastWatched, &maxPct); err == nil {
			byCard[cardID] = agg{lastWatched, maxPct}
			order = append(order, cardID)
		}
	}
	rows.Close()

	if len(order) == 0 {
		return nil, 0
	}

	// Fetch media cards for the full set.
	placeholders := make([]string, len(order))
	args := make([]any, len(order))
	for i, cid := range order {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = cid
	}
	mcRows, err := postgres.Pool.Query(ctx,
		fmt.Sprintf(`SELECT card_id, tmdb_id, media_type, title, original_title,
			poster_path, release_date, first_air_date
			FROM media_cards WHERE card_id IN (%s)`, strings.Join(placeholders, ",")),
		args...,
	)

	type mc struct {
		cardID, mediaType, title, origTitle, poster string
		tmdbID                                       int64
		releaseDate, firstAirDate                    *string
	}
	cardMap := map[string]mc{}
	if err == nil {
		for mcRows.Next() {
			var m mc
			mcRows.Scan(&m.cardID, &m.tmdbID, &m.mediaType, &m.title, &m.origTitle, &m.poster, &m.releaseDate, &m.firstAirDate) //nolint:errcheck
			cardMap[m.cardID] = m
		}
		mcRows.Close()
	}

	total := len(order)
	start := (page - 1) * limit
	if start >= total {
		return nil, total
	}
	pageCards := order[start:]
	if len(pageCards) > limit {
		pageCards = pageCards[:limit]
	}

	var result []HistoryEntry
	for _, cardID := range pageCards {
		a := byCard[cardID]
		m := cardMap[cardID]
		year := ""
		if m.releaseDate != nil && len(*m.releaseDate) >= 4 {
			year = (*m.releaseDate)[:4]
		} else if m.firstAirDate != nil && len(*m.firstAirDate) >= 4 {
			year = (*m.firstAirDate)[:4]
		}
		result = append(result, HistoryEntry{
			CardID:        cardID,
			TmdbID:        m.tmdbID,
			MediaType:     m.mediaType,
			Title:         m.title,
			OriginalTitle: m.origTitle,
			PosterPath:    m.poster,
			Year:          year,
			LastWatched:   a.lastWatched.Format(time.RFC3339),
			MaxPercent:    a.maxPct,
			Progress:      a.maxPct,
			IsComplete:    a.maxPct >= 90,
		})
	}
	return result, total
}

// ─── Profiles ─────────────────────────────────────────────────────────────────

type ProfileInfo struct {
	ProfileID      string         `json:"profile_id"`
	Name           string         `json:"name"`
	Icon           string         `json:"icon"`
	Child          bool           `json:"child"`
	Params         map[string]any `json:"params"`
	TimecodesCount int            `json:"timecodes_count"`
}

func ListProfiles(ctx context.Context, deviceID int64) []ProfileInfo {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT lp.lampa_profile_id, lp.name, COALESCE(lp.icon,''), lp.child, lp.params,
		       COUNT(t.id) AS tc_count
		FROM lampa_profiles lp
		LEFT JOIN timecodes t ON t.device_id = lp.device_id
		                      AND t.lampa_profile_id = lp.lampa_profile_id
		WHERE lp.device_id = $1
		GROUP BY lp.lampa_profile_id, lp.name, lp.icon, lp.child, lp.params
		ORDER BY lp.id`,
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
		if err := rows.Scan(&p.ProfileID, &p.Name, &p.Icon, &p.Child, &paramsRaw, &p.TimecodesCount); err == nil {
			json.Unmarshal(paramsRaw, &p.Params) //nolint:errcheck
			if p.Params == nil {
				p.Params = map[string]any{}
			}
			result = append(result, p)
		}
	}
	return result
}

func CountProfiles(ctx context.Context, deviceID int64) int {
	var n int
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM lampa_profiles WHERE device_id=$1`, deviceID).Scan(&n) //nolint:errcheck
	return n
}

func CreateProfile(ctx context.Context, deviceID int64, profileID, name, icon string) (*models.LampaProfile, error) {
	lp := &models.LampaProfile{
		DeviceID:       deviceID,
		LampaProfileID: profileID,
		Name:           name,
		Icon:           icon,
		Params:         "{}",
	}
	err := postgres.Pool.QueryRow(ctx, `
		INSERT INTO lampa_profiles (device_id, lampa_profile_id, name, icon)
		VALUES ($1, $2, $3, NULLIF($4,''))
		RETURNING id`,
		deviceID, profileID, name, icon,
	).Scan(&lp.ID)
	if err != nil {
		return nil, err
	}
	// migrate default-profile timecodes
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`UPDATE timecodes SET lampa_profile_id=$1
		 WHERE device_id=$2 AND lampa_profile_id=''`,
		profileID, deviceID,
	)
	return lp, nil
}

func UpdateProfile(ctx context.Context, deviceID int64, profileID string, name, icon *string, child *bool, params map[string]any) error {
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
		fmt.Sprintf("UPDATE lampa_profiles SET %s WHERE device_id=$%d AND lampa_profile_id=$%d",
			strings.Join(sets, ","), n, n+1),
		args...,
	)
	return err
}

func DeleteProfile(ctx context.Context, deviceID int64, profileID string) error {
	_, err := postgres.Pool.Exec(ctx,
		`DELETE FROM lampa_profiles WHERE device_id=$1 AND lampa_profile_id=$2`,
		deviceID, profileID,
	)
	return err
}

// ─── Favorite ─────────────────────────────────────────────────────────────────

func GetFavorite(ctx context.Context, deviceID int64, profileID string) any {
	var raw *string
	err := postgres.Pool.QueryRow(ctx,
		`SELECT favorite FROM lampa_profiles WHERE device_id=$1 AND lampa_profile_id=$2`,
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
		INSERT INTO lampa_profiles (device_id, lampa_profile_id, name, favorite)
		VALUES ($1, $2, '', $3)
		ON CONFLICT ON CONSTRAINT uq_lampa_profile
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
		`INSERT INTO lampa_profiles (device_id, lampa_profile_id, name)
		 VALUES ($1, $2, $3)
		 ON CONFLICT ON CONSTRAINT uq_lampa_profile
		 DO UPDATE SET name = EXCLUDED.name`,
		deviceID, profileID, name,
	)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func parsePercent(dataJSON string) float64 {
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
