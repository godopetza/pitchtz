package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/utils"
	"github.com/google/uuid"
)

type reviewReplyInput struct {
	Text string `json:"text" binding:"required,min=2,max=1000"`
}

// OwnerReplyToReview lets the venue owner answer a player review publicly.
func OwnerReplyToReview(c *gin.Context) {
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	reviewID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input reviewReplyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "text is required (2-1000 characters)")
		return
	}
	var review models.Review
	if err := initializers.DB.WithContext(c.Request.Context()).First(&review, "id = ?", reviewID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "REVIEW_NOT_FOUND", "review was not found")
		return
	}
	var venue models.Venue
	if err := initializers.DB.WithContext(c.Request.Context()).First(&venue, "id = ?", review.VenueID).Error; err != nil || venue.OwnerID != ownerID {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_VENUE", "this review is not for your venue")
		return
	}
	now := time.Now().UTC()
	initializers.DB.WithContext(c.Request.Context()).Model(&models.Review{}).Where("id = ?", review.ID).
		Updates(map[string]interface{}{"owner_reply": strings.TrimSpace(input.Text), "replied_at": now})
	utils.RespondSuccess(c, http.StatusOK, gin.H{"replied": true}, "Reply posted.")
}

// OwnerListVenues returns every venue on the owner's account, any status —
// the dashboard onboarding state needs to show pending applications too.
func OwnerListVenues(c *gin.Context) {
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	var venues []models.Venue
	initializers.DB.WithContext(c.Request.Context()).
		Select("id, name, area, status, created_at").
		Where("owner_id = ?", ownerID).Order("created_at ASC").Find(&venues)
	items := make([]gin.H, 0, len(venues))
	for _, venue := range venues {
		items = append(items, gin.H{"id": venue.ID, "name": venue.Name, "area": venue.Area, "status": venue.Status})
	}
	utils.RespondSuccess(c, http.StatusOK, items, "")
}

type ownerPitchInput struct {
	Name        string `json:"name" binding:"required,min=1,max=120"`
	Format      string `json:"format" binding:"required,min=2,max=40"`
	PriceTZS    int64  `json:"price_tzs" binding:"required,gt=0"`
	PhotoR2Key  string `json:"photo_r2_key" binding:"max=200"`
	Surface     string `json:"surface" binding:"max=40"`
}

func ownerOwnsVenue(c *gin.Context, ownerID uuid.UUID, venueID uuid.UUID) bool {
	var venue models.Venue
	if err := initializers.DB.WithContext(c.Request.Context()).First(&venue, "id = ?", venueID).Error; err != nil {
		return false
	}
	return venue.OwnerID == ownerID
}

// OwnerCreatePitch lets an owner add a field to their venue: name, format,
// hourly price, and its own photo.
func OwnerCreatePitch(c *gin.Context) {
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	venueID, ok := parseID(c, "id")
	if !ok {
		return
	}
	if !ownerOwnsVenue(c, ownerID, venueID) {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_VENUE", "this venue is not on your account")
		return
	}
	var input ownerPitchInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "name, format, and price_tzs are required")
		return
	}
	surface := strings.TrimSpace(input.Surface)
	if surface == "" {
		surface = "artificial_turf"
	}
	pitch := models.Pitch{
		VenueID: venueID, Name: strings.TrimSpace(input.Name), Format: strings.TrimSpace(input.Format),
		Surface: surface, BasePriceTZS: input.PriceTZS, PhotoR2Key: strings.TrimSpace(input.PhotoR2Key),
	}
	if err := initializers.DB.WithContext(c.Request.Context()).Create(&pitch).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "PITCH_CREATE_FAILED", "could not create the pitch")
		return
	}
	utils.RespondSuccess(c, http.StatusCreated, pitch, "Pitch added.")
}

// OwnerUpdatePitch adjusts price, name, or photo for one of the owner's pitches.
func OwnerUpdatePitch(c *gin.Context) {
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	pitchID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var pitch models.Pitch
	if err := initializers.DB.WithContext(c.Request.Context()).First(&pitch, "id = ?", pitchID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "PITCH_NOT_FOUND", "pitch was not found")
		return
	}
	if !ownerOwnsVenue(c, ownerID, pitch.VenueID) {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_VENUE", "this pitch is not on your account")
		return
	}
	var input ownerPitchInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "name, format, and price_tzs are required")
		return
	}
	updates := map[string]interface{}{
		"name": strings.TrimSpace(input.Name), "format": strings.TrimSpace(input.Format),
		"base_price_tzs": input.PriceTZS,
	}
	if strings.TrimSpace(input.PhotoR2Key) != "" {
		updates["photo_r2_key"] = strings.TrimSpace(input.PhotoR2Key)
	}
	initializers.DB.WithContext(c.Request.Context()).Model(&models.Pitch{}).Where("id = ?", pitchID).Updates(updates)
	utils.RespondSuccess(c, http.StatusOK, gin.H{"updated": true}, "Pitch updated.")
}

// OwnerVenueBookings feeds the live dashboard: today's and upcoming bookings
// for one of the owner's venues, with paid totals.
func OwnerVenueBookings(c *gin.Context) {
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	venueID, ok := parseID(c, "id")
	if !ok {
		return
	}
	if !ownerOwnsVenue(c, ownerID, venueID) {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_VENUE", "this venue is not on your account")
		return
	}
	type row struct {
		models.Booking
		PitchName    string
		CustomerName string
		PaidTZS      int64
	}
	var rows []row
	initializers.DB.WithContext(c.Request.Context()).
		Table("bookings").
		Select(`bookings.*, pitches.name AS pitch_name,
			COALESCE(users.name, '') AS customer_name,
			COALESCE((SELECT SUM(amount_tzs) FROM payment_shares WHERE payment_shares.booking_id = bookings.id AND payment_shares.status = 'paid'), 0) AS paid_tzs`).
		Joins("JOIN pitches ON pitches.id = bookings.pitch_id").
		Joins("LEFT JOIN users ON users.id = bookings.user_id").
		Where("pitches.venue_id = ? AND bookings.ends_at >= ? AND bookings.status <> ?",
			venueID, time.Now().UTC().Add(-24*time.Hour), models.BookingStatusCancelled).
		Order("bookings.starts_at ASC").Limit(40).Scan(&rows)

	items := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		items = append(items, gin.H{
			"id": r.ID, "code": r.Code, "pitch": r.PitchName, "customer": r.CustomerName,
			"starts_at": r.StartsAt, "ends_at": r.EndsAt, "status": r.Status,
			"total_tzs": r.TotalTZS, "paid_tzs": r.PaidTZS,
		})
	}
	utils.RespondSuccess(c, http.StatusOK, items, "")
}
