package parser

import (
	"context"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"movies-api/db/models"
	"movies-api/db/store"
	"movies-api/utils"
)

var (
	reTitleYearParen   = regexp.MustCompile(`\((\d{4})(?:-\d{4})?\)`)
	reTitleYearRange   = regexp.MustCompile(`^(\d{4})-\d{4}`)
	reTitleBrackets    = regexp.MustCompile(`\[.*?\]`)
	reTitleYearBracket = regexp.MustCompile(`\[(\d{4})(?:-\d{4})?[,\]\s]`)
	reSeasonPartHdr    = regexp.MustCompile(`(?i)^(Сезон|Серии|Series|Season)`)
)

// extractLeadingYear reports whether a " / "-separated title segment IS (or
// starts with) a year — bare "2025", or "2008-2009 США, комедия" — the
// classic 3rd-field format for rutor/kinozal titles without extra alt names.
func extractLeadingYear(p string) (int, bool) {
	if yr, err := strconv.Atoi(p); err == nil && yr >= 1900 && yr <= 2100 {
		return yr, true
	}
	if m := reTitleYearRange.FindStringSubmatch(p); m != nil {
		if yr, err := strconv.Atoi(m[1]); err == nil {
			return yr, true
		}
	}
	if len(p) > 4 && p[4] == ' ' {
		if yr, err := strconv.Atoi(p[:4]); err == nil && yr >= 1900 && yr <= 2100 {
			return yr, true
		}
	}
	return 0, false
}

// HasEpisodeBrackets reports whether title contains [...] before the year — indicates episode range.
func HasEpisodeBrackets(title string) bool {
	part := title
	if m := reTitleYearParen.FindStringIndex(title); m != nil {
		part = title[:m[0]]
	}
	return reTitleBrackets.MatchString(part)
}

// maskBracketSlashes swaps '/' for a lookalike character while inside
// "[...]" groups, so the top-level " / " split below doesn't shred a bracket
// like "[UKR, EN / UKR, EN Sub]" (language/sub tags on rutor's international
// releases) into two pieces with the closing "]" stranded in the next part
// — reTitleBrackets then fails to strip the orphaned half, and it leaks into
// d.Names. Safe because bracket content is discarded wholesale wherever it
// matters (name extraction, HasEpisodeBrackets) — nothing reads this
// placeholder back out.
func maskBracketSlashes(title string) string {
	depth := 0
	runes := []rune(title)
	for i, r := range runes {
		switch r {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case '/':
			if depth > 0 {
				runes[i] = '⁄' // U+2044 FRACTION SLASH
			}
		}
	}
	return string(runes)
}

