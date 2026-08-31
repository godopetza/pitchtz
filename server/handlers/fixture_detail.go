package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/utils"
)

// Match centre: on-demand detail for one fixture — goal scorers with minutes,
// cards, half-time scores and lineups (LiveScore), or the podium for F1
// (Jolpica). Fetched only when someone opens a match and cached for 60s, so
// the cost stays one upstream request per curious minute.

var detailCache = struct {
	sync.Mutex
	entries map[string]detailEntry
}{entries: map[string]detailEntry{}}

type detailEntry struct {
	payload gin.H
	expires time.Time
}

var sportPath = map[string]string{
	"football": "soccer", "basketball": "basketball", "tennis": "tennis", "cricket": "cricket",
}

// Known LiveScore incident type codes; anything else passes through raw.
// Incident types, established by checking real matches rather than assumption:
// which types advance the score, and whether the scorer belongs to the team
// the goal is credited to. The previous map had 37 as an own goal (it is a
// penalty), 39 as a missed penalty (it is the own goal), and 38 as a penalty
// goal (it never advances the score at all).
//
//	36 goal              37 penalty goal      39 own goal
//	41/47/57 goal        43 yellow  44 second yellow  45 red
//	38/40/62 carry a score but never change it — not goals, so ignored.
var incidentLabels = map[int]string{
	36: "goal", 37: "penalty_goal", 39: "own_goal",
	41: "goal", 47: "goal", 57: "goal",
	43: "yellow_card", 44: "yellow_red_card", 45: "red_card",
	63: "substitution",
}

func lsGet(c *gin.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(out)
}

type lsIncident struct {
	Min  int          `json:"Min"`
	Nm   int          `json:"Nm"`
	IT   int          `json:"IT"`
	Pn   string       `json:"Pn"`
	Sc   []int        `json:"Sc"`
	Incs []lsIncident `json:"Incs"`
}

func flattenIncidents(items []lsIncident, out *[]gin.H) {
	for _, item := range items {
		if len(item.Incs) > 0 {
			// Grouped incident = a goal with its assist (IT 63 in-group).
			var goal gin.H
			assist := ""
			for _, child := range item.Incs {
				side := child.Nm
				if side == 0 {
					side = item.Nm
				}
				switch child.IT {
				// Every type that actually puts the ball in the net.
				case 36, 37, 39, 41, 47, 57:
					goal = gin.H{"minute": child.Min, "type": incidentLabels[child.IT], "player": child.Pn, "team": side}
					if len(child.Sc) == 2 {
						goal["score"] = fmt.Sprintf("%d-%d", child.Sc[0], child.Sc[1])
					}
				case 63:
					assist = child.Pn
				}
			}
			if goal != nil {
				if assist != "" {
					goal["assist"] = assist
				}
				*out = append(*out, goal)
			} else {
				flattenIncidents(item.Incs, out)
			}
			continue
		}
		// Same rule as the board: never guess a goal from a score array alone.
		label, known := incidentLabels[item.IT]
		if !known || label == "" {
			continue
		}
		entry := gin.H{"minute": item.Min, "type": label, "player": item.Pn, "team": item.Nm}
		if len(item.Sc) == 2 {
			entry["score"] = fmt.Sprintf("%d-%d", item.Sc[0], item.Sc[1])
		}
		*out = append(*out, entry)
	}
}

