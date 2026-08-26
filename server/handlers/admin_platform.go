package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/utils"
)

func writeAudit(c *gin.Context, action, targetKind, targetID, detail string) {
	actorID, _ := adminUserID(c)
	email := ""
	var actor models.User
	if initializers.DB.WithContext(c.Request.Context()).First(&actor, "id = ?", actorID).Error == nil && actor.Email != nil {
		email = *actor.Email
	}
	initializers.DB.WithContext(c.Request.Context()).Create(&models.AuditLog{
		ActorUserID: actorID, ActorEmail: email, Action: action,
		TargetKind: targetKind, TargetID: targetID, Detail: detail,
	})
}

// AdminPlatformStats powers the superadmin overview with real numbers.
func AdminPlatformStats(c *gin.Context) {
	db := initializers.DB.WithContext(c.Request.Context())
	var bookingsTotal, bookingsToday, venuesActive, venuesPending, disputesOpen, players int64
	var revenuePaid int64
	todayStart := time.Now().UTC().Truncate(24 * time.Hour)

	db.Model(&models.Booking{}).Count(&bookingsTotal)
	db.Model(&models.Booking{}).Where("created_at >= ?", todayStart).Count(&bookingsToday)
	db.Model(&models.Venue{}).Where("status = ?", models.VenueStatusActive).Count(&venuesActive)
	db.Model(&models.Venue{}).Where("status = ?", models.VenueStatusPending).Count(&venuesPending)
	db.Model(&models.Dispute{}).Where("status = ?", "open").Count(&disputesOpen)
	db.Model(&models.User{}).Where("role = ?", "player").Count(&players)
	db.Model(&models.PaymentShare{}).Where("status = ?", "paid").
		Select("COALESCE(SUM(amount_tzs), 0)").Scan(&revenuePaid)

	utils.RespondSuccess(c, http.StatusOK, gin.H{
		"bookings_total":   bookingsTotal,
		"bookings_today":   bookingsToday,
		"revenue_paid_tzs": revenuePaid,
		"venues_active":    venuesActive,
		"venues_pending":   venuesPending,
		"disputes_open":    disputesOpen,
		"players":          players,
	}, "")
}

// AdminListBookings is the payments/bookings feed for the superadmin portal.
func AdminListBookings(c *gin.Context) {
	type row struct {
		models.Booking
		VenueName    string
		PitchName    string
		CustomerName string
		PaidTZS      int64
	}
	var rows []row
	initializers.DB.WithContext(c.Request.Context()).
		Table("bookings").
		Select(`bookings.*, venues.name AS venue_name, pitches.name AS pitch_name,
			COALESCE(users.name, '') AS customer_name,
			COALESCE((SELECT SUM(amount_tzs) FROM payment_shares WHERE payment_shares.booking_id = bookings.id AND payment_shares.status = 'paid'), 0) AS paid_tzs`).
		Joins("JOIN pitches ON pitches.id = bookings.pitch_id").
		Joins("JOIN venues ON venues.id = pitches.venue_id").
		Joins("LEFT JOIN users ON users.id = bookings.user_id").
		Order("bookings.created_at DESC").Limit(50).Scan(&rows)

	items := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		items = append(items, gin.H{
			"id": r.ID, "code": r.Code, "venue": r.VenueName, "pitch": r.PitchName,
			"customer": r.CustomerName, "starts_at": r.StartsAt, "status": r.Status,
			"total_tzs": r.TotalTZS, "paid_tzs": r.PaidTZS, "created_at": r.CreatedAt,
		})
	}
	utils.RespondSuccess(c, http.StatusOK, items, "")
}

// AdminListDisputes lists real disputes for the operations view.
func AdminListDisputes(c *gin.Context) {
	var disputes []models.Dispute
	initializers.DB.WithContext(c.Request.Context()).Order("created_at DESC").Limit(100).Find(&disputes)
	utils.RespondSuccess(c, http.StatusOK, disputes, "")
}

type venueStatusInput struct {
	Status string `json:"status" binding:"required"`
}

// AdminSetVenueStatus activates/suspends/reopens a venue, with an audit row
// recording which admin did it.
func AdminSetVenueStatus(c *gin.Context) {
	venueID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input venueStatusInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "status is required")
		return
	}
	allowed := map[string]bool{
		models.VenueStatusActive: true, models.VenueStatusSuspended: true,
		models.VenueStatusPending: true, models.VenueStatusReview: true,
	}
	status := strings.TrimSpace(input.Status)
	if !allowed[status] {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_STATUS", "status must be one of active, suspended, pending, review")
		return
	}
	var venue models.Venue
	if err := initializers.DB.WithContext(c.Request.Context()).First(&venue, "id = ?", venueID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "VENUE_NOT_FOUND", "venue was not found")
		return
	}
	initializers.DB.WithContext(c.Request.Context()).Model(&models.Venue{}).
		Where("id = ?", venueID).Update("status", status)
	writeAudit(c, "venue.status", "venue", venueID.String(),
		fmt.Sprintf("%s: %s -> %s", venue.Name, venue.Status, status))
	utils.RespondSuccess(c, http.StatusOK, gin.H{"id": venueID, "status": status}, "Venue status updated.")
}

// AdminDeleteVenue soft-deletes: the venue drops out of every list but the
// row (and the audit trail naming the actor) survives.
func AdminDeleteVenue(c *gin.Context) {
	venueID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var venue models.Venue
	if err := initializers.DB.WithContext(c.Request.Context()).First(&venue, "id = ?", venueID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "VENUE_NOT_FOUND", "venue was not found")
		return
	}
	initializers.DB.WithContext(c.Request.Context()).Model(&models.Venue{}).
		Where("id = ?", venueID).Update("status", "deleted")
	writeAudit(c, "venue.delete", "venue", venueID.String(), venue.Name)
	utils.RespondSuccess(c, http.StatusOK, gin.H{"deleted": true}, "Venue deleted.")
}

// AdminListAudit exposes the trail (newest first).
func AdminListAudit(c *gin.Context) {
	var entries []models.AuditLog
	initializers.DB.WithContext(c.Request.Context()).Order("created_at DESC").Limit(100).Find(&entries)
	utils.RespondSuccess(c, http.StatusOK, entries, "")
}
