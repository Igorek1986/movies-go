package api

import (
	"encoding/json"
	"net/http"

	"movies-api/db/store"
	"movies-api/internal/push"
)

// GET /push/vapid-key
// Public — the VAPID public key is not sensitive, the browser needs it before a
// device token even exists to create a PushSubscription.
func handlePushVapidKey(w http.ResponseWriter, r *http.Request) {
	pub, _ := push.EnsureVAPIDKeys(r.Context())
	JSON(w, http.StatusOK, map[string]string{"key": pub})
}

type pushSubscribeBody struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// POST /push/subscribe?token=&profile_id=
func handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}
	var body pushSubscribeBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Endpoint == "" || body.Keys.P256dh == "" || body.Keys.Auth == "" {
		Error(w, http.StatusBadRequest, "invalid subscription")
		return
	}
	profileID := r.URL.Query().Get("profile_id")
	err := store.SavePushSubscription(r.Context(), d.ID, profileID, body.Endpoint, body.Keys.P256dh, body.Keys.Auth, r.Header.Get("User-Agent"))
	if err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /push/unsubscribe?token=
func handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Endpoint == "" {
		Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	store.DeletePushSubscription(r.Context(), body.Endpoint)
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// GET /push/status?token=&profile_id=
func handlePushStatus(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}
	profileID := r.URL.Query().Get("profile_id")
	JSON(w, http.StatusOK, map[string]bool{"subscribed": store.HasPushSubscription(r.Context(), d.ID, profileID)})
}

// POST /push/test?token=&profile_id=
// Sends a canned test notification to every subscription of this device+profile,
// bypassing the "new episode" gating entirely — for manually verifying the VAPID
// keys / service worker / browser permission flow end to end.
func handlePushTest(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}
	profileID := r.URL.Query().Get("profile_id")
	subs := store.GetPushSubscriptions(r.Context(), d.ID, profileID)
	if len(subs) == 0 {
		Error(w, http.StatusNotFound, "no push subscription for this device/profile")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"title": "Movies API",
		"body":  "Тестовое уведомление — если ты это видишь, push работает.",
		"url":   "/calendar",
	})
	sent := 0
	var results []map[string]any
	for _, s := range subs {
		status, body, err := push.Send(r.Context(), push.Subscription{Endpoint: s.Endpoint, P256dh: s.P256dh, Auth: s.Auth}, payload)
		if err != nil {
			results = append(results, map[string]any{"status": 0, "error": err.Error()})
			continue
		}
		if status == 404 || status == 410 {
			store.DeletePushSubscription(r.Context(), s.Endpoint)
		}
		if status >= 200 && status < 300 {
			sent++
		}
		results = append(results, map[string]any{"status": status, "body": body})
	}
	JSON(w, http.StatusOK, map[string]any{"sent": sent, "results": results})
}
