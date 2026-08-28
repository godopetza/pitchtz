package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/services"
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

	weekStart := time.Now().UTC().AddDate(0, 0, -7)
	prevWeekStart := time.Now().UTC().AddDate(0, 0, -14)
	var bookingsWeek, bookingsPrevWeek, bookingsFundedWeek, bookingsFailedWeek int64
	db.Model(&models.Booking{}).Where("created_at >= ?", weekStart).Count(&bookingsWeek)
	db.Model(&models.Booking{}).Where("created_at >= ? AND created_at < ?", prevWeekStart, weekStart).Count(&bookingsPrevWeek)
	db.Model(&models.Booking{}).Where("created_at >= ? AND status IN ?", weekStart,
		[]string{models.BookingStatusConfirmed, models.BookingStatusCompleted, models.BookingStatusPartPaid}).Count(&bookingsFundedWeek)
	db.Model(&models.Booking{}).Where("created_at >= ? AND status = ?", weekStart, models.BookingStatusCancelled).Count(&bookingsFailedWeek)

	var gmvWeek, gmvPrevWeek int64
	db.Model(&models.PaymentShare{}).Where("status = ? AND paid_at >= ?", "paid", weekStart).
		Select("COALESCE(SUM(amount_tzs), 0)").Scan(&gmvWeek)
	db.Model(&models.PaymentShare{}).Where("status = ? AND paid_at >= ? AND paid_at < ?", "paid", prevWeekStart, weekStart).
		Select("COALESCE(SUM(amount_tzs), 0)").Scan(&gmvPrevWeek)

	// GMV, weekly buckets for the last 8 weeks.
	type weekRow struct {
		Week   time.Time
		Amount int64
	}
	var weeks []weekRow
	db.Model(&models.PaymentShare{}).
		Select("date_trunc('week', paid_at) AS week, COALESCE(SUM(amount_tzs), 0) AS amount").
		Where("status = ? AND paid_at >= ?", "paid", time.Now().UTC().AddDate(0, 0, -56)).
		Group("week").Order("week").Scan(&weeks)
	weekly := make([]gin.H, 0, len(weeks))
	for _, w := range weeks {
		weekly = append(weekly, gin.H{"week": w.Week.Format("2006-01-02"), "amount": w.Amount})
	}

	// Payment mix from completed charges, by operator.
	type mixRow struct {
		Operator string
		Count    int64
	}
	var mix []mixRow
	db.Model(&models.PaymentTransaction{}).
		Select("COALESCE(NULLIF(operator, ''), 'other') AS operator, COUNT(*) AS count").
		Where("status = ? AND direction = ?", "completed", "charge").
		Group("operator").Order("count DESC").Scan(&mix)
	mixItems := make([]gin.H, 0, len(mix))
	for _, m := range mix {
		mixItems = append(mixItems, gin.H{"operator": m.Operator, "count": m.Count})
	}

	// Top venues by bookings this week, with weekly GMV and fee income.
	type topRow struct {
		ID            string
		Name          string
		Area          string
		Latitude      float64
		Longitude     float64
		Rating        float64
		FeeRateBPS    int
		BookingsWeek  int64
		FundedWeek    int64
		CancelledWeek int64
		GMVWeek       int64
	}
	var top []topRow
	db.Table("venues").
		Select(`venues.id, venues.name, venues.area, venues.latitude, venues.longitude, venues.rating, venues.fee_rate_bps,
			COALESCE((SELECT COUNT(*) FROM bookings b JOIN pitches p ON p.id = b.pitch_id WHERE p.venue_id = venues.id AND b.created_at >= ?), 0) AS bookings_week,
			COALESCE((SELECT COUNT(*) FROM bookings bf JOIN pitches pf ON pf.id = bf.pitch_id WHERE pf.venue_id = venues.id AND bf.created_at >= ? AND bf.status IN ('confirmed','completed','part_paid')), 0) AS funded_week,
			COALESCE((SELECT COUNT(*) FROM bookings bc JOIN pitches pc ON pc.id = bc.pitch_id WHERE pc.venue_id = venues.id AND bc.created_at >= ? AND bc.status = 'cancelled'), 0) AS cancelled_week,
			COALESCE((SELECT SUM(ps.amount_tzs) FROM payment_shares ps JOIN bookings b2 ON b2.id = ps.booking_id JOIN pitches p2 ON p2.id = b2.pitch_id WHERE p2.venue_id = venues.id AND ps.status = 'paid' AND ps.paid_at >= ?), 0) AS gmv_week`,
			weekStart, weekStart, weekStart, weekStart).
		Where("venues.status = ?", models.VenueStatusActive).
		Order("bookings_week DESC, venues.rating DESC").Limit(8).Scan(&top)
	topItems := make([]gin.H, 0, len(top))
	for _, v := range top {
		topItems = append(topItems, gin.H{
			"id": v.ID, "name": v.Name, "area": v.Area, "lat": v.Latitude, "lng": v.Longitude,
			"rating": v.Rating, "bookings_week": v.BookingsWeek, "funded_week": v.FundedWeek, "cancelled_week": v.CancelledWeek, "gmv_week": v.GMVWeek,
			"fee_week": v.GMVWeek * int64(v.FeeRateBPS) / 10000,
		})
	}

	utils.RespondSuccess(c, http.StatusOK, gin.H{
		"bookings_total":       bookingsTotal,
		"bookings_today":       bookingsToday,
		"bookings_week":        bookingsWeek,
		"bookings_funded_week": bookingsFundedWeek,
		"bookings_failed_week": bookingsFailedWeek,
		"bookings_prev_week":   bookingsPrevWeek,
		"revenue_paid_tzs":     revenuePaid,
		"gmv_week_tzs":         gmvWeek,
		"gmv_prev_week_tzs":    gmvPrevWeek,
		"venues_active":        venuesActive,
		"venues_pending":       venuesPending,
		"disputes_open":        disputesOpen,
		"players":              players,
		"weekly_gmv":           weekly,
		"payment_mix":          mixItems,
		"top_venues":           topItems,
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
	if status == models.VenueStatusActive && venue.Status != models.VenueStatusActive {
		var owner models.User
		if err := initializers.DB.First(&owner, "id = ?", venue.OwnerID).Error; err == nil && owner.Email != nil {
			go services.SendVenueApprovedEmails(context.Background(), *owner.Email, owner.Name, venue.Name, venue.ID)
		}
	}
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

// AdminNotifications is the lightweight poll behind the sidebar badges and
// the bell: everything that currently needs a human.
func AdminNotifications(c *gin.Context) {
	db := initializers.DB.WithContext(c.Request.Context())
	var venuesPending, disputesOpen, careersNew int64
	db.Model(&models.Venue{}).Where("status = ?", models.VenueStatusPending).Count(&venuesPending)
	db.Model(&models.Dispute{}).Where("status = ?", "open").Count(&disputesOpen)
	db.Model(&models.CareerApplication{}).Where("status = ?", "new").Count(&careersNew)
	utils.RespondSuccess(c, http.StatusOK, gin.H{
		"venues_pending": venuesPending,
		"disputes_open":  disputesOpen,
		"careers_new":    careersNew,
	}, "")
}

// AdminSetPitchStatus suspends or restores a single pitch, with an audit row
// naming the staff member. Owners see "suspended by staff" and cannot lift it.
func AdminSetPitchStatus(c *gin.Context) {
	pitchID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input struct {
		Status string `json:"status" binding:"required,oneof=active suspended"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "status must be active or suspended")
		return
	}
	var pitch models.Pitch
	if err := initializers.DB.WithContext(c.Request.Context()).First(&pitch, "id = ?", pitchID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "PITCH_NOT_FOUND", "pitch was not found")
		return
	}
	initializers.DB.WithContext(c.Request.Context()).Model(&models.Pitch{}).
		Where("id = ?", pitchID).Update("status", input.Status)
	writeAudit(c, "pitch.status", "pitch", pitchID.String(),
		fmt.Sprintf("%s: %s -> %s", pitch.Name, pitch.Status, input.Status))
	utils.RespondSuccess(c, http.StatusOK, gin.H{"id": pitchID, "status": input.Status}, "Pitch status updated.")
}

// AdminListPlatformUsers backs the "Registered users" drill-down: every
// player and owner account with its activity, newest first, searchable.
func AdminListPlatformUsers(c *gin.Context) {
	type row struct {
		models.User
		BookingsCount int64
		TeamsCount    int64
	}
	query := initializers.DB.WithContext(c.Request.Context()).
		Table("users").
		Select(`users.*,
			COALESCE((SELECT COUNT(*) FROM bookings WHERE bookings.user_id = users.id), 0) AS bookings_count,
			COALESCE((SELECT COUNT(*) FROM team_members WHERE team_members.user_id = users.id AND team_members.status = 'active'), 0) AS teams_count`)
	if search := strings.TrimSpace(c.Query("q")); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(users.name) LIKE ? OR LOWER(users.email) LIKE ?", like, like)
	}
	if role := strings.TrimSpace(c.Query("role")); role != "" {
		query = query.Where("users.role = ?", role)
	}
	var rows []row
	query.Order("users.created_at DESC").Limit(200).Scan(&rows)

	items := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		email := ""
		if r.Email != nil {
			email = *r.Email
		}
		items = append(items, gin.H{
			"id": r.ID, "name": r.Name, "email": email, "role": r.Role,
			"provider": r.AuthProvider, "avatar_url": r.AvatarURL,
			"created_at": r.CreatedAt, "bookings": r.BookingsCount, "teams": r.TeamsCount,
		})
	}
	utils.RespondSuccess(c, http.StatusOK, items, "")
}
