package services

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/godopetza/pitchtz/initializers"
)

// BookingHoldWindow is how long an unpaid pending booking keeps its slot
// before the system releases it for everyone else. The client UI's "your slot
// is held for N minutes" copy must match this.
func BookingHoldWindow() time.Duration {
	if value := os.Getenv("BOOKING_HOLD_MINUTES"); value != "" {
		if minutes, err := strconv.Atoi(value); err == nil && minutes > 0 {
			return time.Duration(minutes) * time.Minute
		}
	}
	return 10 * time.Minute
}

// ReleaseExpiredHolds cancels pending bookings whose hold lapsed with zero
// payment activity — no paid share and no in-flight mobile money prompt.
// Cancelled bookings fall outside the DB exclusion constraint, so the slot
// instantly becomes bookable again on every platform.
func ReleaseExpiredHolds() int64 {
	if initializers.DB == nil {
		return 0
	}
	cutoff := time.Now().UTC().Add(-BookingHoldWindow())

	// A lapsed hold has two very different stories behind it, and the desk
	// needs to tell them apart: nobody ever pressed pay (a browsing dropout),
	// or somebody tried and their money never arrived (a payments problem).
	// The eligibility clause is identical; only the explanation differs.
	const eligible = `
		WHERE status = 'pending'
		  AND created_at < ?
		  AND id NOT IN (SELECT booking_id FROM payment_shares WHERE status = 'paid')
		  AND id NOT IN (
		    SELECT ps.booking_id FROM payment_shares ps
		    JOIN payment_transactions pt ON pt.share_id = ps.id
		    WHERE pt.status IN ('pending', 'completed')
		  )`

	const anyAttempt = `
		  AND EXISTS (
		    SELECT 1 FROM payment_shares ps
		    JOIN payment_transactions pt ON pt.share_id = ps.id
		    WHERE ps.booking_id = bookings.id
		  )`

	// Tried and failed: name the operator they used, so "why did my M-Pesa
	// not go through?" has an answer without digging through logs.
	tried := initializers.DB.Exec(`
		UPDATE bookings SET status = 'cancelled', cancelled_at = NOW(),
			cancel_reason = 'hold_expired_after_attempt',
			cancel_detail = COALESCE((
				SELECT NULLIF(TRIM(COALESCE(pt.failure_reason, '')), '')
				FROM payment_shares ps
				JOIN payment_transactions pt ON pt.share_id = ps.id
				WHERE ps.booking_id = bookings.id
				ORDER BY pt.created_at DESC LIMIT 1
			), cancel_detail, '')`+eligible+anyAttempt, cutoff)
	if tried.Error != nil {
		log.Printf("hold sweeper (attempted): %v", tried.Error)
	}

	// Never attempted: the slot was held and simply abandoned.
	untouched := initializers.DB.Exec(`
		UPDATE bookings SET status = 'cancelled', cancelled_at = NOW(),
			cancel_reason = 'hold_expired_no_attempt'`+eligible+`
		  AND NOT EXISTS (
		    SELECT 1 FROM payment_shares ps
		    JOIN payment_transactions pt ON pt.share_id = ps.id
		    WHERE ps.booking_id = bookings.id
		  )`, cutoff)
	if untouched.Error != nil {
		log.Printf("hold sweeper (never attempted): %v", untouched.Error)
	}

	released := tried.RowsAffected + untouched.RowsAffected
	if released > 0 {
		log.Printf("hold sweeper: released %d expired holds (%d after a failed payment, %d never attempted)",
			released, tried.RowsAffected, untouched.RowsAffected)
	}
	return released
}

// StartHoldSweeper runs ReleaseExpiredHolds once a minute for the life of the
// process.
func StartHoldSweeper() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			ReconcilePendingCharges()
			ReleaseExpiredHolds()
		}
	}()
}
