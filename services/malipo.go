package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Malipo is the shared godopetza payment gateway. PitchTZ never talks to
// Stripe/PawaPay directly: it asks Malipo to collect a payment and then
// receives a signed callback when the money actually lands.

var malipoHTTP = &http.Client{Timeout: 15 * time.Second}

type MalipoPaymentRequest struct {
	Provider    string            `json:"provider"`
	Amount      int64             `json:"amount"`
	Currency    string            `json:"currency"`
	Reference   string            `json:"reference"`
	Description string            `json:"description,omitempty"`
	Phone       string            `json:"phone,omitempty"`
	Operator    string            `json:"operator,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type MalipoPayment struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Reference   string `json:"reference"`
	CheckoutURL string `json:"checkoutUrl,omitempty"`
}

// MalipoConfigured reports whether payment collection is switched on. Without
// both env vars the booking flow still works, it just cannot request money.
func MalipoConfigured() bool {
	return strings.TrimSpace(os.Getenv("MALIPO_URL")) != "" && strings.TrimSpace(os.Getenv("MALIPO_API_KEY")) != ""
}

// CreateMalipoPayment asks Malipo to collect `amount` (minor units; TZS is
// whole shillings) against `reference`, which must be stable per booking so
// retries are idempotent on Malipo's side.
func CreateMalipoPayment(ctx context.Context, input MalipoPaymentRequest) (*MalipoPayment, error) {
	if !MalipoConfigured() {
		return nil, fmt.Errorf("malipo is not configured")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("MALIPO_URL")), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/payments", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", strings.TrimSpace(os.Getenv("MALIPO_API_KEY")))

	response, err := malipoHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	var payload struct {
		MalipoPayment
		Error string `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("malipo returned an unreadable response (%d)", response.StatusCode)
	}
	if response.StatusCode >= 300 {
		if payload.Error != "" {
			return nil, fmt.Errorf("malipo: %s", payload.Error)
		}
		return nil, fmt.Errorf("malipo returned %d", response.StatusCode)
	}
	return &payload.MalipoPayment, nil
}

// MalipoCallback is the normalized envelope Malipo POSTs to our callback URL.
type MalipoCallback struct {
	Source        string            `json:"source"`
	Type          string            `json:"type"`
	Event         string            `json:"event"`
	PaymentID     string            `json:"paymentId"`
	PayoutID      string            `json:"payoutId"`
	Reference     string            `json:"reference"`
	Status        string            `json:"status"`
	Amount        int64             `json:"amount"`
	Currency      string            `json:"currency"`
	FailureReason string            `json:"failureReason"`
	Metadata      map[string]string `json:"metadata"`
}

// VerifyMalipoSignature checks the "t=<unix>,v1=<hex>" header against the raw
// body: v1 = HMAC-SHA256(secret, "<t>.<body>"). Timestamps older than
// tolerance are rejected so a captured callback cannot be replayed later.
func VerifyMalipoSignature(header string, body []byte, tolerance time.Duration) error {
	secret := strings.TrimSpace(os.Getenv("MALIPO_WEBHOOK_SECRET"))
	if secret == "" {
		return fmt.Errorf("MALIPO_WEBHOOK_SECRET is not set")
	}
	var timestamp, signature string
	for _, part := range strings.Split(header, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		switch key {
		case "t":
			timestamp = value
		case "v1":
			signature = value
		}
	}
	if timestamp == "" || signature == "" {
		return fmt.Errorf("malformed signature header")
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("malformed signature timestamp")
	}
	age := time.Since(time.Unix(seconds, 0))
	if age > tolerance || age < -tolerance {
		return fmt.Errorf("signature timestamp is outside the allowed window")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", seconds)
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}
