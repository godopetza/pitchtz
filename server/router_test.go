package server_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/server"
	"github.com/godopetza/pitchtz/store"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type fixture struct {
	store *store.MemoryStore
	city  models.City
	venue models.Venue
	pitch models.Pitch
}

func newFixture() fixture {
	city := models.City{
		Base: models.Base{ID: uuid.New()}, Name: "Dar es Salaam", Status: models.CityStatusLive,
		Latitude: -6.7924, Longitude: 39.2083,
	}
	pitch := models.Pitch{
		Base: models.Base{ID: uuid.New()}, Name: "Pitch A", Format: "7", Surface: "turf",
		BasePriceTZS: 60000, OpenHours: datatypes.JSON([]byte(`{"monday":["08:00","23:00"]}`)),
	}
	venue := models.Venue{
		Base: models.Base{ID: uuid.New()}, OwnerID: uuid.New(), CityID: city.ID,
		Name: "Mikocheni Arena", Area: "Mikocheni", Latitude: -6.766, Longitude: 39.229,
		Status: models.VenueStatusActive, FeeRateBPS: 1000, Verified: true, Rating: 4.7,
		Amenities: datatypes.JSON([]byte(`["changing_room","parking"]`)),
		Rules:     datatypes.JSON([]byte(`["no_smoking"]`)), CancelWindowHours: 24,
		City: city, Pitches: []models.Pitch{pitch},
		Extras: []models.ExtraCatalog{{
			Base: models.Base{ID: uuid.New()}, Kind: "rent", Name: "Bibs", PriceTZS: 5000,
			Unit: "item", Stock: 20, Available: true,
		}},
	}
	pitch.VenueID = venue.ID
	venue.Pitches[0].VenueID = venue.ID
	venue.Extras[0].VenueID = venue.ID

	memory := store.NewMemoryStore()
	memory.SeedCities(city)
	memory.SeedVenues(venue, models.Venue{
		Base: models.Base{ID: uuid.New()}, OwnerID: uuid.New(), CityID: city.ID,
		Name: "Unapproved Venue", Area: "Masaki", Status: models.VenueStatusPending, City: city,
	})
	return fixture{store: memory, city: city, venue: venue, pitch: venue.Pitches[0]}
}

func newRouter(f fixture) http.Handler {
	return server.NewRouterWithDeps(server.Deps{Catalog: f.store, Waitlist: f.store})
}

func TestHealth(t *testing.T) {
	f := newFixture()
	response := performRequest(newRouter(f), http.MethodGet, "/health", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"service":"pitchtz"`) {
		t.Fatalf("unexpected health response: %s", response.Body.String())
	}
}

func TestClientOpenAPIExcludesPrivateSurfaces(t *testing.T) {
	f := newFixture()
	response := performRequest(newRouter(f), http.MethodGet, "/openapi.yaml", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	contract := response.Body.String()
	for _, required := range []string{"/auth/otp/send:", "/bookings:", "/teams:", "/waitlist:"} {
		if !strings.Contains(contract, required) {
			t.Fatalf("mobile contract is missing %q", required)
		}
	}
	for _, forbidden := range []string{"/admin/", "/owner/", "/webhooks/"} {
		if strings.Contains(contract, forbidden) {
			t.Fatalf("private surface %q leaked into the mobile contract", forbidden)
		}
	}
}

func TestPublicVenueExcludesPrivateFieldsAndPendingVenues(t *testing.T) {
	f := newFixture()
	router := newRouter(f)

	list := performRequest(router, http.MethodGet, "/v1/venues?format=7", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", list.Code, list.Body.String())
	}
	if strings.Contains(list.Body.String(), "Unapproved Venue") {
		t.Fatalf("pending venue leaked into public list: %s", list.Body.String())
	}
	if !strings.Contains(list.Body.String(), "Mikocheni Arena") {
		t.Fatalf("active venue missing from public list: %s", list.Body.String())
	}

	detail := performRequest(router, http.MethodGet, "/v1/venues/"+f.venue.ID.String(), nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", detail.Code, detail.Body.String())
	}
	for _, forbidden := range []string{"owner_id", "ownerId", "fee_rate", "feeRate", f.venue.OwnerID.String(), "r2_key"} {
		if strings.Contains(detail.Body.String(), forbidden) {
			t.Fatalf("private field %q leaked into public response: %s", forbidden, detail.Body.String())
		}
	}
	if !strings.Contains(detail.Body.String(), `"price_from_tzs":60000`) {
		t.Fatalf("public price summary missing: %s", detail.Body.String())
	}
}

func TestAvailabilityDoesNotExposeBookingIdentity(t *testing.T) {
	f := newFixture()
	startsAt := time.Date(2026, time.August, 26, 18, 0, 0, 0, time.FixedZone("EAT", 3*60*60))
	bookingID, userID := uuid.New(), uuid.New()
	f.store.SeedAvailability([]models.Booking{{
		Base: models.Base{ID: bookingID}, PitchID: f.pitch.ID, UserID: &userID,
		StartsAt: startsAt, EndsAt: startsAt.Add(time.Hour), Status: models.BookingStatusConfirmed,
	}}, nil)

	url := "/v1/venues/" + f.venue.ID.String() + "/availability?date=2026-08-26"
	response := performRequest(newRouter(f), http.MethodGet, url, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"kind":"booked"`) {
		t.Fatalf("expected booked window: %s", response.Body.String())
	}
	for _, forbidden := range []string{bookingID.String(), userID.String(), "user_id", "booking_id"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("booking identity %q leaked: %s", forbidden, response.Body.String())
		}
	}
}

func TestWaitlistNormalizesAndDeduplicatesContact(t *testing.T) {
	f := newFixture()
	router := newRouter(f)
	firstBody := []byte(`{"city_id":"` + f.city.ID.String() + `","email":" Player@Example.COM ","phone":"0712 345 678"}`)
	secondBody := []byte(`{"city_id":"` + f.city.ID.String() + `","email":"player@example.com","phone":"+255 712 345 678"}`)

	first := performRequest(router, http.MethodPost, "/v1/waitlist", firstBody)
	second := performRequest(router, http.MethodPost, "/v1/waitlist", secondBody)
	if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted {
		t.Fatalf("expected both requests accepted, got %d and %d: %s / %s", first.Code, second.Code, first.Body.String(), second.Body.String())
	}
	entries := f.store.WaitlistEntries()
	if len(entries) != 1 {
		t.Fatalf("expected one deduplicated entry, got %d", len(entries))
	}
	if entries[0].Email == nil || *entries[0].Email != "player@example.com" {
		t.Fatalf("email was not normalized: %#v", entries[0].Email)
	}
	if entries[0].Phone == nil || *entries[0].Phone != "+255712345678" {
		t.Fatalf("phone was not normalized: %#v", entries[0].Phone)
	}
	if strings.Contains(first.Body.String(), "player@example.com") || strings.Contains(first.Body.String(), "+255712345678") {
		t.Fatalf("waitlist response leaked contact data: %s", first.Body.String())
	}
}

func performRequest(router http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
