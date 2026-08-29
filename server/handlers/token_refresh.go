package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/server/middleware"
	"github.com/godopetza/pitchtz/utils"
)

// Sliding sessions: a still-valid token can be exchanged for a fresh one, so
// an active user never gets logged out mid-work. A token that has already
// expired cannot be refreshed — the auth middleware rejects it and the app
// falls back to a clean re-login.

func RefreshClientToken(c *gin.Context) {
	userID, ok := clientUserID(c)
	if !ok {
		return
	}
	token, expiresAt, err := utils.IssueClientToken(userID)
	if err != nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "AUTH_NOT_CONFIGURED", "sign-in is not configured")
		return
	}
	utils.RespondSuccess(c, http.StatusOK, gin.H{"access_token": token, "token_type": "Bearer", "expires_at": expiresAt}, "")
}

func RefreshOwnerToken(c *gin.Context) {
	userID, ok := ownerUserID(c)
	if !ok {
		return
	}
	token, expiresAt, err := utils.IssueOwnerToken(userID)
	if err != nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "AUTH_NOT_CONFIGURED", "sign-in is not configured")
		return
	}
	utils.RespondSuccess(c, http.StatusOK, gin.H{"access_token": token, "token_type": "Bearer", "expires_at": expiresAt}, "")
}

func RefreshAdminToken(c *gin.Context) {
	userID, ok := adminUserID(c)
	if !ok {
		return
	}
	staffValue, exists := c.Get(middleware.AdminStaffKey)
	role := models.AdminRoleSuperAdmin
	if exists {
		if staff, okStaff := staffValue.(models.AdminStaff); okStaff {
			role = staff.Role
		}
	} else {
		var staff models.AdminStaff
		if initializers.DB.WithContext(c.Request.Context()).First(&staff, "user_id = ?", userID).Error == nil {
			role = staff.Role
		}
	}
	token, expiresAt, err := utils.IssueAdminToken(userID, role)
	if err != nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "AUTH_NOT_CONFIGURED", "sign-in is not configured")
		return
	}
	utils.RespondSuccess(c, http.StatusOK, gin.H{"access_token": token, "token_type": "Bearer", "expires_at": expiresAt}, "")
}
