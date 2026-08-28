package initializers

import (
	"fmt"

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
		&models.TeamMember{},
		&models.Match{},
		&models.Standing{},
		&models.Tournament{},
		&models.TournamentEntry{},
		&models.Promotion{},
		&models.AdminStaff{},
		&models.AdminCredential{},
		&models.OwnerCredential{},
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

	return ensureBookingConstraints(DB)
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
