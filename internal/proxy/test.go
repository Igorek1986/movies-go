package proxy

import (
	"context"
	"fmt"
	"movies-api/db/models"
	"net/http"
	"time"
)

// TestResult is the outcome of probing one proxy config — shared between the
// manual "Тест" button (internal/api/proxy.go) and the background healthcheck
// (internal/tasks/proxy_healthcheck.go).
type TestResult struct {
	OK     bool
	Status int
	Err    error
	MS     int64
}

// TestConfig builds a client for cfg alone and probes it against TMDB —
// the same reachability check the admin "Тест" button uses.
func TestConfig(ctx context.Context, cfg models.ProxyConfig) TestResult {
	client := BuildClient([]models.ProxyConfig{cfg})
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.themoviedb.org/3/configuration", nil)
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return TestResult{OK: false, Err: err, MS: elapsed.Milliseconds()}
	}
	defer resp.Body.Close()
	return TestResult{OK: resp.StatusCode < 500, Status: resp.StatusCode, MS: elapsed.Milliseconds()}
}

// ErrorString renders TestResult's failure reason for logging/storage.
func (r TestResult) ErrorString() string {
	if r.OK {
		return ""
	}
	if r.Err != nil {
		return r.Err.Error()
	}
	return fmt.Sprintf("HTTP %d", r.Status)
}
