package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/utils"
	"gorm.io/gorm"
)

type createReviewInput struct {
	Stars int    `json:"stars" binding:"required,min=1,max=5"`
	Text  string `json:"text" binding:"max=2000"`
}

// CreateVenueReview lets a signed-in customer review a venue — but only once
// they have actually played there: every review is anchored to one of their
// funded bookings (Review.BookingID is unique, so one review per booking).
func CreateVenueReview(c *gin.Context) {
	if initializers.DB == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "DATABASE_REQUIRED", "reviews are unavailable")
		return
	}
	userID, ok := clientUserID(c)
	if !ok {
		return
	}
	venueID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input createReviewInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "stars (1-5) is required; text is optional")
		return
	}

	var bookings []models.Booking
	initializers.DB.WithContext(c.Request.Context()).
		Joins("JOIN pitches ON pitches.id = bookings.pitch_id").
		Where("pitches.venue_id = ? AND bookings.user_id = ?", venueID, userID).
		Where("bookings.status IN ?", []string{models.BookingStatusConfirmed, models.BookingStatusCompleted}).
		Order("bookings.starts_at DESC").
		Find(&bookings)
	if len(bookings) == 0 {
		utils.RespondError(c, http.StatusForbidden, "REVIEW_REQUIRES_BOOKING", "you can review this venue after you have played a booked game here")
		return
	}

	var review *models.Review
	err := initializers.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		for _, booking := range bookings {
			var existing models.Review
			if tx.First(&existing, "booking_id = ?", booking.ID).Error == nil {
				continue
			}
			review = &models.Review{
				BookingID: booking.ID,
				UserID:    userID,
				VenueID:   venueID,
				Stars:     input.Stars,
				Text:      strings.TrimSpace(input.Text),
			}
			if err := tx.Create(review).Error; err != nil {
				return err
			}
			// Keep the venue's headline rating honest: plain average of stars.
			return tx.Exec(
				"UPDATE venues SET rating = (SELECT ROUND(AVG(stars)::numeric, 1) FROM reviews WHERE venue_id = ?) WHERE id = ?",
				venueID, venueID,
			).Error
		}
		return nil
	})
	if err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "REVIEW_SAVE_FAILED", "could not save the review")
		return
	}
	if review == nil {
		utils.RespondError(c, http.StatusConflict, "ALREADY_REVIEWED", "you have already reviewed all your bookings at this venue")
		return
	}
	utils.RespondSuccess(c, http.StatusCreated, review, "Asante! Your review is live.")
}
