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

// Fixture scraping — no paid API. LiveScore's own site JSON is fetched with a
// plain GET; we keep only Tanzania plus the top five European leagues. A
// failed scrape emails the superadmin (once per day) as agreed.

var wantedLeagues = map[string]string{
	"England|Premier League": "Premier League",
	"Spain|LaLiga":           "LaLiga",
	"Italy|Serie A":          "Serie A",
	"Germany|Bundesliga":     "Bundesliga",
	"France|Ligue 1":         "Ligue 1",
}

type lsTeam struct {
	Nm string `json:"Nm"`
}
type lsEvent struct {
	Eid string   `json:"Eid"`
	T1  []lsTeam `json:"T1"`
	T2  []lsTeam `json:"T2"`
	Esd int64    `json:"Esd"`
	Eps string   `json:"Eps"`
	Tr1 string   `json:"Tr1"`
	Tr2 string   `json:"Tr2"`
}
type lsStage struct {
	Cnm    string    `json:"Cnm"`
	Snm    string    `json:"Snm"`
	Events []lsEvent `json:"Events"`
}
type lsDay struct {
	Stages []lsStage `json:"Stages"`
}

func parseEsd(esd int64) (time.Time, error) {
	// Esd arrives as yyyyMMddHHmmss already shifted to EAT (+3) by the URL.
	return time.ParseInLocation("20060102150405", fmt.Sprintf("%d", esd), eatZone())
}

// Basketball competitions worth the board: the NBA, WNBA and FIBA World Cup.
var wantedBasketball = map[string]string{
	"NBA":       "NBA",
	"WNBA":      "WNBA",
	"World Cup": "FIBA World Cup",
}

// ScrapeFixtures pulls today plus the next two days and upserts by external
// id, so kickoff-time changes and live statuses stay current.
func ScrapeFixtures() error {
	if initializers.DB == nil {
		return fmt.Errorf("no database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 20 * time.Second}
	total := 0
	type sportPass struct{ path, sport string }
	passes := []sportPass{{"soccer", "football"}, {"basketball", "basketball"}}
	for _, pass := range passes {
		if err := scrapeSportDays(ctx, client, pass.path, pass.sport, 8, &total); err != nil {
			return err
		}
	}
	cleanupFixtures()
	log.Printf("fixtures scrape: %d new fixtures", total)
	return nil
}

// cleanupFixtures keeps the table lean and honest:
//   - results older than 14 days are history nobody reads here — deleted;
//   - a fixture still "NS" a full day after kickoff was postponed, abandoned
//     or never tracked by the source — deleted rather than left "awaiting
//     result" forever (a rescheduled match returns via the sweep with a new
//     kickoff time anyway).
func cleanupFixtures() {
	now := time.Now().UTC()
	if result := initializers.DB.Where("kickoff_at < ?", now.AddDate(0, 0, -14)).
		Delete(&models.Fixture{}); result.RowsAffected > 0 {
		log.Printf("fixtures cleanup: purged %d old rows", result.RowsAffected)
	}
	if result := initializers.DB.Where("status = ? AND kickoff_at < ?", "NS", now.Add(-24*time.Hour)).
		Delete(&models.Fixture{}); result.RowsAffected > 0 {
		log.Printf("fixtures cleanup: dropped %d never-resolved rows", result.RowsAffected)
	}
}

func scrapeSportDays(ctx context.Context, client *http.Client, sportPath, sport string, days int, total *int) error {
	for offset := range days {
		day := time.Now().In(eatZone()).AddDate(0, 0, offset).Format("20060102")
		url := fmt.Sprintf("https://prod-public-api.livescore.com/v1/api/app/date/%s/%s/3?locale=en&MD=1", sportPath, day)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
		response, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("fetch %s: %w", day, err)
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return fmt.Errorf("fetch %s: status %d", day, response.StatusCode)
		}
		var payload lsDay
		err = json.NewDecoder(response.Body).Decode(&payload)
		response.Body.Close()
		if err != nil {
			return fmt.Errorf("decode %s: %w", day, err)
		}

		for _, stage := range payload.Stages {
			var league string
			var wanted bool
			if sport == "football" {
				league, wanted = wantedLeagues[stage.Cnm+"|"+stage.Snm]
				if stage.Cnm == "Tanzania" {
					league = stage.Snm
					// LiveScore also calls Tanzania's top flight "Premier League";
					// locals know it as Ligi Kuu Bara.
					if league == "Premier League" {
						league = "Ligi Kuu Bara"
					}
					wanted = true
				}
			} else {
				league, wanted = wantedBasketball[stage.Cnm]
			}
			if !wanted {
				continue
			}
			for _, event := range stage.Events {
				if len(event.T1) == 0 || len(event.T2) == 0 || event.Eid == "" {
					continue
				}
				kickoff, err := parseEsd(event.Esd)
				if err != nil {
					continue
				}
				externalID := sport + "-" + event.Eid
				fixture := models.Fixture{
					ExternalID: externalID, Sport: sport, League: league, Country: stage.Cnm,
					Home: event.T1[0].Nm, Away: event.T2[0].Nm,
					KickoffAt: kickoff.UTC(), Status: event.Eps,
					HomeScore: event.Tr1, AwayScore: event.Tr2,
				}
				var existing models.Fixture
				if initializers.DB.First(&existing, "external_id = ?", externalID).Error == nil {
					initializers.DB.Model(&models.Fixture{}).Where("id = ?", existing.ID).
						Updates(map[string]any{"kickoff_at": fixture.KickoffAt, "status": fixture.Status,
							"league": fixture.League, "home_score": fixture.HomeScore, "away_score": fixture.AwayScore})
				} else if err := initializers.DB.Create(&fixture).Error; err == nil {
					*total++
				}
			}
		}
	}
	return nil
}

// finishedStatuses are terminal LiveScore codes — anything else after
// kickoff counts as live (including minute markers like "45'" and "HT").
var finishedStatuses = map[string]bool{
	"FT": true, "AET": true, "AP": true, "Fin": true, "FIN": true,
	"Postp": true, "Canc": true, "Aband": true, "WO": true,
}

// FixtureIsLive: kicked off within the last four hours and not finished.
func FixtureIsLive(kickoffAt time.Time, status string) bool {
	now := time.Now().UTC()
	return kickoffAt.Before(now.Add(2*time.Minute)) &&
		kickoffAt.After(now.Add(-4*time.Hour)) &&
		status != "NS" && !finishedStatuses[status]
}

func anyLiveWindowOpen() bool {
	if initializers.DB == nil {
		return false
	}
	var count int64
	now := time.Now().UTC()
	// A fixture whose kickoff is near or in progress means scores can move.
	initializers.DB.Model(&models.Fixture{}).
		Where("kickoff_at BETWEEN ? AND ? AND status NOT IN ?",
			now.Add(-4*time.Hour), now.Add(10*time.Minute),
			[]string{"FT", "AET", "AP", "Fin", "FIN", "Postp", "Canc", "Aband", "WO"}).
		Count(&count)
	return count > 0
}

// scrapeToday refreshes only the current day — the cheap call used while
// matches are in play to keep scores and minutes moving.
func scrapeToday() error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 20 * time.Second}
	total := 0
	if err := scrapeSportDays(ctx, client, "soccer", "football", 1, &total); err != nil {
		return err
	}
	return scrapeSportDays(ctx, client, "basketball", "basketball", 1, &total)
}

