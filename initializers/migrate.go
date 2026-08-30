package initializers

import (
	"fmt"
	"log"

	"github.com/godopetza/pitchtz/models"
	"gorm.io/gorm"
)

func SyncDatabase() error {
	if DB == nil {
		return fmt.Errorf("database is not connected")
	}

	if err := DB.Exec("CREATE EXTENSION IF NOT EXISTS pgcrypto").Error; err != nil {
		return fmt.Errorf("enable pgcrypto: %w", err)
	}

	if err := DB.Exec("CREATE EXTENSION IF NOT EXISTS btree_gist").Error; err != nil {
		return fmt.Errorf("enable btree_gist: %w", err)
	}

	if err := DB.AutoMigrate(
		&models.City{},
		&models.User{},
		&models.CityWaitlist{},
		&models.Venue{},
		&models.Pitch{},
		&models.SlotBlock{},
		&models.VenuePhoto{},
		&models.ExtraCatalog{},
		&models.Booking{},
		&models.BookingExtra{},
		&models.PaymentShare{},
		&models.PaymentTransaction{},
		&models.Review{},
		&models.Favorite{},
		&models.League{},
		&models.Team{},
		&models.Challenge{},
		&models.PitchPhoto{},
		&models.Fixture{},
		&models.FavoriteTeam{},
		&models.WatchSpot{},
		&models.TeamMember{},
		&models.Match{},
		&models.Standing{},
		&models.Tournament{},
		&models.TournamentEntry{},
		&models.Promotion{},
		&models.AdminStaff{},
		&models.AdminCredential{},
		&models.OwnerCredential{},
		&models.DeviceToken{},
		&models.PasswordResetToken{},
		&models.EmailOTP{},
		&models.ShopProduct{},
		&models.ShopOrder{},
		&models.ShopOrderItem{},
		&models.CareerApplication{},
		&models.AuditLog{},
		&models.VenuePayoutSetting{},
		&models.Payout{},
		&models.PayoutItem{},
		&models.Dispute{},
		&models.Notification{},
	); err != nil {
		return fmt.Errorf("automigrate: %w", err)
	}

	if err := ensureBookingConstraints(DB); err != nil {
		return err
	}
	return backfillBookingContacts(DB)
}

// backfillBookingContacts copies each existing booking's account name and
// phone onto the booking itself, once, so a call desk can reach the people
// who booked before contacts were stored. Only fills blanks, so it never
// overwrites a contact captured at booking time and is safe to re-run.
func backfillBookingContacts(db *gorm.DB) error {
	result := db.Exec(`UPDATE bookings SET
			contact_name = COALESCE(NULLIF(bookings.contact_name, ''), COALESCE(users.name, '')),
			contact_phone = COALESCE(NULLIF(bookings.contact_phone, ''), COALESCE(users.phone, ''))
		FROM users
		WHERE users.id = bookings.user_id
		  AND (bookings.contact_phone = '' OR bookings.contact_name = '')`)
	if result.Error != nil {
		return fmt.Errorf("backfill booking contacts: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		log.Printf("backfilled contact details on %d bookings", result.RowsAffected)
	}
	return nil
}

// ensureBookingConstraints adds a GiST exclusion constraint that rejects any
// booking whose [starts_at, ends_at) range overlaps an existing active
// booking on the same pitch, at the database level. AutoMigrate cannot
// express this, so it runs once here, idempotently, after every migrate.
func ensureBookingConstraints(db *gorm.DB) error {
	const constraintName = "bookings_no_overlap"
	var exists bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = ?)", constraintName).Scan(&exists).Error; err != nil {
		return fmt.Errorf("check booking exclusion constraint: %w", err)
	}
	if exists {
		return nil
	}
	sql := fmt.Sprintf(`ALTER TABLE bookings
		ADD CONSTRAINT %s
		EXCLUDE USING gist (
			pitch_id WITH =,
			tstzrange(starts_at, ends_at) WITH &&
		)
		WHERE (status IN ('pending', 'confirmed', 'part_paid'))`, constraintName)
	if err := db.Exec(sql).Error; err != nil {
		return fmt.Errorf("add booking exclusion constraint: %w", err)
	}
	return nil
}
