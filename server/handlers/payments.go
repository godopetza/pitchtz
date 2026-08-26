package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/services"
	"github.com/godopetza/pitchtz/utils"
	"github.com/google/uuid"
)

type requestPaymentInput struct {
	Provider string `json:"provider" binding:"required"`
	Phone    string `json:"phone"`
	Operator string `json:"operator"`
	// AmountTZS lets an owner collect a deposit instead of the full total.
	// Zero means "the whole outstanding booking total".
	AmountTZS int64 `json:"amount_tzs"`
}

// RequestBookingPayment asks Malipo to collect money for one booking and
// records the attempt as a PaymentShare + PaymentTransaction. The booking is
// only marked paid later, when Malipo's signed callback confirms settlement.
func RequestBookingPayment(c *gin.Context) {
	if initializers.DB == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "DATABASE_REQUIRED", "payments are unavailable")
		return
	}
	if !services.MalipoConfigured() {
		utils.RespondError(c, http.StatusServiceUnavailable, "PAYMENTS_NOT_CONFIGURED", "payment collection is not configured yet")
		return
	}
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	bookingID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input requestPaymentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "provider is required")
		return
	}

	var booking models.Booking
	if err := initializers.DB.WithContext(c.Request.Context()).First(&booking, "id = ?", bookingID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "BOOKING_NOT_FOUND", "booking was not found")
		return
	}
	if !ownerOwnsPitch(c, ownerID, booking.PitchID) {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_VENUE", "this booking does not belong to your venue")
		return
	}

	amount := input.AmountTZS
	if amount <= 0 {
		amount = booking.TotalTZS
	}
	if amount <= 0 {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_AMOUNT", "amount must be greater than zero")
		return
	}

	share := models.PaymentShare{
		BookingID:  booking.ID,
		PayerPhone: strings.TrimSpace(input.Phone),
		AmountTZS:  amount,
		Kind:       "gateway",
		Status:     "unpaid",
	}
	if err := initializers.DB.WithContext(c.Request.Context()).Create(&share).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "PAYMENT_INIT_FAILED", "could not start the payment")
		return
	}
	chargeShareViaMalipo(c, booking, share, input.Provider, input.Phone, input.Operator)
}

// chargeShareViaMalipo starts (or retries) collection of one share: records
// the attempt as a PaymentTransaction and asks Malipo to prompt the payer's
// phone. The first attempt reuses the share id as reference (Malipo-side
// idempotency); later retries get an -rN suffix so a failed prompt can be
// retried with a fresh Malipo payment.
func chargeShareViaMalipo(c *gin.Context, booking models.Booking, share models.PaymentShare, provider, phone, operator string) {
	var attempts int64
	initializers.DB.WithContext(c.Request.Context()).Model(&models.PaymentTransaction{}).
		Where("share_id = ?", share.ID).Count(&attempts)
	var pending int64
	initializers.DB.WithContext(c.Request.Context()).Model(&models.PaymentTransaction{}).
		Where("share_id = ? AND status IN ?", share.ID, []string{"pending", "completed"}).Count(&pending)
	if pending > 0 {
		utils.RespondError(c, http.StatusConflict, "PAYMENT_IN_PROGRESS", "a payment for this share is already pending or completed")
		return
	}
	reference := fmt.Sprintf("pitchtz-share-%s", share.ID)
	if attempts > 0 {
		reference = fmt.Sprintf("pitchtz-share-%s-r%d", share.ID, attempts+1)
	}
	transaction := models.PaymentTransaction{
		ShareID:        share.ID,
		Provider:       provider,
		IdempotencyKey: reference,
		AmountTZS:      share.AmountTZS,
		Direction:      "charge",
		Status:         "initiated",
	}
	if err := initializers.DB.WithContext(c.Request.Context()).Create(&transaction).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "PAYMENT_INIT_FAILED", "could not start the payment")
		return
	}

	payment, err := services.CreateMalipoPayment(c.Request.Context(), services.MalipoPaymentRequest{
		Provider:    provider,
		Amount:      share.AmountTZS,
		Currency:    "TZS",
		Reference:   reference,
		Description: fmt.Sprintf("PitchTZ booking %s", booking.Code),
		Phone:       strings.TrimSpace(phone),
		Operator:    strings.TrimSpace(operator),
		Metadata: map[string]string{
			"bookingId":   booking.ID.String(),
			"bookingCode": booking.Code,
			"shareId":     share.ID.String(),
		},
	})
	if err != nil {
		initializers.DB.Model(&models.PaymentTransaction{}).Where("id = ?", transaction.ID).Update("status", "failed")
		utils.RespondError(c, http.StatusBadGateway, "PAYMENT_GATEWAY_ERROR", err.Error())
		return
	}

	initializers.DB.Model(&models.PaymentTransaction{}).Where("id = ?", transaction.ID).
		Updates(map[string]interface{}{"provider_ref": payment.ID, "status": "pending"})
	if strings.TrimSpace(phone) != "" {
		initializers.DB.Model(&models.PaymentShare{}).Where("id = ?", share.ID).Update("payer_phone", strings.TrimSpace(phone))
	}

	utils.RespondSuccess(c, http.StatusAccepted, gin.H{
		"share_id":     share.ID,
		"reference":    reference,
		"amount_tzs":   share.AmountTZS,
		"payment_id":   payment.ID,
		"status":       payment.Status,
		"checkout_url": payment.CheckoutURL,
	}, "Payment requested. Confirm the prompt on the payer's phone.")
}

