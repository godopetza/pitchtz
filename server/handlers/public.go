package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/services"
	"github.com/godopetza/pitchtz/store"
	"github.com/godopetza/pitchtz/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PublicAPI struct {
	Catalog  store.CatalogStore
	Waitlist store.WaitlistStore
}

func Health(c *gin.Context) {
	database := "disabled"
	if initializers.DB != nil {
		sqlDB, err := initializers.DB.DB()
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "service": "pitchtz", "database": "unavailable"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := sqlDB.PingContext(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "service": "pitchtz", "database": "unavailable"})
			return
		}
		database = "connected"
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "pitchtz", "database": database})
}

func (h *PublicAPI) ListCities(c *gin.Context) {
	cities, err := h.Catalog.ListCities(c.Request.Context())
	if err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "CITY_LIST_FAILED", "could not load cities")
		return
	}
	dtos := make([]CityPublicDTO, 0, len(cities))
	for _, city := range cities {
		dtos = append(dtos, cityPublicDTO(city))
	}
	c.Header("Cache-Control", "public, max-age=300")
	utils.RespondSuccess(c, http.StatusOK, dtos, "")
}

func (h *PublicAPI) ListVenues(c *gin.Context) {
	filter, err := parseVenueFilter(c)
	if err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_FILTER", err.Error())
		return
	}
	venues, err := h.Catalog.ListVenues(c.Request.Context(), filter)
	if err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "VENUE_LIST_FAILED", "could not load venues")
		return
	}
	dtos := make([]VenuePublicDTO, 0, len(venues))
	for _, venue := range venues {
		dtos = append(dtos, venuePublicDTO(venue))
	}
	c.Header("Cache-Control", "public, max-age=60")
	utils.RespondSuccess(c, http.StatusOK, dtos, "")
}

func (h *PublicAPI) GetVenue(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	venue, err := h.Catalog.GetVenue(c.Request.Context(), id)
	if err != nil {
		respondStoreError(c, err, "venue")
		return
	}
	c.Header("Cache-Control", "public, max-age=60")
	utils.RespondSuccess(c, http.StatusOK, venuePublicDTO(venue), "")
}

func (h *PublicAPI) ListVenueReviews(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	reviews, err := h.Catalog.ListVenueReviews(c.Request.Context(), id)
	if err != nil {
		respondStoreError(c, err, "venue")
		return
	}
	dtos := make([]ReviewPublicDTO, 0, len(reviews))
	for _, review := range reviews {
		dtos = append(dtos, reviewPublicDTO(review))
	}
	c.Header("Cache-Control", "public, max-age=60")
	utils.RespondSuccess(c, http.StatusOK, dtos, "")
}

func (h *PublicAPI) ListVenueExtras(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	extras, err := h.Catalog.ListVenueExtras(c.Request.Context(), id)
	if err != nil {
		respondStoreError(c, err, "venue")
		return
	}
	dtos := make([]ExtraPublicDTO, 0, len(extras))
	for _, extra := range extras {
		dtos = append(dtos, extraPublicDTO(extra))
	}
	c.Header("Cache-Control", "public, max-age=60")
	utils.RespondSuccess(c, http.StatusOK, dtos, "")
}

func (h *PublicAPI) GetVenueAvailability(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	from, to, err := parseAvailabilityWindow(c)
	if err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_WINDOW", err.Error())
		return
	}
	venue, err := h.Catalog.GetVenue(c.Request.Context(), id)
	if err != nil {
		respondStoreError(c, err, "venue")
		return
	}
	windows, err := h.Catalog.ListUnavailable(c.Request.Context(), id, from, to)
	if err != nil {
		respondStoreError(c, err, "venue")
		return
	}

	type publicWindow struct {
		StartsAt time.Time `json:"starts_at"`
		EndsAt   time.Time `json:"ends_at"`
		Kind     string    `json:"kind"`
	}
	type pitchAvailability struct {
		Pitch       PitchPublicDTO `json:"pitch"`
		Unavailable []publicWindow `json:"unavailable"`
	}
	byPitch := make(map[uuid.UUID][]publicWindow)
	for _, window := range windows {
		byPitch[window.PitchID] = append(byPitch[window.PitchID], publicWindow{
			StartsAt: window.StartsAt, EndsAt: window.EndsAt, Kind: window.Kind,
		})
	}
	result := make([]pitchAvailability, 0, len(venue.Pitches))
	for _, pitch := range venue.Pitches {
		unavailable := byPitch[pitch.ID]
		if unavailable == nil {
			unavailable = []publicWindow{}
		}
		result = append(result, pitchAvailability{
			Pitch: PitchPublicDTO{ID: pitch.ID, Name: pitch.Name, Format: pitch.Format, Surface: pitch.Surface,
				BasePriceTZS: pitch.BasePriceTZS, OpenHours: validJSON(pitch.OpenHours, "{}")},
			Unavailable: unavailable,
		})
	}
	c.Header("Cache-Control", "public, max-age=15")
	utils.RespondSuccess(c, http.StatusOK, gin.H{"from": from, "to": to, "pitches": result}, "")
}

