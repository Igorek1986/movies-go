package parser

import (
	"context"
	"log"
	"strings"
	"sync/atomic"

	"movies-api/db/store"
)

// Parser is implemented by each tracker parser.
type Parser interface {
	Name() string
	Parse()
}

var runActive atomic.Bool

// RunAll runs all enabled parsers in the configured order.
// It is a no-op if a run is already in progress.
func RunAll() {
	if !runActive.CompareAndSwap(false, true) {
		log.Println("parser: RunAll already in progress, skipping")
		return
	}
	defer runActive.Store(false)

	ctx := context.Background()
	orderVal, ok := store.GetSetting(ctx, "parser_order")
	if !ok || strings.TrimSpace(orderVal) == "" {
		orderVal = "kinozal,nnmclub,rutor"
	}

	all := map[string]Parser{
		"kinozal": NewKinozal(),
		"nnmclub": NewNNMClub(),
		"rutor":   NewRutor(),
	}

	for _, name := range strings.Split(orderVal, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		p, ok := all[name]
		if !ok {
			log.Printf("parser: unknown tracker %q in order, skipping", name)
			continue
		}
		enabled, _ := store.GetSetting(ctx, "parser_"+name+"_enabled")
		if enabled == "0" {
			log.Printf("parser: %s disabled, skipping", name)
			continue
		}
		log.Printf("parser: starting %s", name)
		p.Parse()
	}
	log.Println("parser: RunAll complete")
}

// IsRunning reports whether RunAll is currently executing.
func IsRunning() bool {
	return runActive.Load()
}
