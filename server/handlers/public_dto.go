package handlers

import (
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/godopetza/pitchtz/models"
	"github.com/google/uuid"
)

type CityPublicDTO struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	LaunchETA *time.Time `json:"launch_eta,omitempty"`
	Latitude  float64    `json:"latitude"`
	Longitude float64    `json:"longitude"`
}

type PitchPublicDTO struct {
	ID           uuid.UUID       `json:"id"`
	Name         string          `json:"name"`
	Format       string          `json:"format"`
	Surface      string          `json:"surface"`
	BasePriceTZS int64           `json:"base_price_tzs"`
	PhotoURL     string          `json:"photo_url,omitempty"`
	PhotoURLs    []string        `json:"photo_urls,omitempty"`
	Status       string          `json:"status"`
	OpenHours    json.RawMessage `json:"open_hours"`
}

type VenuePhotoPublicDTO struct {
	URL   string `json:"url,omitempty"`
	R2Key string `json:"r2_key,omitempty"`
	Sort  int    `json:"sort"`
	Alt   string `json:"alt"`
}

type ExtraPublicDTO struct {
	ID        uuid.UUID `json:"id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	PriceTZS  int64     `json:"price_tzs"`
	Unit      string    `json:"unit"`
	Stock     int       `json:"stock"`
	Available bool      `json:"available"`
}

type VenuePublicDTO struct {
	ID                uuid.UUID             `json:"id"`
	Name              string                `json:"name"`
	Area              string                `json:"area"`
	City              CityPublicDTO         `json:"city"`
	Latitude          float64               `json:"latitude"`
	Longitude         float64               `json:"longitude"`
	Verified          bool                  `json:"verified"`
	Rating            float64               `json:"rating"`
	Amenities         json.RawMessage       `json:"amenities"`
	Rules             json.RawMessage       `json:"rules"`
	CancelWindowHours int                   `json:"cancel_window_hours"`
	AutoConfirm       bool                  `json:"auto_confirm"`
	PriceFromTZS      int64                 `json:"price_from_tzs"`
	Pitches           []PitchPublicDTO      `json:"pitches"`
	Status            string                `json:"status"`
	OpenHours         json.RawMessage       `json:"open_hours"`
	Photos            []VenuePhotoPublicDTO `json:"photos"`
	Extras            []ExtraPublicDTO      `json:"extras"`
}

type ReviewPublicDTO struct {
	ID         uuid.UUID       `json:"id"`
	Stars      int             `json:"stars"`
	Text       string          `json:"text"`
	Tags       json.RawMessage `json:"tags"`
	OwnerReply string          `json:"owner_reply,omitempty"`
	RepliedAt  *time.Time      `json:"replied_at,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

func cityPublicDTO(city models.City) CityPublicDTO {
	return CityPublicDTO{
		ID: city.ID, Name: city.Name, Status: city.Status, LaunchETA: city.LaunchETA,
		Latitude: city.Latitude, Longitude: city.Longitude,
	}
}

func venuePublicDTO(venue models.Venue) VenuePublicDTO {
	dto := VenuePublicDTO{
		ID: venue.ID, Name: venue.Name, Area: venue.Area, City: cityPublicDTO(venue.City),
		Latitude: venue.Latitude, Longitude: venue.Longitude, Verified: venue.Verified,
		Rating: venue.Rating, Amenities: validJSON(venue.Amenities, "[]"), Rules: validJSON(venue.Rules, "[]"),
		CancelWindowHours: venue.CancelWindowHours, AutoConfirm: venue.AutoConfirm,
		Pitches: make([]PitchPublicDTO, 0, len(venue.Pitches)),
		Photos:  make([]VenuePhotoPublicDTO, 0, len(venue.Photos)),
		Extras:  make([]ExtraPublicDTO, 0, len(venue.Extras)),
		Status:  venue.Status, OpenHours: openHoursOrDefault(venue.OpenHours)}
	for _, pitch := range venue.Pitches {
		base := strings.TrimRight(os.Getenv("ASSET_BASE_URL"), "/")
		photoURLs := make([]string, 0, len(pitch.Photos)+1)
		if base != "" {
			for _, photo := range pitch.Photos {
				if photo.R2Key != "" {
					photoURLs = append(photoURLs, base+"/"+strings.TrimLeft(photo.R2Key, "/"))
				}
			}
			// Legacy single photo stays the lead image if no gallery rows exist.
			if len(photoURLs) == 0 && pitch.PhotoR2Key != "" {
				photoURLs = append(photoURLs, base+"/"+strings.TrimLeft(pitch.PhotoR2Key, "/"))
			}
		}
		photoURL := ""
		if len(photoURLs) > 0 {
			photoURL = photoURLs[0]
		}
		dto.Pitches = append(dto.Pitches, PitchPublicDTO{
			ID: pitch.ID, Name: pitch.Name, Format: pitch.Format, Surface: pitch.Surface,
			BasePriceTZS: pitch.BasePriceTZS, PhotoURL: photoURL, PhotoURLs: photoURLs, Status: pitch.Status, OpenHours: openHoursOrDefault(pitch.OpenHours),
		})
		if dto.PriceFromTZS == 0 || pitch.BasePriceTZS < dto.PriceFromTZS {
			dto.PriceFromTZS = pitch.BasePriceTZS
		}
	}
	assetBase := strings.TrimRight(os.Getenv("ASSET_BASE_URL"), "/")
	for _, photo := range venue.Photos {
		url := ""
		if assetBase != "" {
			url = assetBase + "/" + strings.TrimLeft(photo.R2Key, "/")
		}
		dto.Photos = append(dto.Photos, VenuePhotoPublicDTO{URL: url, R2Key: photo.R2Key, Sort: photo.Sort, Alt: photo.Alt})
	}
	for _, extra := range venue.Extras {
		if extra.Available {
			dto.Extras = append(dto.Extras, extraPublicDTO(extra))
		}
	}
	return dto
}

func extraPublicDTO(extra models.ExtraCatalog) ExtraPublicDTO {
	return ExtraPublicDTO{
		ID: extra.ID, Kind: extra.Kind, Name: extra.Name, PriceTZS: extra.PriceTZS,
		Unit: extra.Unit, Stock: extra.Stock, Available: extra.Available,
	}
}

func reviewPublicDTO(review models.Review) ReviewPublicDTO {
	return ReviewPublicDTO{
		ID: review.ID, Stars: review.Stars, Text: review.Text, Tags: validJSON(review.Tags, "[]"),
		OwnerReply: review.OwnerReply, RepliedAt: review.RepliedAt, CreatedAt: review.CreatedAt,
	}
}

// defaultOpenHours is the one answer every surface gives when a venue has not
// set its own hours. Before this existed the owner slot grid assumed
// 08:00–21:00, the hours editor 08:00–23:00 and the client 16:00–21:30, so the
// same unconfigured venue advertised three different days. Serving a single
// default here keeps them honest; an owner who saves real hours overrides it.
const defaultOpenHours = `{"mon":{"open":"08:00","close":"22:00"},"tue":{"open":"08:00","close":"22:00"},"wed":{"open":"08:00","close":"22:00"},"thu":{"open":"08:00","close":"22:00"},"fri":{"open":"08:00","close":"22:00"},"sat":{"open":"08:00","close":"22:00"},"sun":{"open":"08:00","close":"22:00"}}`

// openHoursOrDefault returns saved hours, or the shared default when a venue
// or pitch has none — never an empty object, which each client used to fill
// in with a different guess.
func openHoursOrDefault(value []byte) json.RawMessage {
	trimmed := strings.TrimSpace(string(value))
	if trimmed == "" || trimmed == "{}" || trimmed == "null" || !json.Valid(value) {
		return json.RawMessage(defaultOpenHours)
	}
	return json.RawMessage(value)
}

func validJSON(value []byte, fallback string) json.RawMessage {
	if !json.Valid(value) {
		return json.RawMessage(fallback)
	}
	return json.RawMessage(value)
}
