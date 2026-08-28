package initializers

import (
	"log"
	"math"

	"github.com/godopetza/pitchtz/models"
)

// Tanzania's major football cities. Live cities can host venues today; the
// rest collect waitlist interest on the client site. Upserted by name at boot
// so the list stays correct without manual SQL.
var tanzaniaCities = []models.City{
	{Name: "Dar es Salaam", Status: models.CityStatusLive, Latitude: -6.7924, Longitude: 39.2083},
	{Name: "Zanzibar", Status: models.CityStatusLive, Latitude: -6.1659, Longitude: 39.2026},
	{Name: "Arusha", Status: models.CityStatusWaitlist, Latitude: -3.3869, Longitude: 36.6830},
	{Name: "Mwanza", Status: models.CityStatusWaitlist, Latitude: -2.5164, Longitude: 32.9175},
	{Name: "Dodoma", Status: models.CityStatusWaitlist, Latitude: -6.1630, Longitude: 35.7516},
	{Name: "Mbeya", Status: models.CityStatusWaitlist, Latitude: -8.9094, Longitude: 33.4608},
	{Name: "Morogoro", Status: models.CityStatusWaitlist, Latitude: -6.8278, Longitude: 37.6591},
	{Name: "Tanga", Status: models.CityStatusWaitlist, Latitude: -5.0893, Longitude: 39.0993},
}

// SeedCities upserts the city list and then reassigns every venue with real
// coordinates to its nearest city — so a Zanzibar venue enrolled while only
// Dar existed is corrected automatically.
func SeedCities() {
	if DB == nil {
		return
	}
	for _, seed := range tanzaniaCities {
		var existing models.City
		if err := DB.First(&existing, "LOWER(name) = LOWER(?)", seed.Name).Error; err != nil {
			if err := DB.Create(&seed).Error; err != nil {
				log.Printf("seed city %s: %v", seed.Name, err)
			}
			continue
		}
		// Keep coordinates fresh; never downgrade a live city.
		updates := map[string]any{"latitude": seed.Latitude, "longitude": seed.Longitude}
		if existing.Status != models.CityStatusLive && seed.Status == models.CityStatusLive {
			updates["status"] = models.CityStatusLive
		}
		DB.Model(&models.City{}).Where("id = ?", existing.ID).Updates(updates)
	}

	var cities []models.City
	DB.Find(&cities)
	var venues []models.Venue
	DB.Where("latitude <> 0 AND longitude <> 0").Find(&venues)
	for _, venue := range venues {
		best := venue.CityID
		bestDistance := math.MaxFloat64
		for _, city := range cities {
			d := (city.Latitude-venue.Latitude)*(city.Latitude-venue.Latitude) +
				(city.Longitude-venue.Longitude)*(city.Longitude-venue.Longitude)
			if d < bestDistance {
				bestDistance = d
				best = city.ID
			}
		}
		if best != venue.CityID {
			DB.Model(&models.Venue{}).Where("id = ?", venue.ID).Update("city_id", best)
			log.Printf("venue %s reassigned to nearest city", venue.Name)
		}
	}
}
