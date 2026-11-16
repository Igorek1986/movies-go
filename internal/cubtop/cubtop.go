// Package cubtop stores a complete last-successful snapshot of CUB fire charts.
package cubtop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"movies-api/db/postgres"
)

type Card struct {
	ID        int64           `json:"id"`
	MediaType string          `json:"media_type"`
	Title     string          `json:"title"`
	Name      string          `json:"name"`
	Overview  string          `json:"overview"`
	PG        int             `json:"PG"`
	Adult     bool            `json:"adult"`
	Fire      int             `json:"cub_hundred_fire"`
	Raw       json.RawMessage `json:"-"`
}

var refreshMu sync.Mutex
var client = &http.Client{Timeout: 20 * time.Second}

// Refresh replaces the snapshot only after both charts have been downloaded.
func Refresh(ctx context.Context) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cards, err := download(ctx)
	if err != nil {
		log.Printf("cubtop: refresh failed: %v", err)
		return
	}
	data, err := json.Marshal(cards)
	if err != nil {
		return
	}
	_, err = postgres.Pool.Exec(ctx, `INSERT INTO cub_top_snapshot (id, cards, updated_at) VALUES (1,$1,now()) ON CONFLICT(id) DO UPDATE SET cards=EXCLUDED.cards,updated_at=EXCLUDED.updated_at`, data)
	if err != nil {
		log.Printf("cubtop: save failed: %v", err)
	}
}

func download(ctx context.Context) ([]json.RawMessage, error) {
	cards := []Card{}
	seen := map[string]bool{}
	for _, kind := range []string{"movie", "tv"} {
		for page, pages := 1, 1; page <= pages; page++ {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://tmdb.cub.red/top/fire/%s?page=%d", kind, page), nil)
			if err != nil {
				return nil, err
			}
			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			var body struct {
				Results    []json.RawMessage `json:"results"`
				TotalPages int               `json:"total_pages"`
			}
			err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK || err != nil || len(body.Results) == 0 || body.TotalPages < 1 || body.TotalPages > 50 {
				return nil, fmt.Errorf("invalid %s page %d response", kind, page)
			}
			pages = body.TotalPages
			for _, raw := range body.Results {
				var card Card
				if err = json.Unmarshal(raw, &card); err != nil {
					return nil, err
				}
				if card.ID <= 0 || card.Fire <= 0 {
					return nil, fmt.Errorf("invalid chart entry")
				}
				key := fmt.Sprintf("%s_%d", kind, card.ID)
				if seen[key] {
					continue
				}
				seen[key] = true
				var fields map[string]json.RawMessage
				if err = json.Unmarshal(raw, &fields); err != nil {
					return nil, err
				}
				fields["media_type"], _ = json.Marshal(kind)
				// Keep vote_average as TMDB: fire count is a separate popularity metric.
				card.Raw, _ = json.Marshal(fields)
				card.MediaType = kind
				cards = append(cards, card)
			}
		}
	}
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Fire == cards[j].Fire {
			if cards[i].MediaType == cards[j].MediaType {
				return cards[i].ID < cards[j].ID
			}
			return cards[i].MediaType < cards[j].MediaType
		}
		return cards[i].Fire > cards[j].Fire
	})
	result := make([]json.RawMessage, 0, len(cards))
	for _, card := range cards {
		result = append(result, card.Raw)
	}
	return result, nil
}

func Read(ctx context.Context) ([]Card, error) {
	var data []byte
	err := postgres.Pool.QueryRow(ctx, `SELECT cards FROM cub_top_snapshot WHERE id=1`).Scan(&data)
	if err != nil {
		return nil, err
	}
	var raws []json.RawMessage
	if err = json.Unmarshal(data, &raws); err != nil {
		return nil, err
	}
	cards := make([]Card, 0, len(raws))
	for _, raw := range raws {
		var c Card
		if err = json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		c.Raw = raw
		cards = append(cards, c)
	}
	return cards, nil
}
