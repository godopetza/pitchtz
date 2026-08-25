package initializers

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/godopetza/pitchtz/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// BootstrapOwner creates one test venue owner (with a demo city, venue, and
// pitch to book against) for local/staging testing. It runs at most once:
// once any owner credential exists, BOOTSTRAP_OWNER_* is ignored.
func BootstrapOwner() (bool, error) {
	email := strings.ToLower(strings.TrimSpace(os.Getenv("BOOTSTRAP_OWNER_EMAIL")))
	password := os.Getenv("BOOTSTRAP_OWNER_PASSWORD")
	name := strings.TrimSpace(os.Getenv("BOOTSTRAP_OWNER_NAME"))
	if email == "" && password == "" && name == "" {
		return false, nil
	}
	if DB == nil {
		return false, fmt.Errorf("bootstrap owner requires a database")
	}
	if email == "" || password == "" {
		return false, fmt.Errorf("BOOTSTRAP_OWNER_EMAIL and BOOTSTRAP_OWNER_PASSWORD must both be set")
	}
	if len(password) < 12 {
		return false, fmt.Errorf("BOOTSTRAP_OWNER_PASSWORD must be at least 12 characters")
	}
	if name == "" {
		name = "Test Venue Owner"
	}

	var ownerCount int64
	if err := DB.Model(&models.OwnerCredential{}).Count(&ownerCount).Error; err != nil {
		return false, fmt.Errorf("count owners: %w", err)
	}
	if ownerCount > 0 {
		return false, nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return false, fmt.Errorf("hash bootstrap password: %w", err)
	}
	now := time.Now().UTC()

	err = DB.Transaction(func(tx *gorm.DB) error {
		var existing int64
		if err := tx.Model(&models.User{}).Where("LOWER(email) = ?", email).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return fmt.Errorf("a user with BOOTSTRAP_OWNER_EMAIL already exists")
		}

		user := models.User{Email: &email, Name: name, AuthProvider: "password", Language: "en", Role: "owner"}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		credential := models.OwnerCredential{UserID: user.ID, Status: models.OwnerStatusActive, PasswordHash: string(hash), MustChangePassword: false, PasswordChangedAt: &now}
		if err := tx.Create(&credential).Error; err != nil {
			return err
		}

		var city models.City
		if err := tx.Order("created_at ASC").First(&city).Error; err != nil {
			city = models.City{Name: "Dar es Salaam", Status: models.CityStatusLive, Latitude: -6.7924, Longitude: 39.2083}
			if err := tx.Create(&city).Error; err != nil {
				return err
			}
		}

		venue := models.Venue{
			OwnerID: user.ID, CityID: city.ID, Name: "Test Venue (bootstrap)", Area: "Masaki",
			Latitude: -6.7500, Longitude: 39.2700, Status: models.VenueStatusActive, Verified: true,
			Amenities: datatypes.JSON([]byte("[]")), Rules: datatypes.JSON([]byte("[]")),
		}
		if err := tx.Create(&venue).Error; err != nil {
			return err
		}
		pitch := models.Pitch{
			VenueID: venue.ID, Name: "Pitch A", Format: "5-a-side", Surface: "artificial_turf",
			BasePriceTZS: 40000, OpenHours: datatypes.JSON([]byte("{}")),
		}
		return tx.Create(&pitch).Error
	})
	if err != nil {
		return false, fmt.Errorf("create bootstrap owner: %w", err)
	}
	return true, nil
}
