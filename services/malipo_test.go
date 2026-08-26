package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

func sign(secret string, t int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", t)
	mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", t, hex.EncodeToString(mac.Sum(nil)))
}

func TestVerifyMalipoSignature(t *testing.T) {
	const secret = "test-secret"
	t.Setenv("MALIPO_WEBHOOK_SECRET", secret)
	body := []byte(`{"reference":"pitchtz-share-1","status":"completed"}`)
	now := time.Now().Unix()

	if err := VerifyMalipoSignature(sign(secret, now, body), body, 5*time.Minute); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := VerifyMalipoSignature(sign("wrong-secret", now, body), body, 5*time.Minute); err == nil {
		t.Fatal("signature made with the wrong secret was accepted")
	}
	if err := VerifyMalipoSignature(sign(secret, now, body), []byte(`{"reference":"tampered"}`), 5*time.Minute); err == nil {
		t.Fatal("tampered body was accepted")
	}
	stale := time.Now().Add(-10 * time.Minute).Unix()
	if err := VerifyMalipoSignature(sign(secret, stale, body), body, 5*time.Minute); err == nil {
		t.Fatal("replayed stale callback was accepted")
	}
	if err := VerifyMalipoSignature("garbage", body, 5*time.Minute); err == nil {
		t.Fatal("malformed header was accepted")
	}
}
