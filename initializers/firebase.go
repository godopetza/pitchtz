package initializers

import (
	"context"
	"encoding/base64"
	"log"
	"os"
	"strings"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

// FCMClient sends push notifications to the mobile app. It stays nil when no
// service-account credential is configured, and every send path nil-checks it
// — PitchTZ runs fine without push, so a missing credential must never be
// fatal (local dev and CI have none).
var FCMClient *messaging.Client

// InitFirebase boots the Firebase Admin SDK from a service account supplied
// through the environment: FIREBASE_SERVICE_ACCOUNT_JSON holds the raw JSON,
// or FIREBASE_SERVICE_ACCOUNT_BASE64 the same JSON base64-encoded (easier to
// paste into a Railway variable without newline mangling). Never commit
// either — they are platform variables only.
func InitFirebase() {
	credentials := strings.TrimSpace(os.Getenv("FIREBASE_SERVICE_ACCOUNT_JSON"))
	if credentials == "" {
		encoded := strings.TrimSpace(os.Getenv("FIREBASE_SERVICE_ACCOUNT_BASE64"))
		if raw, err := base64.StdEncoding.DecodeString(encoded); err == nil && len(raw) > 0 {
			credentials = string(raw)
		}
	}
	if credentials == "" {
		log.Print("firebase: no service account configured — push notifications disabled")
		return
	}

	ctx := context.Background()
	app, err := firebase.NewApp(ctx, nil, option.WithCredentialsJSON([]byte(credentials)))
	if err != nil {
		log.Printf("firebase init failed: %v", err)
		return
	}
	client, err := app.Messaging(ctx)
	if err != nil {
		log.Printf("firebase messaging client failed: %v", err)
		return
	}
	FCMClient = client
	log.Print("firebase: push notifications enabled")
}
