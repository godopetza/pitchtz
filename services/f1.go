package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
)

// Formula 1 via the free Jolpica (Ergast successor) API — race calendar and
// results, no key. A race row: home = grand prix name, away = circuit.

type jolpicaRace struct {
	Round    string `json:"round"`
	RaceName string `json:"raceName"`
	Date     string `json:"date"`
	Time     string `json:"time"`
	Circuit  struct {
		CircuitName string `json:"circuitName"`
		Location    struct {
			Country string `json:"country"`
		} `json:"Location"`
	} `json:"Circuit"`
}

func jolpicaGet(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "pitchtz/1.0")
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("jolpica status %d", response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(out)
}

// ScrapeF1 upserts the current season's races and settles finished ones with
// the winner. Live window: status flips to "Live" during the race itself.
func ScrapeF1() error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var schedule struct {
		MRData struct {
			RaceTable struct {
				Season string        `json:"season"`
				Races  []jolpicaRace `json:"Races"`
			} `json:"RaceTable"`
		} `json:"MRData"`
	}
	if err := jolpicaGet(ctx, "https://api.jolpi.ca/ergast/f1/current.json", &schedule); err != nil {
		return err
	}
	season := schedule.MRData.RaceTable.Season
	now := time.Now().UTC()

	for _, race := range schedule.MRData.RaceTable.Races {
		start, err := time.Parse(time.RFC3339, race.Date+"T"+race.Time)
		if err != nil {
			continue
		}
		externalID := fmt.Sprintf("f1-%s-%s", season, race.Round)
		status := "NS"
		if now.After(start) && now.Before(start.Add(3*time.Hour)) {
			status = "Live"
		}
		fixture := models.Fixture{
			ExternalID: externalID, Sport: "f1", League: "Formula 1", Country: "F1",
			Home: race.RaceName, Away: race.Circuit.CircuitName,
			KickoffAt: start, Status: status,
		}
		var existing models.Fixture
		if initializers.DB.First(&existing, "external_id = ?", externalID).Error == nil {
			if existing.Status != "FT" {
				updates := map[string]any{"kickoff_at": start}
				if status == "Live" || existing.Status == "NS" {
					updates["status"] = status
				}
				initializers.DB.Model(&models.Fixture{}).Where("id = ?", existing.ID).Updates(updates)
				if now.After(start.Add(2 * time.Hour)) {
					settleF1Race(ctx, existing.ID, season, race.Round)
				}
			}
		} else {
			initializers.DB.Create(&fixture)
			if now.After(start.Add(2 * time.Hour)) {
				var created models.Fixture
				if initializers.DB.First(&created, "external_id = ?", externalID).Error == nil {
					settleF1Race(ctx, created.ID, season, race.Round)
				}
			}
		}
	}
	return nil
}

func settleF1Race(ctx context.Context, fixtureID any, season, round string) {
	var results struct {
		MRData struct {
			RaceTable struct {
				Races []struct {
					Results []struct {
						Driver struct {
							Code       string `json:"code"`
							FamilyName string `json:"familyName"`
						} `json:"Driver"`
					} `json:"Results"`
				} `json:"Races"`
			} `json:"RaceTable"`
		} `json:"MRData"`
	}
	url := fmt.Sprintf("https://api.jolpi.ca/ergast/f1/%s/%s/results.json?limit=1", season, round)
	if err := jolpicaGet(ctx, url, &results); err != nil {
		return
	}
	races := results.MRData.RaceTable.Races
	if len(races) == 0 || len(races[0].Results) == 0 {
		return
	}
	winner := races[0].Results[0].Driver.FamilyName
	initializers.DB.Model(&models.Fixture{}).Where("id = ?", fixtureID).
		Updates(map[string]any{"status": "FT", "home_score": winner, "away_score": ""})
}

// StartF1Scraper: schedule sweep at boot and every 6 hours (race weekends
// only change a few times a year; results settle on the post-race sweep).
func StartF1Scraper() {
	run := func() {
		if err := ScrapeF1(); err != nil {
			log.Printf("f1 scrape failed: %v", err)
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
