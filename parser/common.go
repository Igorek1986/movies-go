package parser

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"movies-api/db/models"
	"movies-api/db/store"
	"movies-api/utils"
)

type enrichJob struct {
	d       *models.TorrentDetails
	isMovie bool
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
