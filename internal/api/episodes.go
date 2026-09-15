package api

import (
	"context"
	"encoding/json"
	"log"
	"movies-api/db/postgres"
	"movies-api/db/store"
	"movies-api/internal/externalsources"
	"movies-api/internal/myshows"
	"movies-api/movies/tmdb"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// GET /api/episodes?card_id=&device_id=&profile_id=&include_specials=0
func handleEpisodes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cardID := q.Get("card_id")
	deviceID, _ := strconv.ParseInt(q.Get("device_id"), 10, 64)
	profileID := q.Get("profile_id")
	includeSpecials := q.Get("include_specials") == "1"

	if cardID == "" || !cardIDRe.MatchString(cardID) {
		JSON(w, http.StatusOK, map[string]any{"episodes": []any{}})
		return
	}

	// Verify device ownership to protect timecodes; skip if no session/device
	if deviceID != 0 {
		u := userFromCtx(r)
		if u != nil {
			var ownerID int64
			if err := postgres.Pool.QueryRow(r.Context(),
				`SELECT user_id FROM devices WHERE id=$1`, deviceID,
			).Scan(&ownerID); err != nil || ownerID != u.ID {
				deviceID = 0
			}
		} else {
			// No session — load timecodes by device token if present
			d := deviceFromRequest(r)
			if d == nil || d.ID != deviceID {
				deviceID = 0
			}
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
		go bgBackfillEpisodeInfo(mc.TmdbID, dbEps)
		JSON(w, http.StatusOK, buildFromTable(ctx, mc, dbEps, timecodeData, includeSpecials))
		return
	}

	// Fallback: TMDB seasons JSON
	JSON(w, http.StatusOK, buildFromTMDB(ctx, mc, timecodeData, includeSpecials))
}

// ─── check-ongoing rate limiter ───────────────────────────────────────────────

var (
	ongoingMu   sync.Mutex
	ongoingLast = map[int64]int{} // deviceID → YearDay last triggered
)

func ongoingAllowed(deviceID int64) bool {
	today := time.Now().YearDay()
	ongoingMu.Lock()
	defer ongoingMu.Unlock()
	if ongoingLast[deviceID] == today {
		return false
	}
	ongoingLast[deviceID] = today
	return true
}

// GET /api/check-ongoing?token= (device-token auth, fire-and-forget)
// Triggers a background episode sync for all stale TV cards the device has timecodes on.
func handleCheckOngoing(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	JSON(w, http.StatusOK, map[string]any{"ok": true})

	if !ongoingAllowed(d.ID) {
		return
	}

	deviceID := d.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		cards := store.GetStaleOngoingCards(ctx, deviceID)
		for i, mc := range cards {
			select {
			case <-ctx.Done():
				return
			default:
			}

			if err := myshows.SyncEpisodes(ctx, mc); err != nil {
				log.Printf("check-ongoing: sync %s: %v", mc.CardID, err)
			}

			if (i+1)%10 == 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		}
	}()
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

	// Find/set myshows_id unconditionally, independent of which source ends
	// up populating the episode list below — other features (progress /
	// "непросмотренные" tracking, see internal/api/myshows_cache.go) key off
	// media_cards.myshows_id on their own, regardless of episodes-table data.
	if mc.MyshowsID == nil {
		if sid := myshows.FindShow(ctx, mc, ""); sid != 0 {
			if err := store.SetMyshowsID(ctx, cardID, sid); err != nil {
				log.Printf("episodes: set myshows_id %s: %v", cardID, err)
			} else {
				mc.MyshowsID = &sid
			}
		}
	}

	// TVmaze as a first-time discovery source only — better specials/season
	// attribution than MyShows (verified empirically, see
	// dev/instance-sync.md), matches by imdb_id when we have one (see
	// internal/externalsources/tvmaze_episodes.go). Gated on "no rows yet"
	// rather than myshows.ShouldSync's ongoing-show re-sync logic below —
	// TVmaze has no equivalent incremental-refresh signal of its own here,
	// so this only ever runs once per show, same as the myshows_id lookup
	// above. UpsertEpisodes' fill-if-empty merge means MyShows below can
	// still layer in afterward without clobbering what TVmaze wrote.
	if len(store.GetEpisodes(ctx, mc.TmdbID)) == 0 {
		if rows := externalsources.FetchSeriesEpisodes(ctx, mc); len(rows) > 0 {
			if err := store.UpsertEpisodes(ctx, mc.TmdbID, rows); err != nil {
				log.Printf("episodes: tvmaze upsert %s: %v", cardID, err)
			}
		}
	}

	// MyShows: first-time discovery (if TVmaze above found nothing) AND the
	// only source of ongoing incremental re-sync as new episodes air
	// (ShouldSync checks next_ep_air_date vs episodes_synced_at) — this
	// fire-and-forget call from every /api/episodes view is what actually
	// keeps ongoing shows' episode lists current in production; there's no
	// separate always-on scheduled task for it (RunRefreshOngoingEpisodes is
	// admin/Telegram-button-triggered only, see internal/tasks/refresh_episodes.go).
	if mc.MyshowsID == nil || !myshows.ShouldSync(mc) {
		return
	}

	if err := myshows.SyncEpisodes(ctx, mc); err != nil {
		log.Printf("episodes: sync %s: %v", cardID, err)
	}
}

