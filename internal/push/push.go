// Package push sends Web Push (VAPID) notifications to browsers.
package push

import (
	"context"
	"errors"
	"io"
	"movies-api/db/store"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// EnsureVAPIDKeys returns the VAPID key pair used to sign push messages,
// generating and persisting one on first use.
func EnsureVAPIDKeys(ctx context.Context) (publicKey, privateKey string) {
	publicKey, _ = store.GetSetting(ctx, "vapid_public_key")
	privateKey, _ = store.GetSetting(ctx, "vapid_private_key")
	if publicKey != "" && privateKey != "" {
		return publicKey, privateKey
	}
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", ""
	}
	store.SetSetting(ctx, "vapid_public_key", pub)
	store.SetSetting(ctx, "vapid_private_key", priv)
	return pub, priv
}

// Subscription mirrors the browser's PushSubscription shape.
type Subscription struct {
	Endpoint string
	P256dh   string
	Auth     string
}

// Send delivers one push message and returns the push service's HTTP status
// code (and response body, for diagnosing rejections), so callers can tell an
// expired subscription (404/410) from a transient or config failure.
func Send(ctx context.Context, sub Subscription, payload []byte) (statusCode int, body string, err error) {
	pub, priv := EnsureVAPIDKeys(ctx)
	if pub == "" || priv == "" {
		return 0, "", errors.New("vapid keys unavailable")
	}
	subscriber, _ := store.GetSetting(ctx, "contact_email")
	if subscriber == "" {
		subscriber = "admin@example.com"
	}
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, &webpush.Options{
		// webpush-go's getVAPIDAuthorizationHeader already prepends "mailto:"
		// itself (unless the value starts with "https:") — doing it here too
		// produced "mailto:mailto:...", which most push services silently
		// tolerate but Apple's web.push.apple.com strictly rejects as
		// BadJwtToken.
		Subscriber:      subscriber,
		VAPIDPublicKey:  pub,
		VAPIDPrivateKey: priv,
		TTL:             86400,
	})
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}
