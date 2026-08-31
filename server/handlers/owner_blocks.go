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

// BlockPitchSlot — POST /v1/owner/pitches/:id/blocks
// Closes a window on one pitch: maintenance, a private game, a holiday. The
// availability endpoint already treats blocks as unavailable, so a blocked
// window disappears from the public slot grid immediately.
func BlockPitchSlot(c *gin.Context) {
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	pitchID, ok := parseID(c, "id")
	if !ok {
		return
	}
	if !ownerOwnsPitch(c, ownerID, pitchID) {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_PITCH", "this pitch is not on your account")
		return
	}
	var input struct {
		StartsAt time.Time `json:"starts_at" binding:"required"`
		EndsAt   time.Time `json:"ends_at" binding:"required"`
		Reason   string    `json:"reason" binding:"max=120"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || !input.EndsAt.After(input.StartsAt) {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "starts_at and a later ends_at are required")
		return
	}

	// A slot already sold is not the owner's to close from here — cancelling a
	// paid booking is a refund decision, not a calendar edit.
	var clashes int64
	initializers.DB.WithContext(c.Request.Context()).Model(&models.Booking{}).
		Where("pitch_id = ? AND status IN ? AND starts_at < ? AND ends_at > ?",
			pitchID, []string{models.BookingStatusConfirmed, models.BookingStatusPartPaid, models.BookingStatusCompleted},
			input.EndsAt.UTC(), input.StartsAt.UTC()).
		Count(&clashes)
	if clashes > 0 {
		utils.RespondError(c, http.StatusConflict, "SLOT_BOOKED", "someone has already paid for this time — cancel the booking first")
		return
	}

	block := models.SlotBlock{
		PitchID:   pitchID,
		StartsAt:  input.StartsAt.UTC(),
		EndsAt:    input.EndsAt.UTC(),
		Reason:    strings.TrimSpace(input.Reason),
		CreatedBy: ownerID,
	}
	if err := initializers.DB.WithContext(c.Request.Context()).Create(&block).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "BLOCK_FAILED", "could not close that time")
		return
	}
	utils.RespondSuccess(c, http.StatusCreated, gin.H{"id": block.ID}, "Slot closed.")
}

// UnblockPitchSlot — DELETE /v1/owner/pitches/:id/blocks
// Reopens whatever the owner closed across the given window.
func UnblockPitchSlot(c *gin.Context) {
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	pitchID, ok := parseID(c, "id")
	if !ok {
		return
	}
	if !ownerOwnsPitch(c, ownerID, pitchID) {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_PITCH", "this pitch is not on your account")
		return
	}
	var input struct {
		StartsAt time.Time `json:"starts_at" binding:"required"`
		EndsAt   time.Time `json:"ends_at" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "starts_at and ends_at are required")
		return
	}
	initializers.DB.WithContext(c.Request.Context()).
		Where("pitch_id = ? AND starts_at < ? AND ends_at > ?", pitchID, input.EndsAt.UTC(), input.StartsAt.UTC()).
		Delete(&models.SlotBlock{})
	utils.RespondSuccess(c, http.StatusOK, gin.H{"reopened": true}, "Slot reopened.")
}

// CloseVenueDay — POST /v1/owner/venues/:id/close-day
// Shuts a whole date across every pitch at the venue: a public holiday, a
// tournament, resurfacing. Implemented as one block per pitch so the public
// availability grid needs no special case.
func CloseVenueDay(c *gin.Context) {
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
	var input struct {
		Date   string `json:"date" binding:"required"`
		Reason string `json:"reason" binding:"max=120"`
		Reopen bool   `json:"reopen"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "date is required as YYYY-MM-DD")
		return
	}
	day, err := time.Parse("2006-01-02", strings.TrimSpace(input.Date))
	if err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_DATE", "date must be YYYY-MM-DD")
		return
	}
	eat := time.FixedZone("EAT", 3*3600)
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, eat).UTC()
	end := start.Add(24 * time.Hour)

	var pitchIDs []uuid.UUID
	initializers.DB.WithContext(c.Request.Context()).Model(&models.Pitch{}).
		Where("venue_id = ?", venueID).Pluck("id", &pitchIDs)
	if len(pitchIDs) == 0 {
		utils.RespondError(c, http.StatusBadRequest, "NO_PITCHES", "this venue has no pitches yet")
		return
	}

	if input.Reopen {
		initializers.DB.WithContext(c.Request.Context()).
			Where("pitch_id IN ? AND starts_at < ? AND ends_at > ?", pitchIDs, end, start).
			Delete(&models.SlotBlock{})
		utils.RespondSuccess(c, http.StatusOK, gin.H{"closed": false}, "Day reopened.")
		return
	}

	var clashes int64
	initializers.DB.WithContext(c.Request.Context()).Model(&models.Booking{}).
		Where("pitch_id IN ? AND status IN ? AND starts_at < ? AND ends_at > ?",
			pitchIDs, []string{models.BookingStatusConfirmed, models.BookingStatusPartPaid, models.BookingStatusCompleted},
			end, start).
		Count(&clashes)
	if clashes > 0 {
		utils.RespondError(c, http.StatusConflict, "DAY_HAS_BOOKINGS", "there are paid bookings that day — cancel them first")
		return
	}

	reason := strings.TrimSpace(input.Reason)
	for _, pitchID := range pitchIDs {
		initializers.DB.WithContext(c.Request.Context()).Create(&models.SlotBlock{
			PitchID: pitchID, StartsAt: start, EndsAt: end, Reason: reason, CreatedBy: ownerID,
		})
	}
	utils.RespondSuccess(c, http.StatusOK, gin.H{"closed": true, "pitches": len(pitchIDs)}, "Day closed.")
}
