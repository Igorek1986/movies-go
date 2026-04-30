package api

import (
	"context"
	"encoding/json"
	"lampa-api/db/postgres"
	"lampa-api/db/store"
	"lampa-api/internal/myshows"
	"log"
	"net/http"
	"strconv"
	"time"
)

// GET /api/episodes?card_id=&device_id=&profile_id=&include_specials=0
func handleEpisodes(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	q := r.URL.Query()
	cardID := q.Get("card_id")
	deviceID, _ := strconv.ParseInt(q.Get("device_id"), 10, 64)
	profileID := q.Get("profile_id")
	includeSpecials := q.Get("include_specials") == "1"

	if cardID == "" || !cardIDRe.MatchString(cardID) {
		JSON(w, http.StatusOK, map[string]any{"episodes": []any{}})
		return
	}

	// Verify device belongs to this user (optional — if no device_id just skip timecodes)
	if deviceID != 0 {
		var ownerID int64
		if err := postgres.Pool.QueryRow(r.Context(),
			`SELECT user_id FROM devices WHERE id=$1`, deviceID,
		).Scan(&ownerID); err != nil || ownerID != u.ID {
			deviceID = 0 // silently ignore bad device
		}
	}

	ctx := r.Context()
	mc := store.GetMediaCardEpInfo(ctx, cardID)
	if mc == nil {
		JSON(w, http.StatusOK, map[string]any{"episodes": []any{}})
		return
	}

	// Load timecodes for this card
	timecodeData := loadCardTimecodes(ctx, deviceID, profileID, cardID)

	// Background sync if needed
	go bgRefreshEpisodes(cardID)

	// Try episodes table first
	dbEps := store.GetEpisodes(ctx, mc.TmdbID)
	if len(dbEps) > 0 {
		JSON(w, http.StatusOK, buildFromTable(mc, dbEps, timecodeData, includeSpecials))
		return
	}

	// Fallback: TMDB seasons JSON
	JSON(w, http.StatusOK, buildFromTMDB(ctx, mc, timecodeData, includeSpecials))
}

