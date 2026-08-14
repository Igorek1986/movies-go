package store

import (
	"context"
	"log"
	"movies-api/db/postgres"
)

// SavePushSubscription upserts a browser push subscription for a device+profile.
// Re-subscribing (browser rotated its endpoint) replaces the keys in place.
func SavePushSubscription(ctx context.Context, deviceID int64, profileID, endpoint, p256dh, auth, userAgent string) error {
	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO push_subscriptions (device_id, profile_id, endpoint, p256dh, auth, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (endpoint) DO UPDATE SET
			device_id  = EXCLUDED.device_id,
			profile_id = EXCLUDED.profile_id,
			p256dh     = EXCLUDED.p256dh,
			auth       = EXCLUDED.auth,
			user_agent = EXCLUDED.user_agent`,
		deviceID, profileID, endpoint, p256dh, auth, userAgent)
	return err
}

// DeletePushSubscription removes a subscription by endpoint — called on explicit
// unsubscribe, and when the push service reports the endpoint is gone (404/410).
func DeletePushSubscription(ctx context.Context, endpoint string) {
	if _, err := postgres.Pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE endpoint = $1`, endpoint); err != nil {
		log.Printf("store: delete push subscription: %v", err)
	}
}

// HasPushSubscription reports whether device+profile has at least one active
// subscription — used by the web app to show the toggle as already-on.
func HasPushSubscription(ctx context.Context, deviceID int64, profileID string) bool {
	var exists bool
	err := postgres.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM push_subscriptions WHERE device_id = $1 AND profile_id = $2)`,
		deviceID, profileID).Scan(&exists)
	return err == nil && exists
}

// PushSubscriptionKeys is the endpoint+keys triple needed to send a push message.
type PushSubscriptionKeys struct {
	Endpoint string
	P256dh   string
	Auth     string
}

// GetPushSubscriptions returns every active subscription for device+profile —
// used by the manual "send test push" admin/debug action.
func GetPushSubscriptions(ctx context.Context, deviceID int64, profileID string) []PushSubscriptionKeys {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT endpoint, p256dh, auth FROM push_subscriptions WHERE device_id = $1 AND profile_id = $2`,
		deviceID, profileID)
	if err != nil {
		log.Printf("store: get push subscriptions: %v", err)
		return nil
	}
	defer rows.Close()
	var out []PushSubscriptionKeys
	for rows.Next() {
		var s PushSubscriptionKeys
		if rows.Scan(&s.Endpoint, &s.P256dh, &s.Auth) == nil {
			out = append(out, s)
		}
	}
	return out
}

// NewEpisodeNotification is one (subscription, newly-aired episode) pair to notify.
type NewEpisodeNotification struct {
	SubscriptionID int64
	Endpoint       string
	P256dh         string
	Auth           string
	DeviceID       int64
	ProfileID      string
	CardID         string
	Title          string // show title
	EpisodeName    string // episode title, may be empty
	Season         int
	Episode        int
}

// FindNewEpisodeNotifications returns, for every push subscription, newly-aired
// episodes (same aired-cutoff as the watched/unwatched logic, see AiredCutoffDate)
// of shows that profile is actively watching, that haven't been notified about
// yet (see push_notified_episodes).
func FindNewEpisodeNotifications(ctx context.Context) []NewEpisodeNotification {
	cutoff := AiredCutoffDate(ctx)
	//nolint:gosec // cutoff comes from AiredCutoffDate (admin setting only), not user input
	rows, err := postgres.Pool.Query(ctx, `
		SELECT ps.id, ps.endpoint, ps.p256dh, ps.auth, ps.device_id, ps.profile_id,
		       ep.card_id, ep.title, ep.episode_title, ep.season, ep.episode
		FROM push_subscriptions ps
		CROSS JOIN LATERAL (
			SELECT DISTINCT mc.card_id, mc.title, COALESCE(e.title, '') AS episode_title, e.season, e.episode
			FROM timecodes tc
			JOIN media_cards mc ON mc.card_id = tc.card_id AND mc.media_type = 'tv'
			JOIN episodes e ON e.tmdb_show_id = mc.tmdb_id AND NOT e.is_special
			WHERE tc.device_id = ps.device_id AND tc.profile_id = ps.profile_id
			  AND e.air_date IS NOT NULL AND e.air_date <= `+cutoff+`
			  -- Hard age cap, independent of push_notified_episodes bookkeeping — a
			  -- push claiming an episode just "aired" is meaningless (and spammy) for
			  -- anything that actually aired long ago, whatever the reason it was never
			  -- marked notified (race at startup, bug, manual DB edit, ...).
			  AND e.air_date >= CURRENT_DATE - INTERVAL '14 days'
			  AND NOT EXISTS (
			      SELECT 1 FROM push_notified_episodes n
			      WHERE n.device_id = ps.device_id AND n.profile_id = ps.profile_id
			        AND n.card_id = mc.card_id AND n.season = e.season AND n.episode = e.episode
			  )
		) ep`)
	if err != nil {
		log.Printf("store: find new episode notifications: %v", err)
		return nil
	}
	defer rows.Close()
	var out []NewEpisodeNotification
	for rows.Next() {
		var n NewEpisodeNotification
		if rows.Scan(&n.SubscriptionID, &n.Endpoint, &n.P256dh, &n.Auth, &n.DeviceID, &n.ProfileID,
			&n.CardID, &n.Title, &n.EpisodeName, &n.Season, &n.Episode) == nil {
			out = append(out, n)
		}
	}
	return out
}

// MarkEpisodeNotified records that device+profile has been notified about this
// episode, so FindNewEpisodeNotifications won't return it again.
func MarkEpisodeNotified(ctx context.Context, deviceID int64, profileID, cardID string, season, episode int) {
	_, err := postgres.Pool.Exec(ctx,
		`INSERT INTO push_notified_episodes (device_id, profile_id, card_id, season, episode)
		 VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
		deviceID, profileID, cardID, season, episode)
	if err != nil {
		log.Printf("store: mark episode notified: %v", err)
	}
}

// SeedNotifiedEpisodes marks every already-aired episode of every subscription's
// watched shows as "notified" WITHOUT sending anything — called once at startup and
// right after a new subscription is created, so FindNewEpisodeNotifications only
// ever surfaces genuinely new episodes instead of flooding a fresh subscription
// with a push for its entire back catalog of aired-but-never-notified episodes.
func SeedNotifiedEpisodes(ctx context.Context) {
	cutoff := AiredCutoffDate(ctx)
	//nolint:gosec // cutoff comes from AiredCutoffDate (admin setting only), not user input
	tag, err := postgres.Pool.Exec(ctx, `
		INSERT INTO push_notified_episodes (device_id, profile_id, card_id, season, episode)
		SELECT DISTINCT ps.device_id, ps.profile_id, mc.card_id, e.season, e.episode
		FROM push_subscriptions ps
		JOIN timecodes tc ON tc.device_id = ps.device_id AND tc.profile_id = ps.profile_id
		JOIN media_cards mc ON mc.card_id = tc.card_id AND mc.media_type = 'tv'
		JOIN episodes e ON e.tmdb_show_id = mc.tmdb_id AND NOT e.is_special
		WHERE e.air_date IS NOT NULL AND e.air_date <= `+cutoff+`
		ON CONFLICT DO NOTHING`)
	if err != nil {
		log.Printf("store: seed notified episodes: %v", err)
		return
	}
	if tag.RowsAffected() > 0 {
		log.Printf("store: seed notified episodes: marked %d already-aired episodes as notified", tag.RowsAffected())
	}
}
