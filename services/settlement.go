package services

import (
	"context"
	"time"

	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SettleShareTransaction is the single place a payment outcome is applied —
// used by both the Malipo webhook and the reconciler, so a lost callback and
// a live callback produce identical state.
func SettleShareTransaction(ctx context.Context, transaction models.PaymentTransaction, settled bool, providerRef string) error {
	now := time.Now().UTC()
	var confirmedBookingID *uuid.UUID
	err := initializers.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		status := "failed"
		if settled {
			status = "completed"
		}
		updates := map[string]interface{}{"status": status, "webhook_at": now}
		if providerRef != "" {
			updates["provider_ref"] = providerRef
		}
		if err := tx.Model(&models.PaymentTransaction{}).Where("id = ?", transaction.ID).
			Updates(updates).Error; err != nil {
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
			if booking.Status != models.BookingStatusConfirmed {
				id := booking.ID
				confirmedBookingID = &id
			}
		}
		// A hold that expired while the charge was in flight is revived here:
		// the customer paid, so the customer keeps the slot.
		return tx.Model(&models.Booking{}).Where("id = ?", booking.ID).
			Updates(map[string]interface{}{"status": next, "cancelled_at": nil}).Error
	})
	if err == nil && confirmedBookingID != nil {
		go NotifyBookingConfirmed(*confirmedBookingID)
	}
	return err
}

// ReconcilePendingCharges actively asks Malipo about charges stuck "pending"
// for over three minutes — the safety net for lost callbacks, so a customer
// who was charged always ends up with a confirmed slot (or, if the charge
// truly failed, the hold is released for everyone else).
func ReconcilePendingCharges() {
	if initializers.DB == nil || !MalipoConfigured() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var stale []models.PaymentTransaction
	initializers.DB.WithContext(ctx).
		Where("status = ? AND direction = ? AND provider_ref <> '' AND updated_at < ?",
			"pending", "charge", time.Now().UTC().Add(-3*time.Minute)).
		Limit(20).Find(&stale)

	for _, transaction := range stale {
		payment, err := RecheckMalipoPayment(ctx, transaction.ProviderRef)
		if err != nil {
			continue
		}
		switch payment.Status {
		case "completed", "succeeded":
			_ = SettleShareTransaction(ctx, transaction, true, payment.ID)
		case "failed", "cancelled", "rejected", "expired":
			_ = SettleShareTransaction(ctx, transaction, false, payment.ID)
		}
	}
}