// StartFixtureScraper runs two loops, tuned for cost:
//   - a full 8-day sweep at boot and every six hours (2 requests × 8 days × 2 sports)
//   - a live refresher every two minutes that fires ONLY while a match is
//     actually in its live window, and fetches just today's page.
//
// On failure the superadmin gets one email per day, not one per retry.
func StartFixtureScraper() {
	run := func() {
		if err := ScrapeFixtures(); err != nil {
			log.Printf("fixtures scrape failed: %v", err)
			notifyScrapeFailure(err)
		}
	}
	go func() {
		run()
		ticker := time.NewTicker(6 * time.Hour)
		for range ticker.C {
			run()
		}
	}()
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		for range ticker.C {
			if anyLiveWindowOpen() {
				if err := scrapeToday(); err != nil {
					log.Printf("live fixtures refresh failed: %v", err)
				}
			}
		}
	}()
}

func notifyScrapeFailure(cause error) {
	admin := adminNotifyEmail()
	if admin == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	day := time.Now().In(eatZone()).Format("2006-01-02")
	body := fmt.Sprintf(`<p style="margin:0 0 12px">The fixtures scrape failed and the match board may be stale.</p>%s`,
		factRows([][2]string{{"Error", cause.Error()}, {"Source", "livescore.com public JSON"}, {"Retry", "automatic, every 6 hours"}}))
	sendBranded(ctx, admin, "Fixtures scrape failed",
		fmt.Sprintf("Fixtures scrape failed:\n%v\n\nSource: livescore.com public JSON\nRetries continue automatically every 6 hours.", cause),
		brandedEmail{Preheader: "Fixtures scrape failed", Eyebrow: "Ops alert", Title: "The match board needs eyes.", BodyHTML: body, ActionLabel: "Open Superadmin", ActionURL: adminAppURL(), Footnote: "Superadmin ops alert — sent at most once per day."},
		"fixtures-scrape-failed-"+day)
}