// bgBackfillEpisodeInfo fetches missing TMDB episode still_path/overview for
// every season that has at least one of either still NULL. Self-limiting:
// once a season is fully backfilled (a real value or the ” sentinel for
// both), this is a no-op on every later view — no separate "already synced"
// timestamp needed. Fire-and-forget from handleEpisodes, never on the
// response's critical path — readPageTmdb can block for up to ~50s on
// repeated TMDB errors (5 retries × 10s), which the page's own
// !episodesLoaded gate can't afford to wait on.
func bgBackfillEpisodeInfo(tmdbShowID int64, eps []store.EpisodeRow) {
	seasons := map[int16]bool{}
	for _, ep := range eps {
		if ep.StillPath == nil || ep.Overview == nil {
			seasons[ep.Season] = true
		}
	}
	if len(seasons) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for sn := range seasons {
		info := tmdb.GetSeasonEpisodeInfo(tmdbShowID, int(sn))
		if info == nil {
			continue // TMDB error — leave NULL, retry on a later view
		}
		if err := store.SetEpisodeSeasonInfo(ctx, tmdbShowID, sn, info); err != nil {
			log.Printf("episodes: backfill info s%d show=%d: %v", sn, tmdbShowID, err)
		}
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
		sql += ` AND profile_id=$3`
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
	Season         int16   `json:"season"`
	Episode        int16   `json:"episode"`
	Title          *string `json:"title,omitempty"`
	Hash           string  `json:"hash"`
	Watched        bool    `json:"watched"`
	Special        bool    `json:"special"`
	UserSpecial    bool    `json:"user_special"`    // user-marked (not MyShows is_special)
	CatalogSpecial bool    `json:"catalog_special"` // is_special from episodes table (real catalog special)
	Percent        float64 `json:"percent"`
	DurationSec    *int    `json:"duration_sec,omitempty"`
	AirDate        *string `json:"air_date,omitempty"`
	StillPath      *string `json:"still_path,omitempty"`
	Overview       *string `json:"overview,omitempty"`
}

func buildFromTable(ctx context.Context, mc *store.MediaCardEpInfo, eps []store.EpisodeRow, tc map[string]timecodeInfo, includeSpecials bool) map[string]any {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	threshold := float64(store.WatchedThreshold(ctx))
	var out []episodeOut

	for _, ep := range eps {
		if ep.Season == 0 && !includeSpecials {
			continue
		}
		if ep.AirDate == nil || ep.AirDate.After(today) {
			continue
		}
		td := tc[ep.Hash]
		// This device's own timecode (its own reported duration for its own
		// watched position) outranks catalog data — trustworthy since it's
		// the same viewer's own signal, not a shared cross-viewer one.
		durSec := td.durSec
		if durSec == nil {
			durSec = ep.DurationSec
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
		var stillPath *string
		if ep.StillPath != nil && *ep.StillPath != "" {
			stillPath = ep.StillPath
		}
		var overview *string
		if ep.Overview != nil && *ep.Overview != "" {
			overview = ep.Overview
		}
		out = append(out, episodeOut{
			Season:         ep.Season,
			Episode:        ep.Episode,
			Title:          ep.Title,
			Hash:           ep.Hash,
			Watched:        td.percent >= threshold || td.special,
			Special:        ep.IsSpecial || td.special,
			UserSpecial:    td.special,
			CatalogSpecial: ep.IsSpecial,
			Percent:        td.percent,
			DurationSec:    durSec,
			AirDate:        airStr,
			StillPath:      stillPath,
			Overview:       overview,
		})
	}
	if out == nil {
		out = []episodeOut{}
	}
	// "synced" — data comes from the episodes table itself (any source that
	// wrote there: TVmaze or MyShows, see bgRefreshEpisodes), as opposed to
	// the TMDB-seasons-JSON fallback below (buildFromTMDB, "source": "tmdb").
	// The frontend (CardDetailPage) treats this value as "the list is
	// settled, stop retrying" — keep it in sync with that check if renamed.
	return map[string]any{"episodes": out, "original_title": mc.OriginalTitle, "source": "synced"}
}

func buildFromTMDB(ctx context.Context, mc *store.MediaCardEpInfo, tc map[string]timecodeInfo, includeSpecials bool) map[string]any {
	threshold := float64(store.WatchedThreshold(ctx))
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
			var out []episodeOut
			for ep := 1; ep <= lastE; ep++ {
				h := myshows.EpisodeHash(lastS, ep, mc.OriginalTitle)
				td := tc[h]
				var durSec *int
				if mc.EpisodeRunTime != nil && *mc.EpisodeRunTime > 0 {
					v := *mc.EpisodeRunTime * 60
					durSec = &v
				}
				out = append(out, episodeOut{
					Season:      int16(lastS),
					Episode:     int16(ep),
					Hash:        h,
					Watched:     td.percent >= threshold || td.special,
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
			var durSec *int
			if mc.EpisodeRunTime != nil && *mc.EpisodeRunTime > 0 {
				v := *mc.EpisodeRunTime * 60
				durSec = &v
			}
			out = append(out, episodeOut{
				Season:      int16(snum),
				Episode:     int16(ep),
				Hash:        h,
				Watched:     td.percent >= threshold || td.special,
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
