package services

import (
	"context"
	"strings"
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
	var slotLostBookingID *uuid.UUID
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
		// A deposit with balance-at-venue confirms the slot: the venue
		// collects the rest in cash at the gate.
		if booking.BalanceAtVenue && paidTotal > 0 {
			next = models.BookingStatusConfirmed
			if booking.Status != models.BookingStatusConfirmed {
				id := booking.ID
				confirmedBookingID = &id
			}
		}
		if paidTotal >= booking.TotalTZS {
			next = models.BookingStatusConfirmed
			if booking.Status != models.BookingStatusConfirmed {
				id := booking.ID
				confirmedBookingID = &id
			}
		}
		// A hold that expired while the charge was in flight is revived here:
		// the customer paid, so the customer keeps the slot. Malipo honours a
		// late COMPLETED after a FAILED, so by now the slot may already belong
		// to somebody else — reviving it would trip bookings_no_overlap, roll
		// this whole transaction back and lose the record of money we took.
		if booking.Status == models.BookingStatusCancelled {
			var taken int64
			tx.Model(&models.Booking{}).
				Where(`pitch_id = ? AND id <> ? AND status IN ? AND starts_at < ? AND ends_at > ?`,
					booking.PitchID, booking.ID,
					[]string{models.BookingStatusPending, models.BookingStatusConfirmed, models.BookingStatusPartPaid},
					booking.EndsAt, booking.StartsAt).
				Count(&taken)
			if taken > 0 {
				// Keep the payment on record and leave the booking cancelled.
				// Support refunds from here; silently failing the callback
				// would strand a customer who has already been debited.
				lost := booking.ID
				slotLostBookingID = &lost
				return tx.Model(&models.Booking{}).Where("id = ?", booking.ID).
					Updates(map[string]interface{}{
						"cancel_reason": "slot_lost_after_payment",
						"cancel_detail": "payment settled after the hold lapsed and the slot had been rebooked — refund due",
					}).Error
			}
		}
		return tx.Model(&models.Booking{}).Where("id = ?", booking.ID).
			Updates(map[string]interface{}{"status": next, "cancelled_at": nil}).Error
	})
	if err == nil && confirmedBookingID != nil {
		go NotifyBookingConfirmed(*confirmedBookingID)
	}
	if err == nil && slotLostBookingID != nil {
		go notifySlotLostAfterPayment(*slotLostBookingID)
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

// MarkBookingPaymentFailed notes on the booking why its payment did not go
// through. It never changes status — a failed attempt is not a dead booking,
// the player can try again until the hold lapses — it only records the reason
// so the desk can explain it if they call.
func MarkBookingPaymentFailed(ctx context.Context, shareID uuid.UUID, reason string) {
	if initializers.DB == nil || strings.TrimSpace(reason) == "" {
		return
	}
	var share models.PaymentShare
	if initializers.DB.WithContext(ctx).First(&share, "id = ?", shareID).Error != nil {
		return
	}
	initializers.DB.WithContext(ctx).Model(&models.Booking{}).
		Where("id = ? AND status = ?", share.BookingID, models.BookingStatusPending).
		Updates(map[string]any{"cancel_reason": "payment_failed", "cancel_detail": reason})
}

// notifySlotLostAfterPayment tells ops that money landed for a slot somebody
// else now holds. Mobile money callbacks arrive out of order and Malipo
// honours a late COMPLETED after a FAILED, so this is rare but real — and it
// always needs a human, because the customer has been debited for nothing.
func notifySlotLostAfterPayment(bookingID uuid.UUID) {
	admin := adminNotifyEmail()
	if admin == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var booking models.Booking
	if initializers.DB.WithContext(ctx).First(&booking, "id = ?", bookingID).Error != nil {
		return
	}
	var paid int64
	initializers.DB.WithContext(ctx).Model(&models.PaymentShare{}).
		Where("booking_id = ? AND status = ?", bookingID, "paid").
		Select("COALESCE(SUM(amount_tzs), 0)").Scan(&paid)

	facts := factRows([][2]string{
		{"Booking", booking.Code},
		{"Paid", formatTZS(paid)},
		{"Slot", booking.StartsAt.In(eatZone()).Format("Mon 2 Jan 15:04")},
		{"Contact", booking.ContactPhone},
		{"Action", "refund the payer — the slot was rebooked"},
	})
	body := `<p style="margin:0 0 12px">A payment settled after its hold had lapsed, and the slot had already gone to somebody else. The money is recorded against the booking, which stays cancelled.</p>` + facts
	sendBranded(ctx, admin, "Refund due: payment landed on a lost slot",
		"A payment settled after the hold lapsed and the slot was rebooked. Booking "+booking.Code+" needs a refund.",
		brandedEmail{
			Preheader: "A paid slot was already taken", Eyebrow: "Ops alert",
			Title: "A payment needs refunding.", BodyHTML: body,
			ActionLabel: "Open Superadmin", ActionURL: adminAppURL(),
			Footnote: "Sent once per affected booking.",
		},
		"slot-lost-after-payment-"+bookingID.String())
}
