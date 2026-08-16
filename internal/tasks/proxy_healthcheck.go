package tasks

import (
	"context"
	"log"
	"sync"
	"time"

	"movies-api/db/store"
	"movies-api/internal/proxy"
)

// runProxyHealthcheckLoop periodically re-probes every enabled proxy config
// (same check as the admin "Тест" button) so /admin/proxies can show a live
// status dot without anyone having to open the page. Logs only on a state
// transition (ok→fail or fail→ok), not on every tick, to stay readable.
func runProxyHealthcheckLoop(ctx context.Context) {
	interval := proxyHealthcheckInterval(ctx)
	tick := time.NewTicker(interval)
	defer tick.Stop()

	RunProxyHealthcheck(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			RunProxyHealthcheck(ctx)
			// The interval itself is admin-configurable — pick up a change
			// without requiring a restart.
			if next := proxyHealthcheckInterval(ctx); next != interval {
				interval = next
				tick.Reset(interval)
			}
		}
	}
}

func proxyHealthcheckInterval(ctx context.Context) time.Duration {
	min := store.GetSettingInt(ctx, "proxy_healthcheck_interval_min")
	if min < 1 {
		min = 5
	}
	return time.Duration(min) * time.Minute
}

// lastProxyHealth tracks the previous OK/fail state per proxy id in-memory,
// purely to decide whether a state change is worth a log line.
var (
	lastProxyHealthMu sync.Mutex
	lastProxyHealth   = map[int]bool{}
)

// RunProxyHealthcheck probes every enabled proxy config and persists the
// result (store.UpdateProxyHealthcheck) for the admin UI to read on load.
func RunProxyHealthcheck(ctx context.Context) {
	configs, err := store.ListProxyConfigs(ctx)
	if err != nil {
		log.Printf("tasks: proxy_healthcheck: list configs: %v", err)
		return
	}

	lastProxyHealthMu.Lock()
	defer lastProxyHealthMu.Unlock()

	for _, c := range configs {
		if !c.Enabled {
			continue
		}
		checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		res := proxy.TestConfig(checkCtx, c)
		cancel()

		if err := store.UpdateProxyHealthcheck(ctx, c.ID, res.OK, res.ErrorString()); err != nil {
			log.Printf("tasks: proxy_healthcheck: update %q: %v", c.Name, err)
		}

		wasOK, known := lastProxyHealth[c.ID]
		if !known || wasOK != res.OK {
			if res.OK {
				log.Printf("tasks: proxy_healthcheck: %q (id=%d) OK (%dms)", c.Name, c.ID, res.MS)
			} else {
				log.Printf("tasks: proxy_healthcheck: %q (id=%d) DOWN: %s", c.Name, c.ID, res.ErrorString())
			}
		}
		lastProxyHealth[c.ID] = res.OK
	}
}
