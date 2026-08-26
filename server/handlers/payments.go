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
	"gorm.io/gorm"
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
	transaction := models.PaymentTransaction{
		Provider:  input.Provider,
		AmountTZS: amount,
		Direction: "charge",
		Status:    "initiated",
	}

	// Reference is stable per share, so a retry hits Malipo's idempotency.
	err := initializers.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&share).Error; err != nil {
			return err
		}
		transaction.ShareID = share.ID
		transaction.IdempotencyKey = fmt.Sprintf("pitchtz-share-%s", share.ID)
		return tx.Create(&transaction).Error
	})
	if err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "PAYMENT_INIT_FAILED", "could not start the payment")
		return
	}

	payment, err := services.CreateMalipoPayment(c.Request.Context(), services.MalipoPaymentRequest{
		Provider:    input.Provider,
		Amount:      amount,
		Currency:    "TZS",
		Reference:   transaction.IdempotencyKey,
		Description: fmt.Sprintf("PitchTZ booking %s", booking.Code),
		Phone:       strings.TrimSpace(input.Phone),
		Operator:    strings.TrimSpace(input.Operator),
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

	utils.RespondSuccess(c, http.StatusAccepted, gin.H{
		"share_id":     share.ID,
		"reference":    transaction.IdempotencyKey,
		"amount_tzs":   amount,
		"payment_id":   payment.ID,
		"status":       payment.Status,
		"checkout_url": payment.CheckoutURL,
	}, "Payment requested. The booking confirms once Malipo reports settlement.")
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

	var transaction models.PaymentTransaction
	if err := initializers.DB.WithContext(c.Request.Context()).
		First(&transaction, "idempotency_key = ?", callback.Reference).Error; err != nil {
		// Unknown reference: ack so Malipo stops retrying a callback we can't match.
		log.Printf("malipo callback for unknown reference %q", callback.Reference)
		utils.RespondSuccess(c, http.StatusOK, gin.H{"matched": false}, "")
		return
	}

	now := time.Now().UTC()
	settled := strings.EqualFold(callback.Status, "completed") || strings.EqualFold(callback.Status, "succeeded")

	err = initializers.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		status := "failed"
		if settled {
			status = "completed"
		}
		if err := tx.Model(&models.PaymentTransaction{}).Where("id = ?", transaction.ID).
			Updates(map[string]interface{}{"status": status, "webhook_at": now, "provider_ref": callback.PaymentID}).Error; err != nil {
			return err
		}
		if !settled {
			return nil
		}
		if err := tx.Model(&models.PaymentShare{}).Where("id = ?", transaction.ShareID).
			Updates(map[string]interface{}{"status": "paid", "paid_at": now}).Error; err != nil {
			return err
		}

		var share models.PaymentShare
		if err := tx.First(&share, "id = ?", transaction.ShareID).Error; err != nil {
			return err
		}
		// Confirm the booking once its paid shares cover the total.
		var paidTotal int64
		tx.Model(&models.PaymentShare{}).Where("booking_id = ? AND status = ?", share.BookingID, "paid").
			Select("COALESCE(SUM(amount_tzs), 0)").Scan(&paidTotal)

		var booking models.Booking
		if err := tx.First(&booking, "id = ?", share.BookingID).Error; err != nil {
			return err
		}
		next := models.BookingStatusPartPaid
		if paidTotal >= booking.TotalTZS {
			next = models.BookingStatusConfirmed
		}
		return tx.Model(&models.Booking{}).Where("id = ?", booking.ID).Update("status", next).Error
	})
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