// payShopOrderViaMalipo starts collection for a shop order. Each attempt gets
// a unique reference; the order's pending-status check is the double-pay guard.
func payShopOrderViaMalipo(c *gin.Context, order models.ShopOrder, input clientPayInput) {
	reference := fmt.Sprintf("pitchtz-order-%s-%d", order.ID, time.Now().Unix())
	payment, err := services.CreateMalipoPayment(c.Request.Context(), services.MalipoPaymentRequest{
		Provider:    input.Provider,
		Amount:      order.TotalTZS,
		Currency:    "TZS",
		Reference:   reference,
		Description: fmt.Sprintf("PitchTZ shop %s", order.Code),
		Phone:       strings.TrimSpace(input.Phone),
		Operator:    strings.TrimSpace(input.Operator),
		Metadata:    map[string]string{"orderId": order.ID.String(), "orderCode": order.Code},
	})
	if err != nil {
		utils.RespondError(c, http.StatusBadGateway, "PAYMENT_GATEWAY_ERROR", err.Error())
		return
	}
	initializers.DB.WithContext(c.Request.Context()).Model(&models.ShopOrder{}).
		Where("id = ?", order.ID).Update("phone", strings.TrimSpace(input.Phone))
	utils.RespondSuccess(c, http.StatusAccepted, gin.H{
		"order_id":   order.ID,
		"reference":  reference,
		"amount_tzs": order.TotalTZS,
		"payment_id": payment.ID,
		"status":     payment.Status,
	}, "Payment requested. Confirm the prompt on your phone.")
}

// applyShopOrderCallback settles shop-order references
// ("pitchtz-order-<uuid>-<ts>"). Returns true when the reference was ours.
func applyShopOrderCallback(c *gin.Context, callback services.MalipoCallback) bool {
	if !strings.HasPrefix(callback.Reference, "pitchtz-order-") {
		return false
	}
	raw := strings.TrimPrefix(callback.Reference, "pitchtz-order-")
	if len(raw) < 36 {
		return true
	}
	orderID, err := uuid.Parse(raw[:36])
	if err != nil {
		return true
	}
	settled := strings.EqualFold(callback.Status, "completed") || strings.EqualFold(callback.Status, "succeeded")
	if !settled {
		return true
	}
	now := time.Now().UTC()
	initializers.DB.WithContext(c.Request.Context()).Model(&models.ShopOrder{}).
		Where("id = ? AND status = ?", orderID, models.ShopOrderStatusPending).
		Updates(map[string]interface{}{"status": models.ShopOrderStatusPaid, "paid_at": now})
	return true
}

// MalipoPaymentCallback receives Malipo's signed settlement notifications and
// is the ONLY place a booking becomes funded. It is public by necessity, so
// the HMAC signature is what authenticates it.
func MalipoPaymentCallback(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_BODY", "could not read the callback body")
		return
	}
	if err := services.VerifyMalipoSignature(c.GetHeader("X-Malipo-Signature"), body, 5*time.Minute); err != nil {
		utils.RespondError(c, http.StatusUnauthorized, "INVALID_SIGNATURE", "callback signature is invalid")
		return
	}

	var callback services.MalipoCallback
	if err := json.Unmarshal(body, &callback); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_BODY", "callback body is not valid JSON")
		return
	}
	if callback.Type != "" && callback.Type != "payment" {
		utils.RespondSuccess(c, http.StatusOK, gin.H{"ignored": true}, "")
		return
	}

	if applyShopOrderCallback(c, callback) {
		utils.RespondSuccess(c, http.StatusOK, gin.H{"applied": true, "kind": "shop_order"}, "")
		return
	}

	var transaction models.PaymentTransaction
	if err := initializers.DB.WithContext(c.Request.Context()).
		First(&transaction, "idempotency_key = ?", callback.Reference).Error; err != nil {
		// Unknown reference: ack so Malipo stops retrying a callback we can't match.
		log.Printf("malipo callback for unknown reference %q", callback.Reference)
		utils.RespondSuccess(c, http.StatusOK, gin.H{"matched": false}, "")
		return
	}

	settled := strings.EqualFold(callback.Status, "completed") || strings.EqualFold(callback.Status, "succeeded")
	err = services.SettleShareTransaction(c.Request.Context(), transaction, settled, callback.PaymentID)
	if err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "CALLBACK_APPLY_FAILED", "could not apply the callback")
		return
	}

	utils.RespondSuccess(c, http.StatusOK, gin.H{"applied": true, "settled": settled}, "")
}

func ownerOwnsPitch(c *gin.Context, ownerID, pitchID uuid.UUID) bool {
	var pitch models.Pitch
	if err := initializers.DB.WithContext(c.Request.Context()).First(&pitch, "id = ?", pitchID).Error; err != nil {
		return false
	}
	var venue models.Venue
	if err := initializers.DB.WithContext(c.Request.Context()).First(&venue, "id = ?", pitch.VenueID).Error; err != nil {
		return false
	}
	return venue.OwnerID == ownerID
}
