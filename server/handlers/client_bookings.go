package handlers

import (
	"errors"
	"fmt"
	"math"
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
	"github.com/jackc/pgx/v5/pgconn"
)

type clientCreateBookingInput struct {
	PitchID  uuid.UUID `json:"pitch_id" binding:"required"`
	StartsAt time.Time `json:"starts_at" binding:"required"`
	EndsAt   time.Time `json:"ends_at" binding:"required"`
}

type clientPayInput struct {
	Provider string `json:"provider" binding:"required"`
	Phone    string `json:"phone" binding:"required,max=32"`
	Operator string `json:"operator" binding:"required,max=40"`
}

type splitInput struct {
	Parts int `json:"parts" binding:"required,min=2,max=11"`
}

func shareDTO(share models.PaymentShare) gin.H {
	return gin.H{
		"id":         share.ID,
		"amount_tzs": share.AmountTZS,
		"kind":       share.Kind,
		"status":     share.Status,
		"paid_at":    share.PaidAt,
		"pay_url":    sharePayURL(share.ID),
	}
}

func sharePayURL(id uuid.UUID) string {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("CLIENT_APP_URL")), "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	return base + "/pay/" + id.String()
}

func bookingWithShares(c *gin.Context, booking models.Booking) gin.H {
	var shares []models.PaymentShare
	initializers.DB.WithContext(c.Request.Context()).
		Where("booking_id = ?", booking.ID).Order("created_at ASC").Find(&shares)
	var paid int64
	shareDTOs := make([]gin.H, 0, len(shares))
	for _, share := range shares {
		if share.Status == "paid" {
			paid += share.AmountTZS
		}
		shareDTOs = append(shareDTOs, shareDTO(share))
	}
	payload := gin.H{
		"id":              booking.ID,
		"code":            booking.Code,
		"pitch_id":        booking.PitchID,
		"starts_at":       booking.StartsAt,
		"ends_at":         booking.EndsAt,
		"status":          booking.Status,
		"pitch_fee_tzs":   booking.PitchFeeTZS,
		"service_fee_tzs": booking.ServiceFeeTZS,
		"total_tzs":       booking.TotalTZS,
		"paid_tzs":        paid,
		"shares":          shareDTOs,
	}
	if booking.Status == models.BookingStatusPending {
		payload["hold_expires_at"] = booking.CreatedAt.Add(services.BookingHoldWindow()).UTC()
	}
	return payload
}

// ClientCreateBooking is the self-service booking path: a signed-in player
// books a pitch directly. The DB exclusion constraint stays the real
// double-booking guard; this handler just surfaces it as a 409.
func ClientCreateBooking(c *gin.Context) {
	if initializers.DB == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "DATABASE_REQUIRED", "booking is unavailable")
		return
	}
	userID, ok := clientUserID(c)
	if !ok {
		return
	}
	var input clientCreateBookingInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "pitch_id, starts_at, and ends_at are required")
		return
	}
	if !input.EndsAt.After(input.StartsAt) {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_RANGE", "ends_at must be after starts_at")
		return
	}
	if input.StartsAt.Before(time.Now().Add(-5 * time.Minute)) {
		utils.RespondError(c, http.StatusBadRequest, "STARTS_IN_PAST", "the booking start time has already passed")
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
	if err := initializers.DB.WithContext(c.Request.Context()).First(&venue, "id = ?", pitch.VenueID).Error; err != nil || venue.Status != "active" {
		utils.RespondError(c, http.StatusNotFound, "VENUE_NOT_AVAILABLE", "this venue is not taking bookings")
		return
	}
	if !venueOpenAt(venue, input.StartsAt, input.EndsAt) {
		utils.RespondError(c, http.StatusConflict, "OUTSIDE_OPEN_HOURS", "the venue is closed at that time")
		return
	}

	hours := input.EndsAt.Sub(input.StartsAt).Hours()
	pitchFee := int64(math.Round(float64(pitch.BasePriceTZS) * hours))
	const serviceFee = 3000

	booking := models.Booking{
		Code:          bookingCode(),
		PitchID:       input.PitchID,
		UserID:        &userID,
		StartsAt:      input.StartsAt.UTC(),
		EndsAt:        input.EndsAt.UTC(),
		Source:        "app",
		Status:        models.BookingStatusPending,
		PitchFeeTZS:   pitchFee,
		ServiceFeeTZS: serviceFee,
		TotalTZS:      pitchFee + serviceFee,
	}
	err := initializers.DB.WithContext(c.Request.Context()).Create(&booking).Error
	if err != nil && isBookingConflict(err) && services.ReleaseExpiredHolds() > 0 {
		// The clash may have been a lapsed unpaid hold — it's gone now, retry once.
		booking.ID = uuid.Nil
		err = initializers.DB.WithContext(c.Request.Context()).Create(&booking).Error
	}
	if err != nil {
		if isBookingConflict(err) {
			utils.RespondError(c, http.StatusConflict, "BOOKING_CONFLICT", "this pitch is already booked for part of that time window")
			return
		}
		utils.RespondError(c, http.StatusInternalServerError, "BOOKING_CREATE_FAILED", "could not create the booking")
		return
	}
	utils.RespondSuccess(c, http.StatusCreated, bookingWithShares(c, booking), "Booking held. Pay to confirm it.")
}

func isBookingConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "23P01" || pgErr.Code == "23505")
}

func clientOwnedBooking(c *gin.Context) (models.Booking, bool) {
	userID, ok := clientUserID(c)
	if !ok {
		return models.Booking{}, false
	}
	bookingID, ok := parseID(c, "id")
	if !ok {
		return models.Booking{}, false
	}
	var booking models.Booking
	if err := initializers.DB.WithContext(c.Request.Context()).First(&booking, "id = ?", bookingID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "BOOKING_NOT_FOUND", "booking was not found")
		return models.Booking{}, false
	}
	if booking.UserID == nil || *booking.UserID != userID {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_BOOKING", "this booking belongs to another account")
		return models.Booking{}, false
	}
	return booking, true
}

func ClientGetBooking(c *gin.Context) {
	booking, ok := clientOwnedBooking(c)
	if !ok {
		return
	}
	utils.RespondSuccess(c, http.StatusOK, bookingWithShares(c, booking), "")
}

func ClientListBookings(c *gin.Context) {
	userID, ok := clientUserID(c)
	if !ok {
		return
	}
	var bookings []models.Booking
	initializers.DB.WithContext(c.Request.Context()).
		Where("user_id = ?", userID).Order("starts_at DESC").Limit(20).Find(&bookings)
	items := make([]gin.H, 0, len(bookings))
	for _, booking := range bookings {
		items = append(items, bookingWithShares(c, booking))
	}
	utils.RespondSuccess(c, http.StatusOK, items, "")
}

// ClientPayBooking collects the full outstanding balance from the booker's
// own phone in one mobile-money prompt.
func ClientPayBooking(c *gin.Context) {
	booking, ok := clientOwnedBooking(c)
	if !ok {
		return
	}
	if !requireMalipo(c) {
		return
	}
	var input clientPayInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "provider, phone, and operator are required")
		return
	}
	if booking.Status == models.BookingStatusCancelled {
		utils.RespondError(c, http.StatusConflict, "BOOKING_EXPIRED", "this hold expired and the slot was released — book again")
		return
	}
	outstanding := outstandingTZS(c, booking)
	if outstanding <= 0 {
		utils.RespondError(c, http.StatusConflict, "ALREADY_PAID", "this booking is already fully paid")
		return
	}
	userID, _ := clientUserID(c)
	share := models.PaymentShare{
		BookingID:   booking.ID,
		PayerUserID: &userID,
		PayerPhone:  strings.TrimSpace(input.Phone),
		AmountTZS:   outstanding,
		Kind:        "gateway",
		Status:      "unpaid",
	}
	if err := initializers.DB.WithContext(c.Request.Context()).Create(&share).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "PAYMENT_INIT_FAILED", "could not start the payment")
		return
	}
	chargeShareViaMalipo(c, booking, share, input.Provider, input.Phone, input.Operator)
}

