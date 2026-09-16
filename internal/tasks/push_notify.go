package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"movies-api/db/store"
	"movies-api/internal/push"
)

// episodeGroup collects every newly-aired episode of one show for one push
// subscription, so they can be sent as a single notification instead of one
// per episode (a show catching up several episodes at once used to fire that
// many separate pushes back-to-back).
type episodeGroup struct {
	sub      store.NewEpisodeNotification // endpoint/keys/device/profile/card/title — same for the whole group
	episodes []store.NewEpisodeNotification
}

// RunPushNotifyCheck sends "new episode" web push notifications for every
// subscription that has a newly-aired episode (respecting the aired_cutoff
// delay) it hasn't been notified about yet. Multiple new episodes of the same
// show for the same subscription are grouped into one notification.
func RunPushNotifyCheck(ctx context.Context) {
	notifications := store.FindNewEpisodeNotifications(ctx)
	if len(notifications) == 0 {
		return
	}
	log.Printf("tasks: push_notify: %d new episode notifications to send", len(notifications))

	groups := make(map[string]*episodeGroup)
	var order []string
	for _, n := range notifications {
		key := fmt.Sprintf("%d|%s", n.SubscriptionID, n.CardID)
		g, ok := groups[key]
		if !ok {
			g = &episodeGroup{sub: n}
			groups[key] = g
			order = append(order, key)
		}
		g.episodes = append(g.episodes, n)
	}

	for _, key := range order {
		g := groups[key]
		sort.Slice(g.episodes, func(i, j int) bool {
			if g.episodes[i].Season != g.episodes[j].Season {
				return g.episodes[i].Season < g.episodes[j].Season
			}
			return g.episodes[i].Episode < g.episodes[j].Episode
		})

		var body string
		if len(g.episodes) == 1 {
			e := g.episodes[0]
			body = fmt.Sprintf("Вышла серия S%02dE%02d", e.Season, e.Episode)
			if e.EpisodeName != "" {
				body += " — " + e.EpisodeName
			}
		} else {
			codes := make([]string, len(g.episodes))
			for i, e := range g.episodes {
				codes[i] = fmt.Sprintf("S%02dE%02d", e.Season, e.Episode)
			}
			body = fmt.Sprintf("Вышло %d новых серий: %s", len(g.episodes), strings.Join(codes, ", "))
		}

		payload, _ := json.Marshal(map[string]any{
			"title": g.sub.Title,
			"body":  body,
			"url":   "/card/" + g.sub.CardID,
		})
		status, respBody, err := push.Send(ctx, push.Subscription{Endpoint: g.sub.Endpoint, P256dh: g.sub.P256dh, Auth: g.sub.Auth}, payload)
		if err != nil {
			log.Printf("tasks: push_notify: send failed for subscription %d: %v", g.sub.SubscriptionID, err)
			continue
		}
		if status == 404 || status == 410 {
			// Subscription expired/revoked in the browser — stop trying.
			store.DeletePushSubscription(ctx, g.sub.Endpoint)
			continue
		}
		if status < 200 || status >= 300 {
			log.Printf("tasks: push_notify: subscription %d rejected: status=%d body=%q", g.sub.SubscriptionID, status, respBody)
			continue
		}
		for _, e := range g.episodes {
			store.MarkEpisodeNotified(ctx, e.DeviceID, e.ProfileID, e.CardID, e.Season, e.Episode)
		}
	}
}
