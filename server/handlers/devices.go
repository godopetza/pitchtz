package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RegisterDevice — POST /v1/me/devices
// The mobile app calls this after sign-in and on every FCM token refresh.
// Upsert on the token: the same install re-registering just refreshes its
// row, and a device that signs in as somebody else is reassigned instead of
// leaving the previous user subscribed to it.
func RegisterDevice(c *gin.Context) {
	userID, ok := clientUserID(c)
	if !ok {
		return
	}
	var input struct {
		Token      string `json:"token" binding:"required,min=32,max=512"`
		Platform   string `json:"platform" binding:"omitempty,oneof=android ios"`
		AppVersion string `json:"app_version" binding:"omitempty,max=40"`
		Language   string `json:"language" binding:"omitempty,oneof=en sw"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "a valid FCM token is required")
		return
	}

	platform := strings.TrimSpace(input.Platform)
	if platform == "" {
		platform = "android"
	}
	language := strings.TrimSpace(input.Language)
	if language == "" {
		// Fall back to the account's own language rather than assuming.
		var user models.User
		if initializers.DB.WithContext(c.Request.Context()).First(&user, "id = ?", userID).Error == nil {
			language = user.Language
		}
		if language == "" {
			language = "sw"
		}
	}

	device := models.DeviceToken{
		UserID:     userID,
		Token:      strings.TrimSpace(input.Token),
		Platform:   platform,
		AppVersion: strings.TrimSpace(input.AppVersion),
		Language:   language,
		LastSeenAt: time.Now().UTC(),
	}
	err := initializers.DB.WithContext(c.Request.Context()).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "token"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"user_id", "platform", "app_version", "language", "last_seen_at", "updated_at",
		}),
	}).Create(&device).Error
	if err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "DEVICE_SAVE_FAILED", "could not save this device")
		return
	}
	utils.RespondSuccess(c, http.StatusOK, gin.H{"registered": true}, "Device registered for notifications.")
}

// UnregisterDevice — DELETE /v1/me/devices
// Called on sign-out so a shared phone stops receiving the previous player's
// bookings. Deleting a token that is already gone is a success, not a 404.
func UnregisterDevice(c *gin.Context) {
	userID, ok := clientUserID(c)
	if !ok {
		return
	}
	var input struct {
		Token string `json:"token" binding:"required,min=32,max=512"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "token is required")
		return
	}
	initializers.DB.WithContext(c.Request.Context()).
		Where("token = ? AND user_id = ?", strings.TrimSpace(input.Token), userID).
		Delete(&models.DeviceToken{})
	utils.RespondSuccess(c, http.StatusOK, gin.H{"registered": false}, "Device removed.")
}

// devicesForUser returns the push targets for one account, newest first.
func devicesForUser(db *gorm.DB, userID interface{}) []models.DeviceToken {
	var devices []models.DeviceToken
	db.Where("user_id = ?", userID).Order("last_seen_at DESC").Limit(10).Find(&devices)
	return devices
}
