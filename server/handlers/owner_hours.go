package handlers

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/utils"
	"gorm.io/datatypes"
)

// Venue opening hours: the owner's weekly schedule. Days absent from the map
// are closed; an entirely empty map means the platform default (08:00–23:00
// every day) so venues work before the owner ever touches scheduling.

var dayKeys = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
var clockPattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

type dayHours struct {
	Open  string `json:"open"`
	Close string `json:"close"`
}

func OwnerSetVenueHours(c *gin.Context) {
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	venueID, ok := parseID(c, "id")
	if !ok {
		return
	}
	if !ownerOwnsVenue(c, ownerID, venueID) {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_VENUE", "this venue is not on your account")
		return
	}
	var input map[string]dayHours
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "send a map of day -> {open, close}")
		return
	}
	valid := map[string]bool{}
	for _, key := range dayKeys {
		valid[key] = true
	}
	for day, hours := range input {
		if !valid[day] {
			utils.RespondError(c, http.StatusBadRequest, "INVALID_DAY", "days must be mon..sun")
			return
		}
		if !clockPattern.MatchString(hours.Open) || !clockPattern.MatchString(hours.Close) || hours.Open >= hours.Close {
			utils.RespondError(c, http.StatusBadRequest, "INVALID_HOURS", "each day needs open < close in HH:MM")
			return
		}
	}
	payload, _ := json.Marshal(input)
	initializers.DB.WithContext(c.Request.Context()).Model(&models.Venue{}).
		Where("id = ?", venueID).Update("open_hours", datatypes.JSON(payload))
	utils.RespondSuccess(c, http.StatusOK, gin.H{"updated": true}, "Opening hours saved.")
}

// venueOpenAt reports whether the venue accepts play in [start, end) — used
// as the server-side guard so bookings can't slip outside opening hours.
func venueOpenAt(venue models.Venue, start, end time.Time) bool {
	var hours map[string]dayHours
	if err := json.Unmarshal(venue.OpenHours, &hours); err != nil || len(hours) == 0 {
		return true
	}
	zone, err := time.LoadLocation("Africa/Dar_es_Salaam")
	if err != nil {
		zone = time.FixedZone("EAT", 3*3600)
	}
	localStart := start.In(zone)
	localEnd := end.In(zone)
	day := dayKeys[(int(localStart.Weekday())+6)%7]
	window, open := hours[day]
	if !open {
		return false
	}
	startClock := localStart.Format("15:04")
	endClock := localEnd.Format("15:04")
	if endClock == "00:00" {
		endClock = "24:00"
	}
	return startClock >= window.Open && endClock <= window.Close && localStart.Day() == localEnd.In(zone).Day()
}
