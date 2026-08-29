package handlers

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/services"
	"github.com/godopetza/pitchtz/utils"
	"gorm.io/datatypes"
)

// Watch spots: bars, lounges and halls where people watch the big game.
// Anyone can apply to list their place; superadmin approves before it shows
// on the client site.

func watchSpotJSON(spot models.WatchSpot) gin.H {
	photoURL := ""
	if base := strings.TrimRight(os.Getenv("ASSET_BASE_URL"), "/"); base != "" && spot.PhotoR2Key != "" {
		photoURL = base + "/" + strings.TrimLeft(spot.PhotoR2Key, "/")
	}
	return gin.H{
		"id": spot.ID, "name": spot.Name, "area": spot.Area, "address": spot.Address,
		"latitude": spot.Latitude, "longitude": spot.Longitude,
		"screens": spot.Screens, "capacity": spot.Capacity,
		"entry_tzs": spot.EntryTZS, "features": validJSON(spot.Features, "[]"),
		"photo_url": photoURL, "status": spot.Status, "created_at": spot.CreatedAt,
	}
}

// ListWatchSpots is public: approved spots only.
func ListWatchSpots(c *gin.Context) {
	var spots []models.WatchSpot
	initializers.DB.WithContext(c.Request.Context()).
		Where("status = ?", "active").Order("created_at ASC").Find(&spots)
	items := make([]gin.H, 0, len(spots))
	for _, spot := range spots {
		items = append(items, watchSpotJSON(spot))
	}
	utils.RespondSuccess(c, http.StatusOK, items, "")
}

type watchSpotApplyInput struct {
	Name         string   `json:"name" binding:"required,min=2,max=120"`
	Area         string   `json:"area" binding:"required,min=2,max=120"`
	Address      string   `json:"address" binding:"max=200"`
	Latitude     float64  `json:"latitude"`
	Longitude    float64  `json:"longitude"`
	Screens      int      `json:"screens" binding:"gte=0,lte=50"`
	Capacity     string   `json:"capacity" binding:"max=40"`
	EntryTZS     int64    `json:"entry_tzs" binding:"gte=0"`
	Features     []string `json:"features" binding:"max=8,dive,max=40"`
	PhotoR2Key   string   `json:"photo_r2_key" binding:"max=200"`
	ContactName  string   `json:"contact_name" binding:"required,min=2,max=120"`
	ContactPhone string   `json:"contact_phone" binding:"required,min=9,max=20"`
}

// ApplyWatchSpot takes a public listing application → pending + admin email.
func ApplyWatchSpot(c *gin.Context) {
	var input watchSpotApplyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "name, area, contact_name and contact_phone are required")
		return
	}
	features, _ := jsonMarshalStrings(input.Features)
	spot := models.WatchSpot{
		Name: strings.TrimSpace(input.Name), Area: strings.TrimSpace(input.Area),
		Address: strings.TrimSpace(input.Address), Latitude: input.Latitude, Longitude: input.Longitude,
		Screens: input.Screens, Capacity: strings.TrimSpace(input.Capacity), EntryTZS: input.EntryTZS,
		Features: features, PhotoR2Key: strings.TrimSpace(input.PhotoR2Key),
		ContactName: strings.TrimSpace(input.ContactName), ContactPhone: strings.TrimSpace(input.ContactPhone),
		Status: "pending",
	}
	if err := initializers.DB.WithContext(c.Request.Context()).Create(&spot).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "APPLICATION_FAILED", "could not submit the application")
		return
	}
	go services.SendWatchSpotApplicationEmail(spot.Name, spot.Area, spot.ContactName, spot.ContactPhone, spot.ID)
	utils.RespondSuccess(c, http.StatusAccepted, gin.H{"id": spot.ID, "status": spot.Status},
		"Application received — we review every spot before it goes live.")
}

func jsonMarshalStrings(values []string) (datatypes.JSON, error) {
	clean := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	if len(clean) == 0 {
		return datatypes.JSON([]byte("[]")), nil
	}
	parts := make([]string, 0, len(clean))
	for _, value := range clean {
		parts = append(parts, fmt.Sprintf("%q", value))
	}
	return datatypes.JSON([]byte("[" + strings.Join(parts, ",") + "]")), nil
}

// AdminListWatchSpots — all spots, filterable by status.
func AdminListWatchSpots(c *gin.Context) {
	query := initializers.DB.WithContext(c.Request.Context()).Model(&models.WatchSpot{})
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	var spots []models.WatchSpot
	query.Order("created_at DESC").Limit(100).Find(&spots)
	items := make([]gin.H, 0, len(spots))
	for _, spot := range spots {
		payload := watchSpotJSON(spot)
		payload["contact_name"] = spot.ContactName
		payload["contact_phone"] = spot.ContactPhone
		items = append(items, payload)
	}
	utils.RespondSuccess(c, http.StatusOK, items, "")
}

// AdminSetWatchSpotStatus approves/rejects with an audit row.
func AdminSetWatchSpotStatus(c *gin.Context) {
	spotID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input struct {
		Status string `json:"status" binding:"required,oneof=active rejected pending"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "status must be active, rejected or pending")
		return
	}
	var spot models.WatchSpot
	if err := initializers.DB.WithContext(c.Request.Context()).First(&spot, "id = ?", spotID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "SPOT_NOT_FOUND", "watch spot was not found")
		return
	}
	initializers.DB.WithContext(c.Request.Context()).Model(&models.WatchSpot{}).
		Where("id = ?", spotID).Update("status", input.Status)
	writeAudit(c, "watchspot.status", "watch_spot", spotID.String(),
		fmt.Sprintf("%s: %s -> %s", spot.Name, spot.Status, input.Status))
	utils.RespondSuccess(c, http.StatusOK, gin.H{"id": spotID, "status": input.Status}, "Watch spot updated.")
}
