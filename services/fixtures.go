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
	Nm  string `json:"Nm"`
	Img string `json:"Img"`
}

// badgeURL turns the feed's relative badge path into the CDN URL clients can
// render directly; empty in stays empty out.
func badgeURL(img string) string {
	if img == "" {
		return ""
	}
	return "https://lsm-static-prod.livescore.com/medium/" + img
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
	Cnm      string    `json:"Cnm"`
	Snm      string    `json:"Snm"`
	BadgeURL string    `json:"badgeUrl"`
	Events   []lsEvent `json:"Events"`
}

// leagueBadgeURL resolves a competition crest. Note the host differs from the
// team badges — competition art lives on storage.livescore.com, not the lsm
// static CDN.
func leagueBadgeURL(badge string) string {
	if badge == "" {
		return ""
	}
	return "https://storage.livescore.com/images/competition/high/" + badge
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

// wantedStage decides, per sport, whether a LiveScore stage belongs on the
// board and what to call its league and country columns.
func wantedStage(sport, cnm, snm string) (league, country string, ok bool) {
	switch sport {
	case "football":
		if cnm == "Tanzania" {
			league = snm
			if league == "Premier League" {
				league = "Ligi Kuu Bara"
			}
			return league, cnm, true
		}
		if name, found := wantedLeagues[cnm+"|"+snm]; found {
			return name, cnm, true
		}
	case "basketball":
		if name, found := wantedBasketball[cnm]; found {
			return name, cnm, true
		}
	case "tennis":
		// Tour-level only: ATP/WTA minus Challenger/ITF undercards.
		if (strings.HasPrefix(cnm, "ATP") || strings.HasPrefix(cnm, "WTA") || strings.Contains(cnm, "Grand Slam")) &&
			!strings.Contains(cnm, "Challenger") {
			return snm, cnm, true
		}
	case "cricket":
		if cnm == "Test Series" || strings.Contains(cnm, "International") || strings.Contains(snm, "World Cup") {
			return snm, cnm, true
		}
		for _, big := range []string{"Indian Premier League", "Caribbean Premier League", "Big Bash", "The Hundred"} {
			if strings.Contains(snm, big) {
				return snm, cnm, true
			}
		}
	}
	return "", "", false
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
	passes := []sportPass{{"soccer", "football"}, {"basketball", "basketball"}, {"tennis", "tennis"}, {"cricket", "cricket"}}
	for _, pass := range passes {
		if err := scrapeSportRange(ctx, client, pass.path, pass.sport, -5, 13, &total); err != nil {
			return err
		}
	}
	cleanupFixtures()
	refreshTimelines()
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
	// Finished matches are cheap to keep and are what a team's recent form is
	// made of, so they live for 90 days; anything else ages out at 14.
	if result := initializers.DB.
		Where("kickoff_at < ? AND status IN ?", now.AddDate(0, 0, -90),
			[]string{"FT", "AET", "AP", "Fin", "FIN"}).
		Delete(&models.Fixture{}); result.RowsAffected > 0 {
		log.Printf("fixtures cleanup: purged %d finished rows older than 90 days", result.RowsAffected)
	}
	if result := initializers.DB.
		Where("kickoff_at < ? AND status NOT IN ?", now.AddDate(0, 0, -14),
			[]string{"FT", "AET", "AP", "Fin", "FIN"}).
		Delete(&models.Fixture{}); result.RowsAffected > 0 {
		log.Printf("fixtures cleanup: purged %d unfinished old rows", result.RowsAffected)
	}
	if result := initializers.DB.Where("status = ? AND kickoff_at < ?", "NS", now.Add(-24*time.Hour)).
		Delete(&models.Fixture{}); result.RowsAffected > 0 {
		log.Printf("fixtures cleanup: dropped %d never-resolved rows", result.RowsAffected)
	}
	// Legacy rows from before external ids were sport-prefixed can never be
	// upserted again — they duplicate their prefixed twins, so drop them.
	if result := initializers.DB.
		Where("external_id NOT LIKE 'football-%' AND external_id NOT LIKE 'basketball-%' AND external_id NOT LIKE 'tennis-%' AND external_id NOT LIKE 'cricket-%' AND external_id NOT LIKE 'f1-%' AND external_id NOT LIKE 'boxing-%'").
		Delete(&models.Fixture{}); result.RowsAffected > 0 {
		log.Printf("fixtures cleanup: dropped %d legacy-id rows", result.RowsAffected)
	}
}

func scrapeSportDays(ctx context.Context, client *http.Client, sportPath, sport string, days int, total *int) error {
	return scrapeSportRange(ctx, client, sportPath, sport, 0, days, total)
}

// scrapeSportRange walks day offsets [from, from+days) — negative offsets
// backfill finished results so a team's recent form exists straight away
// rather than accumulating over the following weeks.
func scrapeSportRange(ctx context.Context, client *http.Client, sportPath, sport string, from, days int, total *int) error {
	for step := range days {
		offset := from + step
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
			league, country, wanted := wantedStage(sport, stage.Cnm, stage.Snm)
			if !wanted {
				continue
			}
			_ = country
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
					ExternalID: externalID, Sport: sport, League: league, Country: country,
					Home: event.T1[0].Nm, Away: event.T2[0].Nm,
					HomeImg: badgeURL(event.T1[0].Img), AwayImg: badgeURL(event.T2[0].Img),
					LeagueImg: leagueBadgeURL(stage.BadgeURL),
					KickoffAt: kickoff.UTC(), Status: event.Eps,
					HomeScore: event.Tr1, AwayScore: event.Tr2,
				}
				var existing models.Fixture
				if initializers.DB.First(&existing, "external_id = ?", externalID).Error == nil {
					initializers.DB.Model(&models.Fixture{}).Where("id = ?", existing.ID).
						Updates(map[string]any{"kickoff_at": fixture.KickoffAt, "status": fixture.Status,
							"league": fixture.League, "home_score": fixture.HomeScore, "away_score": fixture.AwayScore,
							"home_img": fixture.HomeImg, "away_img": fixture.AwayImg, "league_img": fixture.LeagueImg})
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
	for _, pass := range []struct{ path, sport string }{
		{"soccer", "football"}, {"basketball", "basketball"}, {"tennis", "tennis"}, {"cricket", "cricket"},
	} {
		if err := scrapeSportDays(ctx, client, pass.path, pass.sport, 1, &total); err != nil {
			return err
		}
	}
	return nil
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
				refreshTimelines()
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

// ── Inline scorer timelines ──────────────────────────────────────────────────

type timelineEvent struct {
	M  int    `json:"m"`
	P  string `json:"p"`
	A  string `json:"a,omitempty"` // assist, for goal events
	S  string `json:"s,omitempty"`
	T  string `json:"t"`
	Tm int    `json:"tm,omitempty"` // side: 1 home, 2 away
}

var timelineLabels = map[int]string{
	36: "goal", 37: "own_goal", 38: "penalty_goal",
	43: "yellow_card", 45: "red_card", 44: "yellow_red_card",
}

type lsTLIncident struct {
	Min  int            `json:"Min"`
	IT   int            `json:"IT"`
	Nm   int            `json:"Nm"` // side: 1 home, 2 away
	Pn   string         `json:"Pn"`
	Sc   []int          `json:"Sc"`
	Incs []lsTLIncident `json:"Incs"`
}

func flattenTimeline(items []lsTLIncident, out *[]timelineEvent) {
	for _, item := range items {
		if len(item.Incs) > 0 {
			// A group with a score is one goal: the IT-36/37/38 child is the
			// scorer and the IT-63 child within the SAME group is the assist.
			var goal *timelineEvent
			assist := ""
			for _, child := range item.Incs {
				switch child.IT {
				case 36, 37, 38:
					label := timelineLabels[child.IT]
					side := child.Nm
					if side == 0 {
						side = item.Nm
					}
					goal = &timelineEvent{M: child.Min, P: child.Pn, T: label, Tm: side}
					if len(child.Sc) == 2 {
						goal.S = fmt.Sprintf("%d-%d", child.Sc[0], child.Sc[1])
					}
				case 63:
					assist = child.Pn
				}
			}
			if goal != nil {
				goal.A = assist
				*out = append(*out, *goal)
			} else {
				flattenTimeline(item.Incs, out)
			}
			continue
		}
		label, known := timelineLabels[item.IT]
		if !known {
			if len(item.Sc) == 2 {
				label = "goal"
			} else {
				continue
			}
		}
		event := timelineEvent{M: item.Min, P: item.Pn, T: label, Tm: item.Nm}
		if len(item.Sc) == 2 {
			event.S = fmt.Sprintf("%d-%d", item.Sc[0], item.Sc[1])
		}
		*out = append(*out, event)
	}
}

// refreshTimelines pulls incidents for matches that are live now, plus ones
// that finished in the last three hours with an empty timeline (so a match
// ending between polls still gets its final scorers). Bounded per cycle.
func refreshTimelines() {
	if initializers.DB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	now := time.Now().UTC()

	var fixtures []models.Fixture
	initializers.DB.
		Where("sport IN ? AND kickoff_at BETWEEN ? AND ? AND status <> 'NS'",
			[]string{"football", "basketball"}, now.Add(-5*time.Hour), now).
		Limit(16).Find(&fixtures)

	client := &http.Client{Timeout: 15 * time.Second}
	for _, fixture := range fixtures {
		finished := finishedStatuses[fixture.Status]
		// Finished + already captured → nothing to do.
		if finished && string(fixture.Timeline) != "[]" && len(fixture.Timeline) > 4 {
			continue
		}
		sportPath := "soccer"
		if fixture.Sport == "basketball" {
			sportPath = "basketball"
		}
		eid := strings.TrimPrefix(fixture.ExternalID, fixture.Sport+"-")
		url := fmt.Sprintf("https://prod-public-api.livescore.com/v1/api/app/incidents/%s/%s?locale=en", sportPath, eid)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
		response, err := client.Do(req)
		if err != nil {
			continue
		}
		var payload struct {
			Incs map[string][]lsTLIncident `json:"Incs"`
		}
		err = json.NewDecoder(response.Body).Decode(&payload)
		response.Body.Close()
		if err != nil || len(payload.Incs) == 0 {
			continue
		}
		events := []timelineEvent{}
		for _, period := range payload.Incs {
			flattenTimeline(period, &events)
		}
		if len(events) == 0 {
			continue
		}
		for i := 1; i < len(events); i++ {
			for j := i; j > 0 && events[j-1].M > events[j].M; j-- {
				events[j-1], events[j] = events[j], events[j-1]
			}
		}
		if len(events) > 24 {
			events = events[:24]
		}
		encoded, err := json.Marshal(events)
		if err != nil {
			continue
		}
		initializers.DB.Model(&models.Fixture{}).Where("id = ?", fixture.ID).
			Update("timeline", encoded)
	}
}