// ParseTorrentTitle fills Name, Names, Year, VideoQuality, AudioQuality from a torrent title.
// Format: "RuName / EngName / year / ... / quality" or "RuName / EngName (year) quality".
func ParseTorrentTitle(d *models.TorrentDetails, title string) {
	title = strings.ReplaceAll(title, "&amp;", "&")
	title = maskBracketSlashes(title)
	parts := strings.Split(title, " / ")
	if len(parts) < 2 {
		// nnmclub anime: "Romaji | English | Русский [ТВ] [YYYY, Type, N эп.] Quality raw"
		// — no " / " at all, alternate names pipe-separated, year in brackets like
		// rutracker's format. Quality/raw trails the last "]", so split there first
		// to keep it out of the name/year search.
		if idx := strings.LastIndex(title, "]"); idx != -1 && strings.Contains(title, "|") {
			namePart := title[:idx+1]
			qual := strings.TrimSpace(title[idx+1:])
			d.VideoQuality = ParseVQuality(qual)
			d.AudioQuality = ParseAQuality(qual)
			pipeParts := strings.Split(namePart, "|")
			d.Name = strings.TrimSpace(reTitleBrackets.ReplaceAllString(pipeParts[0], ""))
			for _, p := range pipeParts[1:] {
				if clean := strings.TrimSpace(reTitleBrackets.ReplaceAllString(p, "")); clean != "" {
					d.Names = append(d.Names, clean)
				}
			}
			if m := reTitleYearBracket.FindStringSubmatch(namePart); m != nil {
				if yr, err := strconv.Atoi(m[1]); err == nil && yr >= 1900 && yr <= 2100 {
					d.Year = yr
				}
			}
			if d.Year == 0 {
				if m := reTitleYearParen.FindStringSubmatch(namePart); m != nil {
					d.Year, _ = strconv.Atoi(m[1])
				}
			}
			return
		}

		// "Name (YEAR) Quality..." format — common in rutor/nnmclub without "/" separator
		if m := reTitleYearParen.FindStringIndex(title); m != nil {
			d.Year, _ = strconv.Atoi(title[m[0]+1 : m[1]-1])
			d.Name = strings.TrimSpace(reTitleBrackets.ReplaceAllString(title[:m[0]], ""))
			qual := strings.TrimSpace(title[m[1]:])
			d.VideoQuality = ParseVQuality(qual)
			d.AudioQuality = ParseAQuality(qual)
		} else {
			d.Name = strings.TrimSpace(title)
		}
		return
	}

	d.Name = strings.TrimSpace(reTitleBrackets.ReplaceAllString(parts[0], ""))

	qualPart := parts[len(parts)-1]
	d.VideoQuality = ParseVQuality(qualPart)
	d.AudioQuality = ParseAQuality(qualPart)

	// Collect every alternate name in parts[1:] — rutracker/nnmclub titles can
	// list several ("RuName1 / RuName2 / EngName / Сезон: ...", not just one)
	// — until hitting a structural marker: a leading year (bare or "YYYY[-YYYY]
	// ..."), a season/episode header, or a fragment with an unbalanced "(" (the
	// split landed inside a "(Director1 / Director2)" list that itself
	// contains " / "). Missing the later names this way let e.g. "Эндшпиль"/
	// "Endgame" (the real TMDB title) get dropped in favor of an earlier,
	// wrong-language alt name that collided with an unrelated show sharing
	// its Russian translation — see dev/rutracker.md.
	// A 2-part title ("RuName / EngName (year) Quality...", common on nnmclub)
	// has no dedicated quality-only last field — parts[1] is qualPart AND the
	// only place an alt name could live, so the loop below (which always
	// excludes the last part) never runs at all and the alt name is lost
	// entirely. Extract it here the same way the len(parts)<2 branch above
	// does: only the text before "(year)" is the name, everything after
	// (quality) is discarded from it. Case: nnmclub "Любовный напиток номер 9
	// / Love Potion No. 9 (1992) BDRip [H.264/720p] [MVO]" — TMDB's Russian
	// title uses "№9" not "номер 9", so only the English alt name matches.
	if len(parts) == 2 {
		p := parts[1]
		if m := reTitleYearParen.FindStringIndex(p); m != nil {
			if d.Year == 0 {
				d.Year, _ = strconv.Atoi(p[m[0]+1 : m[1]-1])
			}
			if altName := strings.TrimSpace(reTitleBrackets.ReplaceAllString(p[:m[0]], "")); altName != "" {
				d.Names = append(d.Names, altName)
			}
		}
	}

	for i := 1; i < len(parts)-1; i++ {
		p := strings.TrimSpace(parts[i])
		if yr, ok := extractLeadingYear(p); ok {
			// A numeric title ("1917", "2012") looks exactly like a year
			// itself. If the NEXT field is also year-shaped, this one is
			// really the (numeric) alternate name and the actual year
			// follows — keep it as a name and let the next part settle it,
			// rather than locking in the wrong year from the title.
			if i+1 < len(parts)-1 {
				if _, nextIsYear := extractLeadingYear(strings.TrimSpace(parts[i+1])); nextIsYear {
					d.Names = append(d.Names, p)
					continue
				}
			}
			if d.Year == 0 {
				d.Year = yr
			}
			break
		}
		if reSeasonPartHdr.MatchString(p) {
			break
		}
		if idx := strings.Index(p, "("); idx != -1 && strings.Count(p, "(") > strings.Count(p, ")") {
			if prefix := strings.TrimSpace(p[:idx]); prefix != "" {
				d.Names = append(d.Names, prefix)
			}
			break
		}
		if m := reTitleYearParen.FindStringSubmatch(p); m != nil {
			if d.Year == 0 {
				d.Year, _ = strconv.Atoi(m[1])
			}
			p = strings.TrimSpace(reTitleYearParen.ReplaceAllString(p, ""))
		}
		if clean := strings.TrimSpace(reTitleBrackets.ReplaceAllString(p, "")); clean != "" {
			d.Names = append(d.Names, clean)
		}
	}

	// Final fallback: scan full title for (year)
	if d.Year == 0 {
		if m := reTitleYearParen.FindStringSubmatch(title); m != nil {
			d.Year, _ = strconv.Atoi(m[1])
		}
	}

	// rutracker embeds the year inside "[YYYY, Country, Genre, Format]" rather
	// than "(year)" or a dedicated " / year /" field — without it, TV matching
	// can't disambiguate same-name shows from different years (see FindTMDB's
	// tvYearDist) and silently picks the most popular one.
	if d.Year == 0 {
		if m := reTitleYearBracket.FindStringSubmatch(title); m != nil {
			if yr, err := strconv.Atoi(m[1]); err == nil && yr >= 1900 && yr <= 2100 {
				d.Year = yr
			}
		}
	}
}

type enrichJob struct {
	d       *models.TorrentDetails
	isMovie bool
}

// ruMonths maps Russian month abbreviations to month numbers.
var ruMonths = map[string]int{
	"Янв": 1, "Фев": 2, "Мар": 3, "Апр": 4, "Май": 5, "Июн": 6,
	"Июл": 7, "Авг": 8, "Сен": 9, "Окт": 10, "Ноя": 11, "Дек": 12,
}

