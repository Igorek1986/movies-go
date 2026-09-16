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

// showGroup collects every newly-aired episode of one show for one push
// subscription, so they can be sent as a single notification instead of one
// per episode (a show catching up several episodes at once used to fire that
// many separate pushes back-to-back).
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

// episodeCodes formats episodes as "S01E02, S01E03, ...", sorted by season/episode.
func episodeCodes(episodes []store.NewEpisodeNotification) []string {
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
	return codes
}

// showSummary is the per-show fragment of a notification body: the episode
// code (+ name, if a single episode) or a comma-separated list of codes.
func showSummary(episodes []store.NewEpisodeNotification) string {
	codes := episodeCodes(episodes)
	if len(episodes) == 1 && episodes[0].EpisodeName != "" {
		return codes[0] + " — " + episodes[0].EpisodeName
	}
	return strings.Join(codes, ", ")
}

// sendGroupPush sends one push notification and, on success, marks every
// episode it covered as notified so the next check won't resend them.
func sendGroupPush(ctx context.Context, sub store.NewEpisodeNotification, title, body, url string, episodes []store.NewEpisodeNotification) {
	payload, _ := json.Marshal(map[string]any{"title": title, "body": body, "url": url})
	status, respBody, err := push.Send(ctx, push.Subscription{Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth}, payload)
	if err != nil {
		log.Printf("tasks: push_notify: send failed for subscription %d: %v", sub.SubscriptionID, err)
		return
	}
	if status == 404 || status == 410 {
		// Subscription expired/revoked in the browser — stop trying.
		store.DeletePushSubscription(ctx, sub.Endpoint)
		return
	}
	if status < 200 || status >= 300 {
		log.Printf("tasks: push_notify: subscription %d rejected: status=%d body=%q", sub.SubscriptionID, status, respBody)
		return
	}
	for _, e := range episodes {
		store.MarkEpisodeNotified(ctx, e.DeviceID, e.ProfileID, e.CardID, e.Season, e.Episode)
	}
}

// RunPushNotifyCheck sends "new episode" web push notifications for every
// subscription that has a newly-aired episode (respecting the aired_cutoff
// delay) it hasn't been notified about yet. Multiple new episodes of the same
// show are always grouped into one notification; whether different shows for
// the same subscription also combine into a single push is controlled by the
// "push_notify_combine_all" admin setting (default off — one push per show).
func RunPushNotifyCheck(ctx context.Context) {
	notifications := store.FindNewEpisodeNotifications(ctx)
	if len(notifications) == 0 {
		return
	}
	log.Printf("tasks: push_notify: %d new episode notifications to send", len(notifications))

	combineAll, _ := store.GetSetting(ctx, "push_notify_combine_all")

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

		if combineAll != "1" || len(sg.shows) == 1 {
			// One push per show (still combining that show's own new episodes).
			for _, show := range sg.shows {
				var body string
				if len(show.episodes) == 1 {
					body = "Вышла серия " + showSummary(show.episodes)
				} else {
					body = fmt.Sprintf("Вышло %d новых серий: %s", len(show.episodes), showSummary(show.episodes))
				}
				sendGroupPush(ctx, sg.sub, show.title, body, "/card/"+show.cardID, show.episodes)
			}
			continue
		}

		// Combined mode with several different shows — one push for the whole
		// subscription. No single card to deep-link to, so url is left empty
		// (sw.js's notificationclick falls back to '/').
		lines := make([]string, len(sg.shows))
		var allEpisodes []store.NewEpisodeNotification
		for i, show := range sg.shows {
			lines[i] = show.title + ": " + showSummary(show.episodes)
			allEpisodes = append(allEpisodes, show.episodes...)
		}
		sendGroupPush(ctx, sg.sub, "Новые серии", strings.Join(lines, "\n"), "", allEpisodes)
	}
}