func (h *PublicAPI) JoinWaitlist(c *gin.Context) {
	var request struct {
		CityID string `json:"city_id" binding:"required"`
		Email  string `json:"email"`
		Phone  string `json:"phone"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_BODY", "city_id and a valid email or phone are required")
		return
	}
	cityID, err := uuid.Parse(request.CityID)
	if err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_CITY_ID", "city_id must be a UUID")
		return
	}

	var email, phone *string
	if strings.TrimSpace(request.Email) != "" {
		normalized, err := utils.NormalizeEmail(request.Email)
		if err != nil {
			utils.RespondError(c, http.StatusBadRequest, "INVALID_EMAIL", "email is invalid")
			return
		}
		email = &normalized
	}
	if strings.TrimSpace(request.Phone) != "" {
		normalized, err := utils.NormalizeTZPhone(request.Phone)
		if err != nil {
			utils.RespondError(c, http.StatusBadRequest, "INVALID_PHONE", "phone must be a valid Tanzanian mobile number")
			return
		}
		phone = &normalized
	}
	if email == nil && phone == nil {
		utils.RespondError(c, http.StatusBadRequest, "CONTACT_REQUIRED", "email or phone is required")
		return
	}

	entry, city, err := h.Waitlist.JoinWaitlist(c.Request.Context(), store.WaitlistInput{CityID: cityID, Email: email, Phone: phone})
	if err != nil {
		if errors.Is(err, store.ErrInvalidCity) {
			utils.RespondError(c, http.StatusBadRequest, "INVALID_CITY", "city does not exist")
			return
		}
		utils.RespondError(c, http.StatusInternalServerError, "WAITLIST_FAILED", "could not join the waitlist")
		return
	}
	c.Header("Cache-Control", "no-store")
	utils.RespondSuccess(c, http.StatusAccepted, gin.H{
		"id": entry.ID, "status": "accepted", "city": cityPublicDTO(city),
	}, "we will notify you when PitchTZ launches in this city")
}

func parseID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_ID", name+" must be a UUID")
		return uuid.Nil, false
	}
	return id, true
}

func parseVenueFilter(c *gin.Context) (store.VenueFilter, error) {
	var filter store.VenueFilter
	if value := strings.TrimSpace(c.Query("city_id")); value != "" {
		id, err := uuid.Parse(value)
		if err != nil {
			return filter, errors.New("city_id must be a UUID")
		}
		filter.CityID = &id
	}
	if value := strings.ToLower(strings.TrimSpace(c.Query("format"))); value != "" {
		switch value {
		case "5", "7", "11", "futsal":
			filter.Format = value
		default:
			return filter, errors.New("format must be 5, 7, 11, or futsal")
		}
	}

	latValue, lngValue := strings.TrimSpace(c.Query("lat")), strings.TrimSpace(c.Query("lng"))
	if near := strings.TrimSpace(c.Query("near")); near != "" {
		parts := strings.Split(near, ",")
		if len(parts) != 2 {
			return filter, errors.New("near must be formatted as latitude,longitude")
		}
		latValue, lngValue = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	if (latValue == "") != (lngValue == "") {
		return filter, errors.New("latitude and longitude must be provided together")
	}
	if latValue == "" && (c.Query("radius") != "" || c.Query("radius_km") != "") {
		return filter, errors.New("radius requires latitude and longitude")
	}
	if latValue != "" {
		lat, err := strconv.ParseFloat(latValue, 64)
		if err != nil || lat < -90 || lat > 90 {
			return filter, errors.New("latitude is invalid")
		}
		lng, err := strconv.ParseFloat(lngValue, 64)
		if err != nil || lng < -180 || lng > 180 {
			return filter, errors.New("longitude is invalid")
		}
		radiusValue := c.Query("radius_km")
		if radiusValue == "" {
			radiusValue = c.Query("radius")
		}
		radius := 10.0
		if radiusValue != "" {
			radius, err = strconv.ParseFloat(radiusValue, 64)
			if err != nil || radius <= 0 || radius > 100 {
				return filter, errors.New("radius must be between 0 and 100 kilometres")
			}
		}
		filter.Latitude, filter.Longitude, filter.RadiusKM = &lat, &lng, radius
	}
	return filter, nil
}

func parseAvailabilityWindow(c *gin.Context) (time.Time, time.Time, error) {
	location, err := time.LoadLocation("Africa/Dar_es_Salaam")
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("server timezone is unavailable")
	}
	if value := strings.TrimSpace(c.Query("date")); value != "" {
		from, err := time.ParseInLocation("2006-01-02", value, location)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("date must use YYYY-MM-DD")
		}
		return from, from.AddDate(0, 0, 1), nil
	}
	fromValue, toValue := strings.TrimSpace(c.Query("from")), strings.TrimSpace(c.Query("to"))
	if fromValue == "" && toValue == "" {
		now := time.Now().In(location)
		from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
		return from, from.AddDate(0, 0, 1), nil
	}
	if fromValue == "" || toValue == "" {
		return time.Time{}, time.Time{}, errors.New("from and to must be provided together")
	}
	from, err := time.Parse(time.RFC3339, fromValue)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("from must be RFC3339")
	}
	to, err := time.Parse(time.RFC3339, toValue)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("to must be RFC3339")
	}
	if !to.After(from) || to.Sub(from) > 31*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New("availability window must be positive and no longer than 31 days")
	}
	return from, to, nil
}

func respondStoreError(c *gin.Context, err error, resource string) {
	if errors.Is(err, store.ErrNotFound) {
		utils.RespondError(c, http.StatusNotFound, "NOT_FOUND", resource+" not found")
		return
	}
	utils.RespondError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not load "+resource)
}

// ListFixtures serves the scraped match board: up to seven days ahead plus
// today's finished games. Filters: ?sport=football|basketball, ?date=YYYY-MM-DD
// (a single EAT day), ?league=. With a bearer token, each row carries
// is_favorite from the user's saved teams so clients can pin them.
func ListFixtures(c *gin.Context) {
	// ?team= answers "how is my club doing?" — the five most recent results
	// and the next five to come, ignoring the day window entirely.
	if team := strings.TrimSpace(c.Query("team")); team != "" {
		listTeamFixtures(c, team)
		return
	}
	query := initializers.DB.WithContext(c.Request.Context()).Model(&models.Fixture{})
	if date := strings.TrimSpace(c.Query("date")); date != "" {
		if day, err := time.Parse("2006-01-02", date); err == nil {
			eat := time.FixedZone("EAT", 3*3600)
			start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, eat)
			query = query.Where("kickoff_at >= ? AND kickoff_at < ?", start.UTC(), start.AddDate(0, 0, 1).UTC())
		}
	} else {
		// F1 races and boxing cards are weeks apart — give them a wider
		// window so the next one is always on the board.
		now := time.Now().UTC()
		query = query.Where(
			"(kickoff_at >= ? AND kickoff_at <= ?) OR (sport IN ('f1','boxing') AND kickoff_at >= ? AND kickoff_at <= ?)",
			now.Add(-3*24*time.Hour), now.Add(7*24*time.Hour),
			now.Add(-3*24*time.Hour), now.Add(30*24*time.Hour))
	}
	if sport := strings.TrimSpace(c.Query("sport")); sport != "" {
		query = query.Where("sport = ?", sport)
	}
	if league := strings.TrimSpace(c.Query("league")); league != "" {
		query = query.Where("league = ?", league)
	}
	var fixtures []models.Fixture
	query.Order("kickoff_at ASC").Limit(600).Find(&fixtures)

	favorites := map[string]bool{}
	if viewer := viewerID(c); viewer != uuid.Nil {
		var rows []models.FavoriteTeam
		initializers.DB.WithContext(c.Request.Context()).Where("user_id = ?", viewer).Find(&rows)
		for _, row := range rows {
			favorites[strings.ToLower(row.TeamName)] = true
		}
	}

	items := make([]gin.H, 0, len(fixtures))
	for _, fixture := range fixtures {
		items = append(items, fixtureItem(fixture, favorites))
	}
	// Cheap for everyone: anonymous responses are cacheable for 30 seconds.
	if len(favorites) == 0 {
		c.Header("Cache-Control", "public, max-age=30")
	}
	utils.RespondSuccess(c, http.StatusOK, items, "")
}

// fixtureItem is the single wire shape for a fixture, shared by the board and
// the per-team view so they can never drift apart.
func fixtureItem(fixture models.Fixture, favorites map[string]bool) gin.H {
	return gin.H{
		"id": fixture.ID, "sport": fixture.Sport, "league": fixture.League, "country": fixture.Country,
		"home": fixture.Home, "away": fixture.Away,
		"home_img": fixture.HomeImg, "away_img": fixture.AwayImg, "league_img": fixture.LeagueImg,
		"home_score": fixture.HomeScore, "away_score": fixture.AwayScore,
		"kickoff_at": fixture.KickoffAt, "status": fixture.Status,
		"timeline":    json.RawMessage(validJSON(fixture.Timeline, "[]")),
		"live":        services.FixtureIsLive(fixture.KickoffAt, fixture.Status),
		"is_favorite": favorites[strings.ToLower(fixture.Home)] || favorites[strings.ToLower(fixture.Away)],
	}
}

// listTeamFixtures serves one club's recent form and upcoming run: five
// finished matches behind, five ahead, oldest first so the row order reads
// like a timeline.
func listTeamFixtures(c *gin.Context, team string) {
	db := initializers.DB.WithContext(c.Request.Context())
	now := time.Now().UTC()
	match := db.Where("LOWER(home) = LOWER(?) OR LOWER(away) = LOWER(?)", team, team)

	var past []models.Fixture
	match.Session(&gorm.Session{}).
		Where("kickoff_at < ?", now).
		Order("kickoff_at DESC").Limit(5).Find(&past)
	for i, j := 0, len(past)-1; i < j; i, j = i+1, j-1 {
		past[i], past[j] = past[j], past[i]
	}

	var upcoming []models.Fixture
	match.Session(&gorm.Session{}).
		Where("kickoff_at >= ?", now).
		Order("kickoff_at ASC").Limit(5).Find(&upcoming)

	fixtures := append(past, upcoming...)

	favorites := map[string]bool{}
	if viewer := viewerID(c); viewer != uuid.Nil {
		var rows []models.FavoriteTeam
		db.Where("user_id = ?", viewer).Find(&rows)
		for _, row := range rows {
			favorites[strings.ToLower(row.TeamName)] = true
		}
	}

	items := make([]gin.H, 0, len(fixtures))
	for _, fixture := range fixtures {
		items = append(items, fixtureItem(fixture, favorites))
	}
	if len(favorites) == 0 {
		c.Header("Cache-Control", "public, max-age=60")
	}
	utils.RespondSuccess(c, http.StatusOK, items, "")
}

// ListFavoriteTeams returns the signed-in user's pinned clubs.
func ListFavoriteTeams(c *gin.Context) {
	userID, ok := clientUserID(c)
	if !ok {
		return
	}
	var rows []models.FavoriteTeam
	initializers.DB.WithContext(c.Request.Context()).
		Where("user_id = ?", userID).Order("created_at ASC").Find(&rows)
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.TeamName)
	}
	utils.RespondSuccess(c, http.StatusOK, names, "")
}

// SetFavoriteTeams replaces the user's pinned clubs (max 20 names).
func SetFavoriteTeams(c *gin.Context) {
	userID, ok := clientUserID(c)
	if !ok {
		return
	}
	var input struct {
		Teams []string `json:"teams" binding:"max=20,dive,min=1,max=120"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "teams must be up to 20 names")
		return
	}
	initializers.DB.WithContext(c.Request.Context()).Where("user_id = ?", userID).Delete(&models.FavoriteTeam{})
	seen := map[string]bool{}
	for _, name := range input.Teams {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || seen[strings.ToLower(trimmed)] {
			continue
		}
		seen[strings.ToLower(trimmed)] = true
		initializers.DB.WithContext(c.Request.Context()).Create(&models.FavoriteTeam{UserID: userID, TeamName: trimmed})
	}
	utils.RespondSuccess(c, http.StatusOK, gin.H{"count": len(seen)}, "Favorite teams saved.")
}
