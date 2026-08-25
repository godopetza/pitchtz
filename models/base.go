package models

import (
	"time"

	"github.com/google/uuid"
)

type Base struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const (
	CityStatusLive     = "live"
	CityStatusWaitlist = "waitlist"

	VenueStatusPending   = "pending"
	VenueStatusReview    = "review"
	VenueStatusActive    = "active"
	VenueStatusSuspended = "suspended"

	BookingStatusPending   = "pending"
	BookingStatusConfirmed = "confirmed"
	BookingStatusPartPaid  = "part_paid"
	BookingStatusCompleted = "completed"
	BookingStatusCancelled = "cancelled"
	BookingStatusDisputed  = "disputed"
)
