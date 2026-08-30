package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
)

// Boxing via TheSportsDB's free tier (league 4445) — headline fights only,
// no live rounds. A fight row: home / away = the two fighters. Like F1 the
// calendar is sparse, so the public list gives boxing a wide date window.

const boxingLeagueID = "4445"

type tsdbEvent struct {
	IDEvent   string `json:"idEvent"`
	IDLeague  string `json:"idLeague"`
	StrEvent  string `json:"strEvent"`
	Timestamp string `json:"strTimestamp"`
	Country   string `json:"strCountry"`
	Status    string `json:"strStatus"`
}

func scrapeBoxing() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 20 * time.Second}
	total := 0
	for offset := range 14 {
		day := time.Now().UTC().AddDate(0, 0, offset).Format("2006-01-02")
		url := fmt.Sprintf("https://www.thesportsdb.com/api/v1/json/123/eventsday.php?d=%s&s=Fighting", day)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		var payload struct {
			Events []tsdbEvent `json:"events"`
		}
		err = json.NewDecoder(response.Body).Decode(&payload)
		response.Body.Close()
		if err != nil {
			continue // some days return a bare null body
		}
		for _, event := range payload.Events {
			if event.IDLeague != boxingLeagueID || event.IDEvent == "" || event.Timestamp == "" {
				continue
			}
			start, err := time.Parse("2006-01-02T15:04:05", event.Timestamp)
			if err != nil {
				continue
			}
			home, away, found := strings.Cut(event.StrEvent, " vs ")
			if !found {
				home, away = event.StrEvent, ""
			}
			status := "NS"
			if event.Status == "FT" || strings.HasPrefix(event.Status, "Fin") {
				status = "FT"
			}
			externalID := "boxing-" + event.IDEvent
			fixture := models.Fixture{
				ExternalID: externalID, Sport: "boxing", League: "Boxing", Country: event.Country,
				Home: strings.TrimSpace(home), Away: strings.TrimSpace(away),
				KickoffAt: start.UTC(), Status: status,
			}
			var existing models.Fixture
			if initializers.DB.First(&existing, "external_id = ?", externalID).Error == nil {
				initializers.DB.Model(&models.Fixture{}).Where("id = ?", existing.ID).
					Updates(map[string]any{"kickoff_at": fixture.KickoffAt, "status": fixture.Status,
						"home": fixture.Home, "away": fixture.Away, "country": fixture.Country})
			} else if err := initializers.DB.Create(&fixture).Error; err == nil {
				total++
			}
		}
		time.Sleep(400 * time.Millisecond) // stay polite on the free tier
	}
	// The source rarely flips status itself — settle fights well past their
	// start so the NS-cleanup rule never eats a real card.
	initializers.DB.Model(&models.Fixture{}).
		Where("sport = ? AND status = ? AND kickoff_at < ?", "boxing", "NS", time.Now().UTC().Add(-6*time.Hour)).
		Update("status", "FT")
	if total > 0 {
		log.Printf("boxing scrape: %d new fights", total)
	}
	return nil
}

// StartBoxingScraper: boot plus every 6 hours — cards change rarely.
func StartBoxingScraper() {
	run := func() {
		if err := scrapeBoxing(); err != nil {
			log.Printf("boxing scrape failed: %v", err)
		}
	}
	go func() {
		run()
		ticker := time.NewTicker(6 * time.Hour)
		for range ticker.C {
			run()
		}
	}()
}