// GetFixtureDetail — GET /v1/fixtures/:id/detail
func GetFixtureDetail(c *gin.Context) {
	fixtureID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var fixture models.Fixture
	if err := initializers.DB.WithContext(c.Request.Context()).First(&fixture, "id = ?", fixtureID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "FIXTURE_NOT_FOUND", "fixture was not found")
		return
	}

	cacheKey := fixture.ExternalID
	detailCache.Lock()
	if entry, hit := detailCache.entries[cacheKey]; hit && time.Now().Before(entry.expires) {
		detailCache.Unlock()
		utils.RespondSuccess(c, http.StatusOK, entry.payload, "")
		return
	}
	detailCache.Unlock()

	payload := gin.H{
		"sport": fixture.Sport, "league": fixture.League,
		"home": fixture.Home, "away": fixture.Away,
		"home_img": fixture.HomeImg, "away_img": fixture.AwayImg,
		"home_score": fixture.HomeScore, "away_score": fixture.AwayScore,
		"status": fixture.Status, "kickoff_at": fixture.KickoffAt,
	}

	if fixture.Sport == "f1" {
		parts := strings.Split(fixture.ExternalID, "-")
		if len(parts) == 3 {
			var results struct {
				MRData struct {
					RaceTable struct {
						Races []struct {
							Results []struct {
								Position string `json:"position"`
								Points   string `json:"points"`
								Status   string `json:"status"`
								Driver   struct {
									GivenName  string `json:"givenName"`
									FamilyName string `json:"familyName"`
								} `json:"Driver"`
								Constructor struct {
									Name string `json:"name"`
								} `json:"Constructor"`
							} `json:"Results"`
						} `json:"Races"`
					} `json:"RaceTable"`
				} `json:"MRData"`
			}
			url := fmt.Sprintf("https://api.jolpi.ca/ergast/f1/%s/%s/results.json?limit=10", parts[1], parts[2])
			if lsGet(c, url, &results) == nil && len(results.MRData.RaceTable.Races) > 0 {
				podium := []gin.H{}
				for _, row := range results.MRData.RaceTable.Races[0].Results {
					podium = append(podium, gin.H{
						"position": row.Position,
						"driver":   row.Driver.GivenName + " " + row.Driver.FamilyName,
						"team":     row.Constructor.Name,
						"points":   row.Points,
						"status":   row.Status,
					})
				}
				payload["results"] = podium
			}
		}
	} else if path, supported := sportPath[fixture.Sport]; supported {
		eid := strings.TrimPrefix(fixture.ExternalID, fixture.Sport+"-")

		var incidents struct {
			Trh1 string                  `json:"Trh1"`
			Trh2 string                  `json:"Trh2"`
			Incs map[string][]lsIncident `json:"Incs"`
		}
		if lsGet(c, fmt.Sprintf("https://prod-public-api.livescore.com/v1/api/app/incidents/%s/%s?locale=en", path, eid), &incidents) == nil {
			timeline := []gin.H{}
			// LiveScore groups incidents by period; within them, the nested
			// entries carry team side via ordering — the flat list keeps
			// minute order which is what a timeline needs.
			for _, period := range incidents.Incs {
				flattenIncidents(period, &timeline)
			}
			// sort by minute
			for i := 1; i < len(timeline); i++ {
				for j := i; j > 0 && timeline[j-1]["minute"].(int) > timeline[j]["minute"].(int); j-- {
					timeline[j-1], timeline[j] = timeline[j], timeline[j-1]
				}
			}
			payload["timeline"] = timeline
			if incidents.Trh1 != "" || incidents.Trh2 != "" {
				payload["half_time"] = incidents.Trh1 + "-" + incidents.Trh2
			}
		}

		if fixture.Sport == "football" {
			var lineups struct {
				Lu []struct {
					Tnb int `json:"Tnb"`
					Ps  []struct {
						Fn  string  `json:"Fn"`
						Ln  string  `json:"Ln"`
						Snm string  `json:"Snm"`
						Snu int     `json:"Snu"`
						Pon string  `json:"Pon"`
						Fp  *string `json:"Fp"`
					} `json:"Ps"`
				} `json:"Lu"`
			}
			if lsGet(c, fmt.Sprintf("https://prod-public-api.livescore.com/v1/api/app/lineups/%s/%s?locale=en", path, eid), &lineups) == nil && len(lineups.Lu) > 0 {
				teams := []gin.H{}
				for _, side := range lineups.Lu {
					starters := []gin.H{}
					bench := []gin.H{}
					for _, player := range side.Ps {
						name := strings.TrimSpace(player.Fn + " " + player.Ln)
						if name == "" {
							name = player.Snm
						}
						entry := gin.H{"name": name, "number": player.Snu, "position": player.Pon}
						// A formation slot (Fp) marks a starter; the rest are
						// the bench, in shirt order as delivered.
						if player.Fp != nil && *player.Fp != "" {
							starters = append(starters, entry)
						} else {
							bench = append(bench, entry)
						}
					}
					teams = append(teams, gin.H{"team": side.Tnb, "players": starters, "subs": bench})
				}
				payload["lineups"] = teams
			}

			// Match stats: possession, shots, corners… once the game is on.
			var stats struct {
				Stat []struct {
					Tnb  int     `json:"Tnb"`
					Pss  int     `json:"Pss"`
					Shon int     `json:"Shon"`
					Shof int     `json:"Shof"`
					Cos  int     `json:"Cos"`
					Fls  int     `json:"Fls"`
					Ofs  int     `json:"Ofs"`
					Ycs  int     `json:"Ycs"`
					Rcs  int     `json:"Rcs"`
					Xg   float64 `json:"Xg"`
				} `json:"Stat"`
			}
			if lsGet(c, fmt.Sprintf("https://prod-public-api.livescore.com/v1/api/app/statistics/%s/%s?locale=en", path, eid), &stats) == nil && len(stats.Stat) == 2 {
				home, away := stats.Stat[0], stats.Stat[1]
				if home.Tnb == 2 {
					home, away = away, home
				}
				payload["stats"] = []gin.H{
					{"key": "possession", "home": home.Pss, "away": away.Pss, "pct": true},
					{"key": "shots_on", "home": home.Shon, "away": away.Shon},
					{"key": "shots_off", "home": home.Shof, "away": away.Shof},
					{"key": "corners", "home": home.Cos, "away": away.Cos},
					{"key": "fouls", "home": home.Fls, "away": away.Fls},
					{"key": "offsides", "home": home.Ofs, "away": away.Ofs},
					{"key": "yellow_cards", "home": home.Ycs, "away": away.Ycs},
					{"key": "xg", "home": home.Xg, "away": away.Xg},
				}
			}
		}
	}

	detailCache.Lock()
	detailCache.entries[cacheKey] = detailEntry{payload: payload, expires: time.Now().Add(60 * time.Second)}
	// keep the cache from growing unbounded
	if len(detailCache.entries) > 500 {
		for key, entry := range detailCache.entries {
			if time.Now().After(entry.expires) {
				delete(detailCache.entries, key)
			}
		}
	}
	detailCache.Unlock()

	c.Header("Cache-Control", "public, max-age=30")
	utils.RespondSuccess(c, http.StatusOK, payload, "")
}
