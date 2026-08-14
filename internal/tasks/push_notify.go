package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"movies-api/db/store"
	"movies-api/internal/push"
)

// RunPushNotifyCheck sends "new episode" web push notifications for every
// subscription that has a newly-aired episode (respecting the aired_cutoff
// delay) it hasn't been notified about yet.
func RunPushNotifyCheck(ctx context.Context) {
	notifications := store.FindNewEpisodeNotifications(ctx)
	if len(notifications) == 0 {
		return
	}
	log.Printf("tasks: push_notify: %d new episode notifications to send", len(notifications))

	for _, n := range notifications {
		body := fmt.Sprintf("Вышла серия S%02dE%02d", n.Season, n.Episode)
		if n.EpisodeName != "" {
			body += " — " + n.EpisodeName
		}
		payload, _ := json.Marshal(map[string]any{
			"title": n.Title,
			"body":  body,
			"url":   "/card/" + n.CardID,
		})
		status, body, err := push.Send(ctx, push.Subscription{Endpoint: n.Endpoint, P256dh: n.P256dh, Auth: n.Auth}, payload)
		if err != nil {
			log.Printf("tasks: push_notify: send failed for subscription %d: %v", n.SubscriptionID, err)
			continue
		}
		if status == 404 || status == 410 {
			// Subscription expired/revoked in the browser — stop trying.
			store.DeletePushSubscription(ctx, n.Endpoint)
			continue
		}
		if status < 200 || status >= 300 {
			log.Printf("tasks: push_notify: subscription %d rejected: status=%d body=%q", n.SubscriptionID, status, body)
			continue
		}
		store.MarkEpisodeNotified(ctx, n.DeviceID, n.ProfileID, n.CardID, n.Season, n.Episode)
	}
}