// parseRuDate parses "D Mon YYYY" or "D Mon YY" (2-digit year gets "20" prefix).
// Extra fields after the first three are ignored.
func parseRuDate(s string) time.Time {
	parts := strings.Fields(s)
	if len(parts) < 3 {
		return time.Time{}
	}
	day, _ := strconv.Atoi(parts[0])
	monthNum := ruMonths[parts[1]]
	yearStr := parts[2]
	if len(yearStr) == 2 {
		yearStr = "20" + yearStr
	}
	year, _ := strconv.Atoi(yearStr)
	if day == 0 || monthNum == 0 || year == 0 {
		return time.Time{}
	}
	return time.Date(year, time.Month(monthNum), day, 0, 0, 0, 0, time.Local)
}

var reAnimeMovieTag = regexp.MustCompile(`\[[^]]*\bMovie\b[^]]*\]`)

// isMovieCat reports whether the category is a movie (not a series or TV show).
// CatAnime is always TV-searched by category alone, but nnmclub/rutracker
// both tag standalone theatrical anime with a "[Movie]" (or "[YYYY, Movie]")
// bracket distinct from "[TV, ...]"/"[OVA]"/"[ONA]" — TMDB catalogs those as
// movies, and search/tv finds zero results for them (see dev/rutracker.md,
// "Wolf Children" — a real Ghibli-style theatrical film — case).
func isMovieCat(cat string, title string) bool {
	if cat == models.CatAnime {
		return reAnimeMovieTag.MatchString(title)
	}
	return cat == models.CatMovie || cat == models.CatDocMovie || cat == models.CatCartoonMovie
}

// scanCutoff computes the incremental cutoff date for a tracker.
// Returns fullScan=true on the first run (no prior timestamp).
func scanCutoff(tracker string) (fullScan bool, cutoff time.Time) {
	lastParsed := store.LastParsedAtFor(tracker)
	fullScan = lastParsed.IsZero()
	if fullScan {
		log.Printf("%s: first run — full scan", tracker)
		return
	}
	overlapDays := store.GetSettingInt(context.Background(), "parser_overlap_days")
	if overlapDays <= 0 {
		overlapDays = 2
	}
	cutoff = lastParsed.Add(-time.Duration(overlapDays) * 24 * time.Hour)
	log.Printf("%s: incremental scan, cutoff %s (overlap %d days)", tracker, cutoff.Format("2006-01-02"), overlapDays)
	return
}

// commitScan updates last_parsed_at for the tracker if any torrents were processed.
func commitScan(tracker string, processed *atomic.Int64) {
	if n := processed.Load(); n > 0 {
		store.SetLastParsedAtFor(tracker)
		log.Printf("%s: scan complete, processed %d torrents", tracker, n)
	} else {
		log.Printf("%s: scan complete, no torrents processed — last_parsed_at not updated", tracker)
	}
}

// retryOpts reads listing-fetch retry params from app_settings.
// Defaults: 10 attempts, 30s base, 120s max, ratio 2.0.
func retryOpts() (attempts int, baseWait, maxWait time.Duration, ratio float64) {
	ctx := context.Background()
	attempts = store.GetSettingInt(ctx, "parser_retry_attempts")
	if attempts <= 0 {
		attempts = 10
	}
	baseSec := store.GetSettingInt(ctx, "parser_retry_base_wait_sec")
	if baseSec <= 0 {
		baseSec = 30
	}
	maxSec := store.GetSettingInt(ctx, "parser_retry_max_wait_sec")
	if maxSec <= 0 {
		maxSec = 120
	}
	ratio = 2.0
	if v, ok := store.GetSetting(ctx, "parser_retry_ratio"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 1.0 {
			ratio = f
		}
	}
	return attempts, time.Duration(baseSec) * time.Second, time.Duration(maxSec) * time.Second, ratio
}

// runPageLoop paginates a tracker listing using fetchBytesRetry.
// Retry parameters are read from app_settings on each call.
//
// buildURL(page) → URL for 0-based page index.
// parseItems(body) → jobs to enrich, whether cutoff was hit, raw item count from the page.
// processJob is called concurrently (concurrency goroutines) for each job.
//
// Stops when: rawCount == 0, rawCount < pageSize, hitCutoff, or fetch fails.
func runPageLoop(
	fetch fetchFunc,
	tracker string,
	concurrency, pageSize int,
	buildURL func(page int) string,
	parseItems func(body []byte) (jobs []enrichJob, hitCutoff bool, rawCount int),
	processJob func(job enrichJob),
) {
	attempts, baseWait, maxWait, ratio := retryOpts()

	for page := 0; ; page++ {
		if stopRequest.Load() {
			log.Printf("%s: stop requested, halting", tracker)
			return
		}
		url := buildURL(page)
		body, err := fetchBytesRetry(fetch, url, attempts, baseWait, maxWait, ratio)
		if err != nil {
			log.Printf("%s: get %s: %v", tracker, url, err)
			return
		}
		jobs, hitCutoff, rawCount := parseItems(body)
		if rawCount == 0 {
			return
		}
		utils.PForLim(jobs, concurrency, func(_ int, job enrichJob) {
			processJob(job)
		})
		if hitCutoff || rawCount < pageSize {
			return
		}
	}
}
