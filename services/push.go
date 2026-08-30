package services

import (
	"context"
	"log"
	"time"

	"firebase.google.com/go/v4/messaging"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/google/uuid"
)

// notificationChannel matches the Android channel the mobile app creates.
// Changing it here without changing it in the app means silent notifications.
const notificationChannel = "pitchtz_alerts"

// Push is one notification headed for a player's devices. Title and Body are
// what they read on the lock screen; Data rides along invisibly so a tap can
// deep-link straight to the booking or match that triggered it.
type Push struct {
	Title string
	Body  string
	// Data always carries a "type" key — the app switches on it to route the
	// tap (e.g. {"type":"booking_confirmed","booking_id":"…"}).
	Data map[string]string
}

// Text is a bilingual pair. PitchTZ users each carry a language on their
// account, so a push speaks Swahili or English per recipient rather than
// picking one and hoping.
type Text struct {
	EN string
	SW string
}

func (t Text) forLanguage(language string) string {
	if language == "en" {
		return t.EN
	}
	if t.SW == "" {
		return t.EN
	}
	return t.SW
}

// SendToUser delivers a push to every device the user has registered, in that
// device's own language. Safe to call as `go services.SendToUser(...)` — it
// swallows its own errors and no-ops when Firebase isn't configured.
func SendToUser(userID uuid.UUID, title, body Text, data map[string]string) {
	if initializers.FCMClient == nil || initializers.DB == nil {
		return
	}
	var devices []models.DeviceToken
	initializers.DB.Where("user_id = ?", userID).Order("last_seen_at DESC").Limit(10).Find(&devices)
	if len(devices) == 0 {
		return
	}

	// Group by language so each batch shares one rendered message.
	byLanguage := map[string][]string{}
	for _, device := range devices {
		byLanguage[device.Language] = append(byLanguage[device.Language], device.Token)
	}
	for language, tokens := range byLanguage {
		sendBatch(tokens, Push{
			Title: title.forLanguage(language),
			Body:  body.forLanguage(language),
			Data:  data,
		})
	}
}

// sendBatch delivers one rendered message to many tokens in a single API call
// and prunes the tokens FCM reports as permanently dead (app uninstalled, or
// the token rotated) so we stop paying to retry them forever.
func sendBatch(tokens []string, push Push) {
	if initializers.FCMClient == nil || len(tokens) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	messages := make([]*messaging.Message, 0, len(tokens))
	for _, token := range tokens {
		messages = append(messages, &messaging.Message{
			Token:        token,
			Notification: &messaging.Notification{Title: push.Title, Body: push.Body},
			Data:         push.Data,
			Android: &messaging.AndroidConfig{
				Priority: "high",
				Notification: &messaging.AndroidNotification{
					ChannelID: notificationChannel,
					Sound:     "default",
				},
			},
			APNS: &messaging.APNSConfig{
				Payload: &messaging.APNSPayload{Aps: &messaging.Aps{Sound: "default"}},
			},
		})
	}

	response, err := initializers.FCMClient.SendEach(ctx, messages)
	if err != nil {
		log.Printf("push send failed (%d tokens): %v", len(tokens), err)
		return
	}
	var dead []string
	for index, result := range response.Responses {
		if result.Success || index >= len(tokens) {
			continue
		}
		if messaging.IsUnregistered(result.Error) || messaging.IsInvalidArgument(result.Error) {
			dead = append(dead, tokens[index])
		}
	}
	if len(dead) > 0 {
		initializers.DB.Where("token IN ?", dead).Delete(&models.DeviceToken{})
		log.Printf("push: pruned %d dead device tokens", len(dead))
	}
}

// SendToUsers fans one notification out to several accounts — used where both
// captains, or a whole squad, should hear the same news.
func SendToUsers(userIDs []uuid.UUID, title, body Text, data map[string]string) {
	seen := map[uuid.UUID]bool{}
	for _, userID := range userIDs {
		if userID == uuid.Nil || seen[userID] {
			continue
		}
		seen[userID] = true
		SendToUser(userID, title, body, data)
	}
}
