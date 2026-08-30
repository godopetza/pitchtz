package handlers

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/utils"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type createBookingInput struct {
	PitchID       uuid.UUID  `json:"pitch_id" binding:"required"`
	StartsAt      time.Time  `json:"starts_at" binding:"required"`
	EndsAt        time.Time  `json:"ends_at" binding:"required"`
	CustomerName  string     `json:"customer_name" binding:"required,min=2,max=120"`
	CustomerPhone string     `json:"customer_phone" binding:"required,max=32"`
	TeamID        *uuid.UUID `json:"team_id"`
	Source        string     `json:"source"`
}

// CreateBooking lets an authenticated venue owner record a booking (walk-in,
// phone, manual entry) for one of their own pitches. Overlap safety does not
// rely on this handler's own checks: the database's bookings_no_overlap
// exclusion constraint (initializers/migrate.go) is what actually prevents
// double-booking, even under concurrent requests. This handler just turns
// that DB rejection into a clean 409 for the caller.
func CreateBooking(c *gin.Context) {
	if initializers.DB == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "DATABASE_REQUIRED", "booking creation is unavailable")
		return
	}
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	var input createBookingInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "pitch_id, starts_at, ends_at, customer_name, and customer_phone are required")
		return
	}
	if !input.EndsAt.After(input.StartsAt) {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_RANGE", "ends_at must be after starts_at")
		return
	}

	var pitch models.Pitch
	if err := initializers.DB.WithContext(c.Request.Context()).First(&pitch, "id = ?", input.PitchID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "PITCH_NOT_FOUND", "pitch was not found")
		return
	}
	if pitch.Status != "" && pitch.Status != "active" {
		utils.RespondError(c, http.StatusConflict, "PITCH_UNAVAILABLE", "this pitch is not accepting bookings right now")
		return
	}
	var venue models.Venue
	if err := initializers.DB.WithContext(c.Request.Context()).First(&venue, "id = ?", pitch.VenueID).Error; err != nil || venue.OwnerID != ownerID {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_VENUE", "this pitch does not belong to your venue")
		return
	}

	customerID, err := findOrCreateCustomer(c, input.CustomerName, input.CustomerPhone)
	if err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "CUSTOMER_LOOKUP_FAILED", "could not record the customer")
		return
	}

	hours := input.EndsAt.Sub(input.StartsAt).Hours()
	pitchFee := int64(math.Round(float64(pitch.BasePriceTZS) * hours))
	const serviceFee = 3000
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = "owner_manual"
	}

	booking := models.Booking{
		Code:          bookingCode(),
		PitchID:       input.PitchID,
		UserID:        &customerID,
		TeamID:        input.TeamID,
		StartsAt:      input.StartsAt.UTC(),
		EndsAt:        input.EndsAt.UTC(),
		Source:        source,
		ContactName:   strings.TrimSpace(input.CustomerName),
		ContactPhone:  strings.TrimSpace(input.CustomerPhone),
		Status:        models.BookingStatusPending,
		PitchFeeTZS:   pitchFee,
		ServiceFeeTZS: serviceFee,
		TotalTZS:      pitchFee + serviceFee,
	}

	if err := initializers.DB.WithContext(c.Request.Context()).Create(&booking).Error; err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.Code == "23P01" || pgErr.Code == "23505") {
			utils.RespondError(c, http.StatusConflict, "BOOKING_CONFLICT", "this pitch is already booked for part of that time window")
			return
		}
		utils.RespondError(c, http.StatusInternalServerError, "BOOKING_CREATE_FAILED", "could not create the booking")
		return
	}

	utils.RespondSuccess(c, http.StatusCreated, booking, "Booking created.")
}

func bookingCode() string {
	return fmt.Sprintf("PTZ-%s", strings.ToUpper(uuid.NewString()[:6]))
}

// findOrCreateCustomer resolves a walk-in/manual booking's customer to a
// User row by phone, creating an unverified one on first sight.
func findOrCreateCustomer(c *gin.Context, name, phone string) (uuid.UUID, error) {
	phone = strings.TrimSpace(phone)
	var user models.User
	err := initializers.DB.WithContext(c.Request.Context()).Where("phone = ?", phone).First(&user).Error
	if err == nil {
		return user.ID, nil
	}
	user = models.User{Phone: &phone, Name: strings.TrimSpace(name), AuthProvider: "manual", Language: "sw", Role: "player"}
	if err := initializers.DB.WithContext(c.Request.Context()).Create(&user).Error; err != nil {
		// Lost a race with another concurrent booking for the same new phone number.
		if refetchErr := initializers.DB.WithContext(c.Request.Context()).Where("phone = ?", phone).First(&user).Error; refetchErr == nil {
			return user.ID, nil
		}
		return uuid.Nil, err
	}
	return user.ID, nil
}
