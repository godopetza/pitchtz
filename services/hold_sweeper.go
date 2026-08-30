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
	result := initializers.DB.Exec(`
		UPDATE bookings SET status = 'cancelled', cancelled_at = NOW(), cancel_reason = 'hold_expired'
		WHERE status = 'pending'
		  AND created_at < ?
		  AND id NOT IN (SELECT booking_id FROM payment_shares WHERE status = 'paid')
		  AND id NOT IN (
		    SELECT ps.booking_id FROM payment_shares ps
		    JOIN payment_transactions pt ON pt.share_id = ps.id
		    WHERE pt.status IN ('pending', 'completed')
		  )`, cutoff)
	if result.Error != nil {
		log.Printf("hold sweeper: %v", result.Error)
		return 0
	}
	if result.RowsAffected > 0 {
		log.Printf("hold sweeper: released %d expired holds", result.RowsAffected)
	}
	return result.RowsAffected
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
