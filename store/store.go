package store

import (
	"context"
	"errors"
	"time"

	"github.com/godopetza/pitchtz/models"
	"github.com/google/uuid"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrInvalidCity = errors.New("invalid city")
)

type VenueFilter struct {
	CityID    *uuid.UUID
	Format    string
	Latitude  *float64
	Longitude *float64
	RadiusKM  float64
}

type UnavailableWindow struct {
	PitchID  uuid.UUID
	StartsAt time.Time
	EndsAt   time.Time
	Kind     string
}

type WaitlistInput struct {
	CityID uuid.UUID
	Email  *string
	Phone  *string
}

type CatalogStore interface {
	ListCities(ctx context.Context) ([]models.City, error)
	ListVenues(ctx context.Context, filter VenueFilter) ([]models.Venue, error)
	GetVenue(ctx context.Context, id uuid.UUID) (models.Venue, error)
	ListVenueReviews(ctx context.Context, venueID uuid.UUID) ([]models.Review, error)
	ListVenueExtras(ctx context.Context, venueID uuid.UUID) ([]models.ExtraCatalog, error)
	ListUnavailable(ctx context.Context, venueID uuid.UUID, from, to time.Time) ([]UnavailableWindow, error)
}

type WaitlistStore interface {
	JoinWaitlist(ctx context.Context, input WaitlistInput) (models.CityWaitlist, models.City, error)
}