// ClientSplitBooking divides the outstanding balance into equal shares, each
// with its own public pay link (rendered as a QR code by the frontend) so
// teammates can pay their part from their own phones.
func ClientSplitBooking(c *gin.Context) {
	booking, ok := clientOwnedBooking(c)
	if !ok {
		return
	}
	var input splitInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "parts must be between 2 and 11")
		return
	}
	var existingUnpaid int64
	initializers.DB.WithContext(c.Request.Context()).Model(&models.PaymentShare{}).
		Where("booking_id = ? AND kind = ? AND status = ?", booking.ID, "split", "unpaid").Count(&existingUnpaid)
	if existingUnpaid > 0 {
		utils.RespondError(c, http.StatusConflict, "ALREADY_SPLIT", "this booking already has unpaid split shares")
		return
	}
	outstanding := outstandingTZS(c, booking)
	if outstanding <= 0 {
		utils.RespondError(c, http.StatusConflict, "ALREADY_PAID", "this booking is already fully paid")
		return
	}

	parts := int64(input.Parts)
	base := outstanding / parts
	remainder := outstanding - base*parts
	shares := make([]gin.H, 0, input.Parts)
	for index := int64(0); index < parts; index++ {
		amount := base
		if index == 0 {
			amount += remainder
		}
		share := models.PaymentShare{
			BookingID: booking.ID,
			AmountTZS: amount,
			Kind:      "split",
			Status:    "unpaid",
		}
		if err := initializers.DB.WithContext(c.Request.Context()).Create(&share).Error; err != nil {
			utils.RespondError(c, http.StatusInternalServerError, "SPLIT_FAILED", "could not create the split shares")
			return
		}
		shares = append(shares, shareDTO(share))
	}
	utils.RespondSuccess(c, http.StatusCreated, gin.H{
		"booking_id": booking.ID,
		"shares":     shares,
	}, fmt.Sprintf("Split into %d shares. Send each teammate their link or QR.", input.Parts))
}

func outstandingTZS(c *gin.Context, booking models.Booking) int64 {
	var paid int64
	initializers.DB.WithContext(c.Request.Context()).Model(&models.PaymentShare{}).
		Where("booking_id = ? AND status = ?", booking.ID, "paid").
		Select("COALESCE(SUM(amount_tzs), 0)").Scan(&paid)
	return booking.TotalTZS - paid
}

// GetPublicShare powers the /pay/{shareId} page a teammate lands on from a
// QR code. Public by design: the unguessable share id is the capability, and
// the endpoint only reveals what a payer needs to see.
func GetPublicShare(c *gin.Context) {
	if initializers.DB == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "DATABASE_REQUIRED", "payments are unavailable")
		return
	}
	shareID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var share models.PaymentShare
	if err := initializers.DB.WithContext(c.Request.Context()).First(&share, "id = ?", shareID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "SHARE_NOT_FOUND", "this payment link is not valid")
		return
	}
	var booking models.Booking
	if err := initializers.DB.WithContext(c.Request.Context()).First(&booking, "id = ?", share.BookingID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "SHARE_NOT_FOUND", "this payment link is not valid")
		return
	}
	var pitch models.Pitch
	var venue models.Venue
	initializers.DB.WithContext(c.Request.Context()).First(&pitch, "id = ?", booking.PitchID)
	initializers.DB.WithContext(c.Request.Context()).First(&venue, "id = ?", pitch.VenueID)

	utils.RespondSuccess(c, http.StatusOK, gin.H{
		"share_id":     share.ID,
		"amount_tzs":   share.AmountTZS,
		"status":       share.Status,
		"kind":         share.Kind,
		"booking_code": booking.Code,
		"starts_at":    booking.StartsAt,
		"venue_name":   venue.Name,
		"pitch_name":   pitch.Name,
	}, "")
}

// PayPublicShare triggers the mobile-money prompt for one share from the
// public pay page — no account needed to chip in for your team.
func PayPublicShare(c *gin.Context) {
	if initializers.DB == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "DATABASE_REQUIRED", "payments are unavailable")
		return
	}
	if !requireMalipo(c) {
		return
	}
	shareID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input clientPayInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "provider, phone, and operator are required")
		return
	}
	var share models.PaymentShare
	if err := initializers.DB.WithContext(c.Request.Context()).First(&share, "id = ?", shareID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "SHARE_NOT_FOUND", "this payment link is not valid")
		return
	}
	if share.Status == "paid" {
		utils.RespondError(c, http.StatusConflict, "ALREADY_PAID", "this share is already paid — asante!")
		return
	}
	var booking models.Booking
	if err := initializers.DB.WithContext(c.Request.Context()).First(&booking, "id = ?", share.BookingID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "SHARE_NOT_FOUND", "this payment link is not valid")
		return
	}
	if booking.Status == models.BookingStatusCancelled {
		utils.RespondError(c, http.StatusConflict, "BOOKING_EXPIRED", "this hold expired and the slot was released — ask the booker to rebook")
		return
	}
	chargeShareViaMalipo(c, booking, share, input.Provider, input.Phone, input.Operator)
}

func requireMalipo(c *gin.Context) bool {
	if !services.MalipoConfigured() {
		utils.RespondError(c, http.StatusServiceUnavailable, "PAYMENTS_NOT_CONFIGURED", "payment collection is not configured yet")
		return false
	}
	return true
}
