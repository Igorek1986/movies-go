package parser

import (
	"context"
	"log"
	"net/http"
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
	reTitleYearParen = regexp.MustCompile(`\((\d{4})(?:-\d{4})?\)`)
	reTitleYearRange = regexp.MustCompile(`^(\d{4})-\d{4}`)
	reTitleBrackets  = regexp.MustCompile(`\[.*?\]`)
)

// HasEpisodeBrackets reports whether title contains [...] before the year — indicates episode range.
func HasEpisodeBrackets(title string) bool {
	part := title
	if m := reTitleYearParen.FindStringIndex(title); m != nil {
		part = title[:m[0]]
	}
	return reTitleBrackets.MatchString(part)
}

// ParseTorrentTitle fills Name, Names, Year, VideoQuality, AudioQuality from a torrent title.
// Format: "RuName / EngName / year / ... / quality" or "RuName / EngName (year) quality".
func ParseTorrentTitle(d *models.TorrentDetails, title string) {
	title = strings.ReplaceAll(title, "&amp;", "&")
	parts := strings.Split(title, " / ")
	if len(parts) < 2 {
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

	p1 := strings.TrimSpace(parts[1])
	if yr, err := strconv.Atoi(p1); err == nil && yr >= 1900 && yr <= 2100 {
		d.Year = yr
		return
	}

	// parts[1] is an English name — year may be embedded as "(2006)"
	if m := reTitleYearParen.FindStringSubmatch(p1); m != nil {
		d.Year, _ = strconv.Atoi(m[1])
		p1 = strings.TrimSpace(reTitleYearParen.ReplaceAllString(p1, ""))
	}
	d.Names = []string{strings.TrimSpace(reTitleBrackets.ReplaceAllString(p1, ""))}

	if d.Year == 0 && len(parts) >= 3 {
		yearStr := strings.TrimSpace(parts[2])
		if m := reTitleYearRange.FindStringSubmatch(yearStr); m != nil {
			yearStr = m[1]
		}
		if fields := strings.Fields(yearStr); len(fields) > 0 {
			if yr, err := strconv.Atoi(fields[0]); err == nil && yr >= 1900 && yr <= 2100 {
				d.Year = yr
			}
		}
	}

	// Final fallback: scan full title for (year)
	if d.Year == 0 {
		if m := reTitleYearParen.FindStringSubmatch(title); m != nil {
			d.Year, _ = strconv.Atoi(m[1])
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

// isMovieCat reports whether the category is a movie (not a series or TV show).
func isMovieCat(cat string) bool {
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
	client *http.Client,
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
		body, err := fetchBytesRetry(client, url, attempts, baseWait, maxWait, ratio)
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
