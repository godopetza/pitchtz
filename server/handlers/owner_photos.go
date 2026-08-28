package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/utils"
)

// Owner gallery management. Photos are addressed by their R2 key within the
// venue — the key is what the owner app already has from the public DTO.

type ownerPhotoAddInput struct {
	R2Key string `json:"r2_key" binding:"required,max=200"`
	Alt   string `json:"alt" binding:"max=120"`
}

func OwnerAddVenuePhoto(c *gin.Context) {
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
	var input ownerPhotoAddInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "r2_key is required")
		return
	}
	var maxSort int
	initializers.DB.WithContext(c.Request.Context()).Model(&models.VenuePhoto{}).
		Where("venue_id = ?", venueID).Select("COALESCE(MAX(sort), -1)").Scan(&maxSort)
	photo := models.VenuePhoto{VenueID: venueID, R2Key: strings.TrimSpace(input.R2Key), Sort: maxSort + 1, Alt: strings.TrimSpace(input.Alt)}
	if err := initializers.DB.WithContext(c.Request.Context()).Create(&photo).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "PHOTO_ADD_FAILED", "could not add the photo")
		return
	}
	utils.RespondSuccess(c, http.StatusCreated, gin.H{"r2_key": photo.R2Key, "sort": photo.Sort}, "Photo added.")
}

type ownerPhotoOrderInput struct {
	Keys []string `json:"keys" binding:"required,min=1,max=30"`
}

// OwnerReorderVenuePhotos sets the gallery order; the first key becomes the
// cover players see on cards.
func OwnerReorderVenuePhotos(c *gin.Context) {
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
	var input ownerPhotoOrderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "keys is required")
		return
	}
	for index, key := range input.Keys {
		initializers.DB.WithContext(c.Request.Context()).Model(&models.VenuePhoto{}).
			Where("venue_id = ? AND r2_key = ?", venueID, strings.TrimSpace(key)).Update("sort", index)
	}
	utils.RespondSuccess(c, http.StatusOK, gin.H{"updated": true}, "Gallery order saved.")
}

type ownerPhotoAltInput struct {
	R2Key string `json:"r2_key" binding:"required,max=200"`
	Alt   string `json:"alt" binding:"max=120"`
}

func OwnerSetVenuePhotoAlt(c *gin.Context) {
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
	var input ownerPhotoAltInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "r2_key is required")
		return
	}
	initializers.DB.WithContext(c.Request.Context()).Model(&models.VenuePhoto{}).
		Where("venue_id = ? AND r2_key = ?", venueID, strings.TrimSpace(input.R2Key)).
		Update("alt", strings.TrimSpace(input.Alt))
	utils.RespondSuccess(c, http.StatusOK, gin.H{"updated": true}, "Photo title saved.")
}

func OwnerDeleteVenuePhoto(c *gin.Context) {
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
	key := strings.TrimSpace(c.Query("key"))
	if key == "" {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "key query parameter is required")
		return
	}
	var count int64
	initializers.DB.WithContext(c.Request.Context()).Model(&models.VenuePhoto{}).
		Where("venue_id = ?", venueID).Count(&count)
	if count <= 1 {
		utils.RespondError(c, http.StatusConflict, "LAST_PHOTO", "a venue needs at least one photo — add another before removing this one")
		return
	}
	initializers.DB.WithContext(c.Request.Context()).
		Where("venue_id = ? AND r2_key = ?", venueID, key).Delete(&models.VenuePhoto{})
	utils.RespondSuccess(c, http.StatusOK, gin.H{"deleted": true}, "Photo removed.")
}
