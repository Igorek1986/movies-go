package parser

import (
	"context"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"movies-api/db/store"
)

// Parser is implemented by each tracker parser.
type Parser interface {
	Name() string
	Parse()
}

// OnComplete is called after every RunAll or StartOne completes.
// Set from main to trigger cache invalidation without a circular import.
var OnComplete func()

var (
	runActive      atomic.Bool
	stopRequest    atomic.Bool
	nextRunAtVal   atomic.Value // stores time.Time
	currentTracker atomic.Value // stores string, "" when idle
)

// CurrentTracker returns the name of the tracker being parsed right now, or "".
func CurrentTracker() string {
	if v := currentTracker.Load(); v != nil {
		return v.(string)
	}
	return ""
}

// SetNextRunAt stores the scheduled time of the next RunAll call.
func SetNextRunAt(t time.Time) { nextRunAtVal.Store(t) }

// NextRunAt returns the scheduled time of the next RunAll call (zero if unknown).
func NextRunAt() time.Time {
	if v := nextRunAtVal.Load(); v != nil {
		return v.(time.Time)
	}
	return time.Time{}
}

// RequestStop asks a running RunAll to stop after the current tracker finishes.
func RequestStop() { stopRequest.Store(true) }

// IsRunning reports whether RunAll is currently executing.
func IsRunning() bool { return runActive.Load() }

// IsStopRequested reports whether a stop has been requested.
func IsStopRequested() bool { return stopRequest.Load() }

// StartOne claims the run slot and launches a single named tracker in a goroutine.
// Returns false if a run is already in progress or the name is unknown.
// The CAS is done synchronously so IsRunning() returns true before the caller responds.
func StartOne(name string) bool {
	all := map[string]Parser{
		"kinozal": NewKinozal(),
		"nnmclub": NewNNMClub(),
		"rutor":   NewRutor(),
	}
	p, ok := all[name]
	if !ok {
		log.Printf("parser: unknown tracker %q", name)
		return false
	}
	if !runActive.CompareAndSwap(false, true) {
		return false
	}
	stopRequest.Store(false)
	currentTracker.Store(name)
	go func() {
		defer func() {
			currentTracker.Store("")
			runActive.Store(false)
			log.Printf("parser: ■ %s завершён", name)
			if OnComplete != nil {
				OnComplete()
			}
		}()
		log.Printf("parser: ▶ запуск %s (single)", name)
		p.Parse()
	}()
	return true
}

// RunAll runs all enabled parsers in the configured order.
// It is a no-op if a run is already in progress.
func RunAll() {
	if !runActive.CompareAndSwap(false, true) {
		log.Println("parser: RunAll already in progress, skipping")
		return
	}
	stopRequest.Store(false)
	log.Println("parser: ▶ запуск")
	defer func() {
		currentTracker.Store("")
		runActive.Store(false)
		log.Println("parser: ■ остановлен")
	}()

	ctx := context.Background()
	orderVal, ok := store.GetSetting(ctx, "parser_order")
	if !ok || strings.TrimSpace(orderVal) == "" {
		orderVal = "rutor,kinozal,nnmclub"
	}

	all := map[string]Parser{
		"kinozal": NewKinozal(),
		"nnmclub": NewNNMClub(),
		"rutor":   NewRutor(),
	}

	// rutor is on by default; kinozal and nnmclub must be explicitly enabled.
	defaultEnabled := map[string]bool{
		"rutor":   true,
		"kinozal": false,
		"nnmclub": false,
	}

	for _, name := range strings.Split(orderVal, ",") {
		if stopRequest.Load() {
			log.Println("parser: остановлен по запросу")
			break
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		p, ok := all[name]
		if !ok {
			log.Printf("parser: unknown tracker %q in order, skipping", name)
			continue
		}
		enabled, hasSetting := store.GetSetting(ctx, "parser_"+name+"_enabled")
		isEnabled := defaultEnabled[name]
		if hasSetting {
			isEnabled = enabled == "1"
		}
		if !isEnabled {
			log.Printf("parser: %s disabled, skipping", name)
			continue
		}
		log.Printf("parser: starting %s", name)
		currentTracker.Store(name)
		p.Parse()
	}
	log.Println("parser: RunAll complete")
	if OnComplete != nil {
		OnComplete()
	}
}

