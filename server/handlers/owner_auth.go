package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/server/middleware"
	"github.com/godopetza/pitchtz/utils"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type ownerLoginInput struct {
	Email    string `json:"email" binding:"required,email,max=254"`
	Password string `json:"password" binding:"required,max=128"`
}

type changeOwnerPasswordInput struct {
	CurrentPassword string `json:"current_password" binding:"required,max=128"`
	NewPassword     string `json:"new_password" binding:"required,min=12,max=128"`
}

type ownerDTO struct {
	ID                 uuid.UUID   `json:"id"`
	Name               string      `json:"name"`
	Email              string      `json:"email"`
	AvatarURL          string      `json:"avatar_url,omitempty"`
	Status             string      `json:"status"`
	MustChangePassword bool        `json:"must_change_password"`
	Venues             []uuid.UUID `json:"venue_ids"`
	LastVenueID        *uuid.UUID  `json:"last_venue_id,omitempty"`
	LastLoginProvider  string      `json:"last_login_provider,omitempty"`
	LastLoginAt        *time.Time  `json:"last_login_at,omitempty"`
	CreatedAt          time.Time   `json:"created_at"`
}

func OwnerLogin(c *gin.Context) {
	if initializers.DB == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "DATABASE_REQUIRED", "owner login is unavailable")
		return
	}
	var input ownerLoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "a valid email and password are required")
		return
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	var user models.User
	if err := initializers.DB.WithContext(c.Request.Context()).Where("LOWER(email) = ?", email).First(&user).Error; err != nil {
		respondInvalidOwnerCredentials(c)
		return
	}
	var credential models.OwnerCredential
	if err := initializers.DB.WithContext(c.Request.Context()).First(&credential, "user_id = ?", user.ID).Error; err != nil {
		respondInvalidOwnerCredentials(c)
		return
	}
	if credential.Status != models.OwnerStatusActive {
		utils.RespondError(c, http.StatusForbidden, "OWNER_DISABLED", "owner access is not active")
		return
	}
	if credential.LockedUntil != nil && credential.LockedUntil.After(time.Now()) {
		utils.RespondError(c, http.StatusTooManyRequests, "ACCOUNT_LOCKED", "too many failed attempts; try again later")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(credential.PasswordHash), []byte(input.Password)); err != nil {
		recordFailedOwnerLogin(user.ID, credential.FailedLoginCount+1)
		respondInvalidOwnerCredentials(c)
		return
	}

	now := time.Now().UTC()
	initializers.DB.Model(&models.OwnerCredential{}).Where("user_id = ?", user.ID).Updates(map[string]interface{}{"failed_login_count": 0, "locked_until": nil})
	initializers.DB.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]interface{}{"last_login_provider": "password", "last_login_at": now})
	user.LastLoginProvider = "password"
	user.LastLoginAt = &now
	token, expiresAt, err := utils.IssueOwnerToken(user.ID)
	if err != nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "AUTH_NOT_CONFIGURED", "owner authentication is not configured")
		return
	}
	utils.RespondSuccess(c, http.StatusOK, gin.H{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_at":   expiresAt,
		"owner":        toOwnerDTO(c, user, credential),
	}, "")
}

func OwnerMe(c *gin.Context) {
	userID, ok := ownerUserID(c)
	if !ok {
		return
	}
	var user models.User
	var credential models.OwnerCredential
	if err := initializers.DB.WithContext(c.Request.Context()).First(&user, "id = ?", userID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "OWNER_NOT_FOUND", "owner account was not found")
		return
	}
	initializers.DB.WithContext(c.Request.Context()).First(&credential, "user_id = ?", userID)
	utils.RespondSuccess(c, http.StatusOK, toOwnerDTO(c, user, credential), "")
}