// GET /api/refresh-card-episodes?card_id= (device-token auth, fire-and-forget)
func handleRefreshCardEpisodes(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	cardID := r.URL.Query().Get("card_id")
	if cardID == "" || !cardIDRe.MatchString(cardID) {
		JSON(w, http.StatusOK, map[string]any{"ok": false})
		return
	}
	go bgRefreshEpisodes(cardID)
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ─── Background refresh ───────────────────────────────────────────────────────

func bgRefreshEpisodes(cardID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mc := store.GetMediaCardEpInfo(ctx, cardID)
	if mc == nil {
		return
	}

	// If no myshows_id yet — try to find it
	if mc.MyshowsID == nil {
		sid := myshows.FindShow(ctx, mc, "")
		if sid == 0 {
			return
		}
		if err := store.SetMyshowsID(ctx, cardID, sid); err != nil {
			log.Printf("episodes: set myshows_id %s: %v", cardID, err)
			return
		}
		mc.MyshowsID = &sid
	}

	if !myshows.ShouldSync(mc) {
		return
	}

	if err := myshows.SyncEpisodes(ctx, mc); err != nil {
		log.Printf("episodes: sync %s: %v", cardID, err)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

type timecodeInfo struct {
	percent float64
	special bool
	durSec  *int
}

func loadCardTimecodes(ctx context.Context, deviceID int64, profileID, cardID string) map[string]timecodeInfo {
	result := map[string]timecodeInfo{}
	if deviceID == 0 {
		return result
	}

	sql := `SELECT item, data FROM timecodes WHERE device_id=$1 AND card_id=$2`
	args := []any{deviceID, cardID}
	if profileID != "" {
		sql += ` AND lampa_profile_id=$3`
		args = append(args, profileID)
	}

	rows, err := postgres.Pool.Query(ctx, sql, args...)
	if err != nil {
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var item, dataStr string
		if err := rows.Scan(&item, &dataStr); err != nil {
			continue
		}
		var d map[string]any
		if json.Unmarshal([]byte(dataStr), &d) != nil {
			continue
		}
		pct, _ := d["percent"].(float64)
		special, _ := d["special"].(bool)
		var durSec *int
		if dur, ok := d["duration"].(float64); ok && dur > 0 {
			v := int(dur)
			durSec = &v
		}
		result[item] = timecodeInfo{percent: pct, special: special, durSec: durSec}
	}
	return result
}

type episodeOut struct {
	Season      int16   `json:"season"`
	Episode     int16   `json:"episode"`
	Title       *string `json:"title,omitempty"`
	Hash        string  `json:"hash"`
	Watched     bool    `json:"watched"`
	Special     bool    `json:"special"`
	UserSpecial bool    `json:"user_special"` // user-marked (not MyShows is_special)
	Percent     float64 `json:"percent"`
	DurationSec *int    `json:"duration_sec,omitempty"`
	AirDate     *string `json:"air_date,omitempty"`
}

func buildFromTable(mc *store.MediaCardEpInfo, eps []store.EpisodeRow, tc map[string]timecodeInfo, includeSpecials bool) map[string]any {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var out []episodeOut

	for _, ep := range eps {
		if ep.Season == 0 && !includeSpecials {
			continue
		}
		future := ep.AirDate != nil && ep.AirDate.After(today)
		if future {
			continue
		}
		td := tc[ep.Hash]
		durSec := ep.DurationSec
		if durSec == nil {
			durSec = td.durSec
			if durSec == nil && mc.EpisodeRunTime != nil && *mc.EpisodeRunTime > 0 {
				v := *mc.EpisodeRunTime * 60
				durSec = &v
			}
		}
		var airStr *string
		if ep.AirDate != nil {
			s := ep.AirDate.Format("2006-01-02")
			airStr = &s
		}
		out = append(out, episodeOut{
			Season:      ep.Season,
			Episode:     ep.Episode,
			Title:       ep.Title,
			Hash:        ep.Hash,
			Watched:     td.percent >= 90 || td.special,
			Special:     ep.IsSpecial || td.special,
			UserSpecial: td.special,
			Percent:     td.percent,
			DurationSec: durSec,
			AirDate:     airStr,
		})
	}
	if out == nil {
		out = []episodeOut{}
	}
	return map[string]any{"episodes": out, "original_title": mc.OriginalTitle, "source": "myshows"}
}

func buildFromTMDB(ctx context.Context, mc *store.MediaCardEpInfo, tc map[string]timecodeInfo, includeSpecials bool) map[string]any {
	var seasonsJSON []byte
	var lastEpSeason, lastEpNumber *int

	postgres.Pool.QueryRow(ctx, //nolint:errcheck
		`SELECT seasons, last_ep_season, last_ep_number FROM media_cards WHERE card_id=$1`, mc.CardID,
	).Scan(&seasonsJSON, &lastEpSeason, &lastEpNumber)

	// treat JSON null the same as SQL NULL
	if string(seasonsJSON) == "null" {
		seasonsJSON = nil
	}

	if seasonsJSON == nil {
		// seasons column is NULL — try to fetch from TMDB and persist
		if fetched := fetchAndPersistTVSeasons(ctx, mc.CardID, mc.TmdbID); fetched != nil {
			if b, err := json.Marshal(fetched); err == nil {
				seasonsJSON = b
			}
		}
	}
	if seasonsJSON == nil {
		// Last-resort fallback: build from last_ep_season / last_ep_number.
		// Works for single-season shows; multi-season shows only get the last season.
		if lastEpSeason != nil && lastEpNumber != nil && *lastEpSeason > 0 && *lastEpNumber > 0 {
			lastS := *lastEpSeason
			lastE := *lastEpNumber
			var durSec *int
			if mc.EpisodeRunTime != nil && *mc.EpisodeRunTime > 0 {
				v := *mc.EpisodeRunTime * 60
				durSec = &v
			}
			var out []episodeOut
			for ep := 1; ep <= lastE; ep++ {
				h := myshows.EpisodeHash(lastS, ep, mc.OriginalTitle)
				td := tc[h]
				out = append(out, episodeOut{
					Season:      int16(lastS),
					Episode:     int16(ep),
					Hash:        h,
					Watched:     td.percent >= 90 || td.special,
					Special:     td.special,
					UserSpecial: td.special,
					Percent:     td.percent,
					DurationSec: durSec,
				})
			}
			if len(out) > 0 {
				return map[string]any{"episodes": out, "original_title": mc.OriginalTitle, "source": "last_ep"}
			}
		}
		return map[string]any{"episodes": []any{}}
	}

	var seasons []struct {
		SeasonNumber int    `json:"season_number"`
		EpisodeCount int    `json:"episode_count"`
		AirDate      string `json:"air_date"`
	}
	if json.Unmarshal(seasonsJSON, &seasons) != nil {
		return map[string]any{"episodes": []any{}}
	}

	todayStr := time.Now().UTC().Format("2006-01-02")
	var durSec *int
	if mc.EpisodeRunTime != nil && *mc.EpisodeRunTime > 0 {
		v := *mc.EpisodeRunTime * 60
		durSec = &v
	}
	lastS := 0
	if lastEpSeason != nil {
		lastS = *lastEpSeason
	}
	lastE := 0
	if lastEpNumber != nil {
		lastE = *lastEpNumber
	}

	var out []episodeOut
	for _, s := range seasons {
		snum := s.SeasonNumber
		if snum == 0 {
			// Specials come from MyShows only — skip from TMDB fallback
			continue
		}
		var airedTo int
		if lastS > 0 {
			if snum < lastS {
				airedTo = s.EpisodeCount
			} else if snum == lastS {
				// lastE may be cumulative (e.g. long-running anime) — cap at episode_count
				airedTo = lastE
				if s.EpisodeCount > 0 && airedTo > s.EpisodeCount {
					airedTo = s.EpisodeCount
				}
			} else {
				continue
			}
		} else {
			if s.AirDate != "" && s.AirDate <= todayStr {
				airedTo = s.EpisodeCount
			} else {
				continue
			}
		}
		for ep := 1; ep <= airedTo; ep++ {
			h := myshows.EpisodeHash(snum, ep, mc.OriginalTitle)
			td := tc[h]
			out = append(out, episodeOut{
				Season:      int16(snum),
				Episode:     int16(ep),
				Hash:        h,
				Watched:     td.percent >= 90 || td.special,
				Special:     td.special,
				UserSpecial: td.special,
				Percent:     td.percent,
				DurationSec: durSec,
			})
		}
	}
	if out == nil {
		out = []episodeOut{}
	}
	return map[string]any{"episodes": out, "original_title": mc.OriginalTitle, "source": "tmdb"}
}

