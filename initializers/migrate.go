package initializers

import (
	"fmt"

	"github.com/godopetza/pitchtz/models"
)

func SyncDatabase() error {
	if DB == nil {
		return fmt.Errorf("database is not connected")
	}

	if err := DB.Exec("CREATE EXTENSION IF NOT EXISTS pgcrypto").Error; err != nil {
		return fmt.Errorf("enable pgcrypto: %w", err)
	}

	return DB.AutoMigrate(
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
		&models.TeamMember{},
		&models.Match{},
		&models.Standing{},
		&models.Tournament{},
		&models.TournamentEntry{},
		&models.Promotion{},
		&models.AdminStaff{},
		&models.VenuePayoutSetting{},
		&models.Payout{},
		&models.PayoutItem{},
		&models.Dispute{},
		&models.Notification{},
	)
}
