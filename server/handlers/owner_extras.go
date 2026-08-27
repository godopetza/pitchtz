package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/utils"
)

// Extras are venue facilities and rentals — bibs, balls, a football watch
// area, drinks. PriceTZS 0 means the facility is free; Available switches a
// facility on or off without deleting its setup.
type ownerExtraInput struct {
	Kind      string `json:"kind" binding:"required,min=2,max=40"`
	Name      string `json:"name" binding:"required,min=1,max=120"`
	PriceTZS  int64  `json:"price_tzs" binding:"gte=0"`
	Unit      string `json:"unit" binding:"max=40"`
	Stock     int    `json:"stock" binding:"gte=0"`
	Available *bool  `json:"available"`
}

// OwnerListExtras returns every extra on the venue, including switched-off
// ones — the public endpoint filters those, the owner must see them.
func OwnerListExtras(c *gin.Context) {
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
	var extras []models.ExtraCatalog
	initializers.DB.WithContext(c.Request.Context()).
		Where("venue_id = ?", venueID).Order("created_at ASC").Find(&extras)
	rows := make([]ExtraPublicDTO, 0, len(extras))
	for _, extra := range extras {
		rows = append(rows, extraPublicDTO(extra))
	}
	utils.RespondSuccess(c, http.StatusOK, rows, "")
}

func OwnerCreateExtra(c *gin.Context) {
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
	var input ownerExtraInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "kind and name are required; price and stock must not be negative")
		return
	}
	unit := strings.TrimSpace(input.Unit)
	if unit == "" {
		unit = "per session"
	}
	available := true
	if input.Available != nil {
		available = *input.Available
	}
	extra := models.ExtraCatalog{
		VenueID: venueID, Kind: strings.TrimSpace(input.Kind), Name: strings.TrimSpace(input.Name),
		PriceTZS: input.PriceTZS, Unit: unit, Stock: input.Stock, Available: available,
	}
	if err := initializers.DB.WithContext(c.Request.Context()).Create(&extra).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "EXTRA_CREATE_FAILED", "could not create the extra")
		return
	}
	utils.RespondSuccess(c, http.StatusCreated, extraPublicDTO(extra), "Extra added.")
}

func OwnerUpdateExtra(c *gin.Context) {
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	extraID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var extra models.ExtraCatalog
	if err := initializers.DB.WithContext(c.Request.Context()).First(&extra, "id = ?", extraID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "EXTRA_NOT_FOUND", "extra was not found")
		return
	}
	if !ownerOwnsVenue(c, ownerID, extra.VenueID) {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_VENUE", "this extra is not on your account")
		return
	}
	var input ownerExtraInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "kind and name are required; price and stock must not be negative")
		return
	}
	updates := map[string]interface{}{
		"kind": strings.TrimSpace(input.Kind), "name": strings.TrimSpace(input.Name),
		"price_tzs": input.PriceTZS, "stock": input.Stock,
	}
	if unit := strings.TrimSpace(input.Unit); unit != "" {
		updates["unit"] = unit
	}
	if input.Available != nil {
		updates["available"] = *input.Available
	}
	initializers.DB.WithContext(c.Request.Context()).Model(&models.ExtraCatalog{}).
		Where("id = ?", extraID).Updates(updates)
	utils.RespondSuccess(c, http.StatusOK, gin.H{"updated": true}, "Extra updated.")
}

func OwnerDeleteExtra(c *gin.Context) {
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	extraID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var extra models.ExtraCatalog
	if err := initializers.DB.WithContext(c.Request.Context()).First(&extra, "id = ?", extraID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "EXTRA_NOT_FOUND", "extra was not found")
		return
	}
	if !ownerOwnsVenue(c, ownerID, extra.VenueID) {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_VENUE", "this extra is not on your account")
		return
	}
	initializers.DB.WithContext(c.Request.Context()).Delete(&models.ExtraCatalog{}, "id = ?", extraID)
	utils.RespondSuccess(c, http.StatusOK, gin.H{"deleted": true}, "Extra removed.")
}
