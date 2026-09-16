package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"

	"movies-api/db/store"
	"movies-api/internal/push"
)

// showGroup collects every newly-aired episode of one show for one push
// subscription, so they can be sent as a single notification card instead of
// one per episode (a show catching up several episodes at once used to fire
// that many separate pushes back-to-back).
type showGroup struct {
	cardID   string
	title    string
	episodes []store.NewEpisodeNotification
}

// subGroup collects every showGroup for one push subscription.
type subGroup struct {
	sub   store.NewEpisodeNotification // endpoint/keys/device/profile — same for the whole subscription
	shows []*showGroup
}

// showSummary is the notification body for one show: "S01E02 — Name" for a
// single new episode, "Вышло N новых серий: S01E02, S01E03, ..." for several.
func showSummary(episodes []store.NewEpisodeNotification) string {
	sort.Slice(episodes, func(i, j int) bool {
		if episodes[i].Season != episodes[j].Season {
			return episodes[i].Season < episodes[j].Season
		}
		return episodes[i].Episode < episodes[j].Episode
	})
	codes := make([]string, len(episodes))
	for i, e := range episodes {
		codes[i] = fmt.Sprintf("S%02dE%02d", e.Season, e.Episode)
	}
	if len(episodes) == 1 {
		body := "Вышла серия " + codes[0]
		if episodes[0].EpisodeName != "" {
			body += " — " + episodes[0].EpisodeName
		}
		return body
	}
	joined := codes[0]
	for _, c := range codes[1:] {
		joined += ", " + c
	}
	return fmt.Sprintf("Вышло %d новых серий: %s", len(episodes), joined)
}

// RunPushNotifyCheck sends "new episode" web push notifications for every
// subscription that has a newly-aired episode (respecting the aired_cutoff
// delay) it hasn't been notified about yet.
//
// Every show gets its own notification card (multiple new episodes of the
// SAME show are grouped into one card), but all of a subscription's cards for
// this check go out as a single push message — sw.js's push handler fans a
// "notifications" array back out into one showNotification() call per show,
// so the device only buzzes/alerts once per check instead of once per show
// (see sw.js's comment), while every show still shows up as its own stacked
// notification.
func RunPushNotifyCheck(ctx context.Context) {
	notifications := store.FindNewEpisodeNotifications(ctx)
	if len(notifications) == 0 {
		return
	}
	log.Printf("tasks: push_notify: %d new episode notifications to send", len(notifications))

	subGroups := make(map[int64]*subGroup)
	var subOrder []int64
	for _, n := range notifications {
		sg, ok := subGroups[n.SubscriptionID]
		if !ok {
			sg = &subGroup{sub: n}
			subGroups[n.SubscriptionID] = sg
			subOrder = append(subOrder, n.SubscriptionID)
		}
		var show *showGroup
		for _, s := range sg.shows {
			if s.cardID == n.CardID {
				show = s
				break
			}
		}
		if show == nil {
			show = &showGroup{cardID: n.CardID, title: n.Title}
			sg.shows = append(sg.shows, show)
		}
		show.episodes = append(show.episodes, n)
	}

	for _, subID := range subOrder {
		sg := subGroups[subID]

		type cardPayload struct {
			Title string `json:"title"`
			Body  string `json:"body"`
			URL   string `json:"url"`
		}
		cards := make([]cardPayload, len(sg.shows))
		var allEpisodes []store.NewEpisodeNotification
		for i, show := range sg.shows {
			cards[i] = cardPayload{Title: show.title, Body: showSummary(show.episodes), URL: "/card/" + show.cardID}
			allEpisodes = append(allEpisodes, show.episodes...)
		}
		payload, _ := json.Marshal(map[string]any{"notifications": cards})

		sub := sg.sub
		status, respBody, err := push.Send(ctx, push.Subscription{Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth}, payload)
		if err != nil {
			log.Printf("tasks: push_notify: send failed for subscription %d: %v", sub.SubscriptionID, err)
			continue
		}
		if status == 404 || status == 410 {
			// Subscription expired/revoked in the browser — stop trying.
			store.DeletePushSubscription(ctx, sub.Endpoint)
			continue
		}
		if status < 200 || status >= 300 {
			log.Printf("tasks: push_notify: subscription %d rejected: status=%d body=%q", sub.SubscriptionID, status, respBody)
			continue
		}
		for _, e := range allEpisodes {
			store.MarkEpisodeNotified(ctx, e.DeviceID, e.ProfileID, e.CardID, e.Season, e.Episode)
		}
	}
}
