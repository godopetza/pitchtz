package store

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godopetza/pitchtz/models"
	"github.com/google/uuid"
)

type MemoryStore struct {
	mu       sync.RWMutex
	cities   []models.City
	venues   []models.Venue
	reviews  []models.Review
	bookings []models.Booking
	blocks   []models.SlotBlock
	waitlist []models.CityWaitlist
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (s *MemoryStore) SeedCities(cities ...models.City) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cities = append(s.cities, cities...)
}

func (s *MemoryStore) SeedVenues(venues ...models.Venue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.venues = append(s.venues, venues...)
}

func (s *MemoryStore) SeedReviews(reviews ...models.Review) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reviews = append(s.reviews, reviews...)
}

func (s *MemoryStore) SeedAvailability(bookings []models.Booking, blocks []models.SlotBlock) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bookings = append(s.bookings, bookings...)
	s.blocks = append(s.blocks, blocks...)
}

func (s *MemoryStore) WaitlistEntries() []models.CityWaitlist {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]models.CityWaitlist(nil), s.waitlist...)
}

func (s *MemoryStore) ListCities(_ context.Context) ([]models.City, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]models.City(nil), s.cities...)
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	if len(result) > 100 {
		result = result[:100]
	}
	return result, nil
}

func (s *MemoryStore) ListVenues(_ context.Context, filter VenueFilter) ([]models.Venue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]models.Venue, 0)
	for _, venue := range s.venues {
		if venue.Status != models.VenueStatusActive {
			continue
		}
		if filter.CityID != nil && venue.CityID != *filter.CityID {
			continue
		}
		if filter.Format != "" && !venueHasFormat(venue, filter.Format) {
			continue
		}
		if filter.Latitude != nil && filter.Longitude != nil &&
			haversineKM(*filter.Latitude, *filter.Longitude, venue.Latitude, venue.Longitude) > filter.RadiusKM {
			continue
		}
		result = append(result, venue)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	if len(result) > 100 {
		result = result[:100]
	}
	return result, nil
}

func (s *MemoryStore) GetVenue(_ context.Context, id uuid.UUID) (models.Venue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, venue := range s.venues {
		if venue.ID == id && venue.Status == models.VenueStatusActive {
			return venue, nil
		}
	}
	return models.Venue{}, ErrNotFound
}

func (s *MemoryStore) ListVenueReviews(_ context.Context, venueID uuid.UUID) ([]models.Review, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.hasActiveVenue(venueID) {
		return nil, ErrNotFound
	}
	result := make([]models.Review, 0)
	for _, review := range s.reviews {
		if review.VenueID == venueID {
			result = append(result, review)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

func (s *MemoryStore) ListVenueExtras(_ context.Context, venueID uuid.UUID) ([]models.ExtraCatalog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, venue := range s.venues {
		if venue.ID == venueID && venue.Status == models.VenueStatusActive {
			result := make([]models.ExtraCatalog, 0)
			for _, extra := range venue.Extras {
				if extra.Available {
					result = append(result, extra)
				}
			}
			return result, nil
		}
	}
	return nil, ErrNotFound
}

func (s *MemoryStore) ListUnavailable(_ context.Context, venueID uuid.UUID, from, to time.Time) ([]UnavailableWindow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pitchIDs := make(map[uuid.UUID]struct{})
	for _, venue := range s.venues {
		if venue.ID == venueID && venue.Status == models.VenueStatusActive {
			for _, pitch := range venue.Pitches {
				pitchIDs[pitch.ID] = struct{}{}
			}
			break
		}
	}
	if len(pitchIDs) == 0 && !s.hasActiveVenue(venueID) {
		return nil, ErrNotFound
	}

	result := make([]UnavailableWindow, 0)
	for _, booking := range s.bookings {
		if _, ok := pitchIDs[booking.PitchID]; !ok || !overlaps(booking.StartsAt, booking.EndsAt, from, to) {
			continue
		}
		switch booking.Status {
		case models.BookingStatusPending, models.BookingStatusConfirmed, models.BookingStatusPartPaid:
			result = append(result, UnavailableWindow{PitchID: booking.PitchID, StartsAt: booking.StartsAt, EndsAt: booking.EndsAt, Kind: "booked"})
		}
	}
	for _, block := range s.blocks {
		if _, ok := pitchIDs[block.PitchID]; ok && overlaps(block.StartsAt, block.EndsAt, from, to) {
			result = append(result, UnavailableWindow{PitchID: block.PitchID, StartsAt: block.StartsAt, EndsAt: block.EndsAt, Kind: "blocked"})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StartsAt.Before(result[j].StartsAt) })
	return result, nil
}

func (s *MemoryStore) JoinWaitlist(_ context.Context, input WaitlistInput) (models.CityWaitlist, models.City, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var city models.City
	found := false
	for _, candidate := range s.cities {
		if candidate.ID == input.CityID {
			city = candidate
			found = true
			break
		}
	}
	if !found {
		return models.CityWaitlist{}, models.City{}, ErrInvalidCity
	}

	for _, existing := range s.waitlist {
		if existing.CityID != input.CityID {
			continue
		}
		if sameStringPointer(existing.Email, input.Email) || sameStringPointer(existing.Phone, input.Phone) {
			return existing, city, nil
		}
	}

	entry := models.CityWaitlist{
		Base:   models.Base{ID: uuid.New(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
		CityID: input.CityID,
		Email:  input.Email,
		Phone:  input.Phone,
	}
	s.waitlist = append(s.waitlist, entry)
	return entry, city, nil
}

func (s *MemoryStore) hasActiveVenue(id uuid.UUID) bool {
	for _, venue := range s.venues {
		if venue.ID == id && venue.Status == models.VenueStatusActive {
			return true
		}
	}
	return false
}

func venueHasFormat(venue models.Venue, format string) bool {
	for _, pitch := range venue.Pitches {
		if strings.EqualFold(pitch.Format, format) {
			return true
		}
	}
	return false
}

func sameStringPointer(a, b *string) bool {
	return a != nil && b != nil && *a == *b
}

func overlaps(aStart, aEnd, bStart, bEnd time.Time) bool {
	return aStart.Before(bEnd) && aEnd.After(bStart)
}

func haversineKM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKM = 6371.0
	toRadians := math.Pi / 180
	dLat := (lat2 - lat1) * toRadians
	dLon := (lon2 - lon1) * toRadians
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*toRadians)*math.Cos(lat2*toRadians)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusKM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