func ChangeOwnerPassword(c *gin.Context) {
	userID, ok := ownerUserID(c)
	if !ok {
		return
	}
	var input changeOwnerPasswordInput
	if err := c.ShouldBindJSON(&input); err != nil || input.CurrentPassword == input.NewPassword {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "current password and a different 12+ character new password are required")
		return
	}
	var credential models.OwnerCredential
	if err := initializers.DB.WithContext(c.Request.Context()).First(&credential, "user_id = ?", userID).Error; err != nil || bcrypt.CompareHashAndPassword([]byte(credential.PasswordHash), []byte(input.CurrentPassword)) != nil {
		utils.RespondError(c, http.StatusUnauthorized, "INVALID_PASSWORD", "current password is incorrect")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), 12)
	if err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "PASSWORD_HASH_FAILED", "could not secure the new password")
		return
	}
	now := time.Now().UTC()
	if err := initializers.DB.WithContext(c.Request.Context()).Model(&models.OwnerCredential{}).Where("user_id = ?", userID).Updates(map[string]interface{}{
		"password_hash": string(hash), "must_change_password": false, "password_changed_at": now, "failed_login_count": 0, "locked_until": nil,
	}).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "PASSWORD_CHANGE_FAILED", "could not change the password")
		return
	}
	utils.RespondSuccess(c, http.StatusOK, gin.H{"password_changed": true}, "Password changed.")
}

func recordFailedOwnerLogin(userID uuid.UUID, attempts int) {
	updates := map[string]interface{}{"failed_login_count": attempts}
	if attempts >= 5 {
		lockedUntil := time.Now().UTC().Add(15 * time.Minute)
		updates["locked_until"] = lockedUntil
		updates["failed_login_count"] = 0
	}
	initializers.DB.Model(&models.OwnerCredential{}).Where("user_id = ?", userID).Updates(updates)
}

func respondInvalidOwnerCredentials(c *gin.Context) {
	utils.RespondError(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "email or password is incorrect")
}

func ownerUserID(c *gin.Context) (uuid.UUID, bool) {
	value, exists := c.Get(middleware.OwnerUserIDKey)
	userID, ok := value.(uuid.UUID)
	if !exists || !ok {
		utils.RespondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "owner authentication is required")
		return uuid.Nil, false
	}
	return userID, true
}

func toOwnerDTO(c *gin.Context, user models.User, credential models.OwnerCredential) ownerDTO {
	email := ""
	if user.Email != nil {
		email = *user.Email
	}
	var venueIDs []uuid.UUID
	initializers.DB.WithContext(c.Request.Context()).Model(&models.Venue{}).Where("owner_id = ?", user.ID).Pluck("id", &venueIDs)
	return ownerDTO{
		ID: user.ID, Name: user.Name, Email: email, AvatarURL: user.AvatarURL, Status: credential.Status,
		MustChangePassword: credential.MustChangePassword, Venues: venueIDs, LastLoginProvider: user.LastLoginProvider, LastLoginAt: user.LastLoginAt, CreatedAt: user.CreatedAt,
		LastVenueID: user.LastVenueID,
	}
}

// SetOwnerLastVenue remembers which venue the owner is working in so the
// portal reopens there on their next visit, from any device. Silently ignores
// a venue that is not theirs rather than erroring — this is a convenience,
// never something worth interrupting the user for.
func SetOwnerLastVenue(c *gin.Context) {
	userID, ok := ownerUserID(c)
	if !ok {
		return
	}
	var input struct {
		VenueID uuid.UUID `json:"venue_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "venue_id is required")
		return
	}
	var owned int64
	initializers.DB.WithContext(c.Request.Context()).Model(&models.Venue{}).
		Where("id = ? AND owner_id = ?", input.VenueID, userID).Count(&owned)
	if owned == 0 {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_VENUE", "this venue is not on your account")
		return
	}
	initializers.DB.WithContext(c.Request.Context()).Model(&models.User{}).
		Where("id = ?", userID).Update("last_venue_id", input.VenueID)
	utils.RespondSuccess(c, http.StatusOK, gin.H{"last_venue_id": input.VenueID}, "")
}
