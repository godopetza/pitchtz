package store

import (
	"context"
	"errors"
	"time"

	"github.com/godopetza/pitchtz/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

func (s *GormStore) ListCities(ctx context.Context) ([]models.City, error) {
	var cities []models.City
	err := s.db.WithContext(ctx).Order("name ASC").Find(&cities).Error
	return cities, err
}

func (s *GormStore) ListVenues(ctx context.Context, filter VenueFilter) ([]models.Venue, error) {
	query := s.db.WithContext(ctx).
		Model(&models.Venue{}).
		Where("venues.status = ?", models.VenueStatusActive)

	if filter.CityID != nil {
		query = query.Where("venues.city_id = ?", *filter.CityID)
	}
	if filter.Format != "" {
		query = query.Where("EXISTS (SELECT 1 FROM pitches WHERE pitches.venue_id = venues.id AND pitches.format = ?)", filter.Format)
	}
	if filter.Latitude != nil && filter.Longitude != nil {
		const distanceSQL = "6371 * acos(LEAST(1.0, GREATEST(-1.0, cos(radians(?)) * cos(radians(venues.latitude)) * cos(radians(venues.longitude) - radians(?)) + sin(radians(?)) * sin(radians(venues.latitude)))))"
		query = query.
			Where(distanceSQL+" <= ?", *filter.Latitude, *filter.Longitude, *filter.Latitude, filter.RadiusKM).
			Order(clause.Expr{SQL: distanceSQL, Vars: []interface{}{*filter.Latitude, *filter.Longitude, *filter.Latitude}})
	} else {
		query = query.Order("venues.name ASC")
	}

	var venues []models.Venue
	err := query.
		Preload("City").
		Preload("Pitches").
		Preload("Photos", func(db *gorm.DB) *gorm.DB { return db.Order("sort ASC") }).
		Preload("Extras", "available = ?", true).
		Limit(100).
		Find(&venues).Error
	return venues, err
}

func (s *GormStore) GetVenue(ctx context.Context, id uuid.UUID) (models.Venue, error) {
	var venue models.Venue
	err := s.db.WithContext(ctx).
		Where("id = ? AND status = ?", id, models.VenueStatusActive).
		Preload("City").
		Preload("Pitches").
		Preload("Photos", func(db *gorm.DB) *gorm.DB { return db.Order("sort ASC") }).
		Preload("Extras", "available = ?", true).
		First(&venue).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Venue{}, ErrNotFound
	}
	return venue, err
}

func (s *GormStore) ListVenueReviews(ctx context.Context, venueID uuid.UUID) ([]models.Review, error) {
	if err := s.requireActiveVenue(ctx, venueID); err != nil {
		return nil, err
	}
	var reviews []models.Review
	err := s.db.WithContext(ctx).Where("venue_id = ?", venueID).Order("created_at DESC").Find(&reviews).Error
	return reviews, err
}

func (s *GormStore) ListVenueExtras(ctx context.Context, venueID uuid.UUID) ([]models.ExtraCatalog, error) {
	if err := s.requireActiveVenue(ctx, venueID); err != nil {
		return nil, err
	}
	var extras []models.ExtraCatalog
	err := s.db.WithContext(ctx).
		Where("venue_id = ? AND available = ?", venueID, true).
		Order("name ASC").
		Find(&extras).Error
	return extras, err
}

func (s *GormStore) ListUnavailable(ctx context.Context, venueID uuid.UUID, from, to time.Time) ([]UnavailableWindow, error) {
	if err := s.requireActiveVenue(ctx, venueID); err != nil {
		return nil, err
	}

	var bookings []models.Booking
	err := s.db.WithContext(ctx).
		Select("bookings.*").
		Joins("JOIN pitches ON pitches.id = bookings.pitch_id").
		Where("pitches.venue_id = ?", venueID).
		Where("bookings.status IN ?", []string{models.BookingStatusPending, models.BookingStatusConfirmed, models.BookingStatusPartPaid}).
		Where("bookings.starts_at < ? AND bookings.ends_at > ?", to, from).
		Find(&bookings).Error
	if err != nil {
		return nil, err
	}

	var blocks []models.SlotBlock
	err = s.db.WithContext(ctx).
		Select("slot_blocks.*").
		Joins("JOIN pitches ON pitches.id = slot_blocks.pitch_id").
		Where("pitches.venue_id = ?", venueID).
		Where("slot_blocks.starts_at < ? AND slot_blocks.ends_at > ?", to, from).
		Find(&blocks).Error
	if err != nil {
		return nil, err
	}

	result := make([]UnavailableWindow, 0, len(bookings)+len(blocks))
	for _, booking := range bookings {
		result = append(result, UnavailableWindow{PitchID: booking.PitchID, StartsAt: booking.StartsAt, EndsAt: booking.EndsAt, Kind: "booked"})
	}
	for _, block := range blocks {
		result = append(result, UnavailableWindow{PitchID: block.PitchID, StartsAt: block.StartsAt, EndsAt: block.EndsAt, Kind: "blocked"})
	}
	return result, nil
}

func (s *GormStore) JoinWaitlist(ctx context.Context, input WaitlistInput) (models.CityWaitlist, models.City, error) {
	var entry models.CityWaitlist
	var city models.City
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&city, "id = ?", input.CityID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInvalidCity
			}
			return err
		}

		findExisting := func() error {
			query := tx.Where("city_id = ?", input.CityID)
			switch {
			case input.Email != nil && input.Phone != nil:
				query = query.Where("email = ? OR phone = ?", *input.Email, *input.Phone)
			case input.Email != nil:
				query = query.Where("email = ?", *input.Email)
			case input.Phone != nil:
				query = query.Where("phone = ?", *input.Phone)
			}
			return query.First(&entry).Error
		}
		if err := findExisting(); err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		entry = models.CityWaitlist{CityID: input.CityID, Email: input.Email, Phone: input.Phone}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&entry)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return findExisting()
		}
		return nil
	})
	return entry, city, err
}

func (s *GormStore) requireActiveVenue(ctx context.Context, id uuid.UUID) error {
	var count int64
	err := s.db.WithContext(ctx).Model(&models.Venue{}).
		Where("id = ? AND status = ?", id, models.VenueStatusActive).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
