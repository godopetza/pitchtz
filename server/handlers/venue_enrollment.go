package handlers

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/services"
	"github.com/godopetza/pitchtz/utils"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type enrollVenueInput struct {
	VenueName  string `json:"venue_name" binding:"required,min=2,max=120"`
	Area       string `json:"area" binding:"required,min=2,max=120"`
	CityID     string `json:"city_id" binding:"required"`
	OwnerName  string `json:"owner_name" binding:"required,min=2,max=120"`
	OwnerEmail string `json:"owner_email" binding:"required,email,max=254"`
	OwnerPhone string `json:"owner_phone" binding:"required,max=32"`
}

// EnrollVenue is the public "list your venue" application. It does not grant
// any access on its own: it creates a Venue in "pending" status and an owner
// User with no credential, awaiting admin review via ApproveVenue.
func (h *PublicAPI) EnrollVenue(c *gin.Context) {
	if initializers.DB == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "DATABASE_REQUIRED", "venue enrollment is unavailable")
		return
	}
	var input enrollVenueInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "venue_name, area, city_id, owner_name, owner_email, and owner_phone are required")
		return
	}
	cityID, err := uuid.Parse(input.CityID)
	if err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_CITY_ID", "city_id must be a UUID")
		return
	}
	email, err := utils.NormalizeEmail(input.OwnerEmail)
	if err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_EMAIL", "owner_email is invalid")
		return
	}
	phone, err := utils.NormalizeTZPhone(input.OwnerPhone)
	if err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_PHONE", "owner_phone must be a valid Tanzanian mobile number")
		return
	}

	var venue models.Venue
	err = initializers.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var city models.City
		if err := tx.First(&city, "id = ?", cityID).Error; err != nil {
			return errInvalidCity
		}

		var owner models.User
		err := tx.Where("LOWER(email) = ?", email).First(&owner).Error
		if err != nil {
			owner = models.User{Email: &email, Phone: &phone, Name: strings.TrimSpace(input.OwnerName), AuthProvider: "pending", Language: "en", Role: "owner"}
			if err := tx.Create(&owner).Error; err != nil {
				return err
			}
		}

		venue = models.Venue{
			OwnerID: owner.ID, CityID: cityID, Name: strings.TrimSpace(input.VenueName), Area: strings.TrimSpace(input.Area),
			Status: models.VenueStatusPending, Amenities: datatypes.JSON([]byte("[]")), Rules: datatypes.JSON([]byte("[]")),
		}
		return tx.Create(&venue).Error
	})
	if err != nil {
		if errors.Is(err, errInvalidCity) {
			utils.RespondError(c, http.StatusBadRequest, "INVALID_CITY", "city does not exist")
			return
		}
		utils.RespondError(c, http.StatusInternalServerError, "ENROLLMENT_FAILED", "could not submit the venue application")
		return
	}
	c.Header("Cache-Control", "no-store")
	utils.RespondSuccess(c, http.StatusAccepted, gin.H{
		"venue_id": venue.ID, "status": venue.Status,
	}, "application received; our team will review it shortly")
}

var errInvalidCity = errors.New("invalid city")

type pendingVenueDTO struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Area       string    `json:"area"`
	Status     string    `json:"status"`
	OwnerName  string    `json:"owner_name"`
	OwnerEmail string    `json:"owner_email"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListVenuesForAdmin lists venues for staff review, optionally filtered by
// status (e.g. ?status=pending for the enrollment queue).
func ListVenuesForAdmin(c *gin.Context) {
	query := initializers.DB.WithContext(c.Request.Context()).Model(&models.Venue{}).Order("created_at DESC")
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	var venues []models.Venue
	if err := query.Find(&venues).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "VENUE_LIST_FAILED", "could not load venues")
		return
	}
	dtos := make([]pendingVenueDTO, 0, len(venues))
	for _, venue := range venues {
		var owner models.User
		initializers.DB.WithContext(c.Request.Context()).First(&owner, "id = ?", venue.OwnerID)
		email := ""
		if owner.Email != nil {
			email = *owner.Email
		}
		dtos = append(dtos, pendingVenueDTO{
			ID: venue.ID, Name: venue.Name, Area: venue.Area, Status: venue.Status,
			OwnerName: owner.Name, OwnerEmail: email, CreatedAt: venue.CreatedAt,
		})
	}
	utils.RespondSuccess(c, http.StatusOK, dtos, "")
}

// ApproveVenue activates a pending venue and, if the owner has no login yet,
// creates one with a fresh temporary password for staff to relay securely —
// the same pattern CreateAdmin uses for new staff accounts.
func ApproveVenue(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var venue models.Venue
	if err := initializers.DB.WithContext(c.Request.Context()).First(&venue, "id = ?", id).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "VENUE_NOT_FOUND", "venue was not found")
		return
	}

	var owner models.User
	if err := initializers.DB.WithContext(c.Request.Context()).First(&owner, "id = ?", venue.OwnerID).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "OWNER_NOT_FOUND", "venue owner was not found")
		return
	}

	var temporaryPassword string
	err := initializers.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&venue).Updates(map[string]interface{}{"status": models.VenueStatusActive, "verified": true}).Error; err != nil {
			return err
		}
		var existing int64
		tx.Model(&models.OwnerCredential{}).Where("user_id = ?", owner.ID).Count(&existing)
		if existing > 0 {
			return nil
		}
		var err error
		temporaryPassword, err = randomPassword()
		if err != nil {
			return err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(temporaryPassword), 12)
		if err != nil {
			return err
		}
		credential := models.OwnerCredential{UserID: owner.ID, Status: models.OwnerStatusActive, PasswordHash: string(hash), MustChangePassword: true}
		return tx.Create(&credential).Error
	})
	if err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "VENUE_APPROVE_FAILED", "could not approve the venue")
		return
	}

	response := gin.H{"venue_id": venue.ID, "status": models.VenueStatusActive, "owner_email": owner.Email}
	message := "Venue approved."
	if temporaryPassword != "" {
		response["temporary_password"] = temporaryPassword
		message = "Venue approved. Share the temporary password with the owner securely."
		if owner.Email != nil {
			portalURL := strings.TrimRight(strings.TrimSpace(os.Getenv("OWNER_APP_URL")), "/")
			if portalURL == "" {
				portalURL = "http://localhost:3002"
			}
			if err := services.SendWelcomeAccess(c.Request.Context(), *owner.Email, owner.Name, temporaryPassword, portalURL, "owner", "welcome-owner-"+owner.ID.String()); err != nil {
				log.Printf("owner welcome email failed for user %s: %v", owner.ID, err)
			}
		}
	}
	utils.RespondSuccess(c, http.StatusOK, response, message)
}

func randomPassword() (string, error) {
	buf := make([]byte, 15)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}
