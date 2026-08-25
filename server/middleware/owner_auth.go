package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/utils"
	"github.com/google/uuid"
)

const OwnerUserIDKey = "owner_user_id"

func RequireOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		parts := strings.Fields(header)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			utils.RespondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "a bearer access token is required")
			c.Abort()
			return
		}
		claims, err := utils.ParseOwnerToken(parts[1])
		if err != nil {
			utils.RespondError(c, http.StatusUnauthorized, "INVALID_TOKEN", "access token is invalid or expired")
			c.Abort()
			return
		}
		userID, err := uuid.Parse(claims.Subject)
		if err != nil || initializers.DB == nil {
			utils.RespondError(c, http.StatusUnauthorized, "INVALID_TOKEN", "access token is invalid")
			c.Abort()
			return
		}
		var credential models.OwnerCredential
		if err := initializers.DB.WithContext(c.Request.Context()).Where("user_id = ?", userID).First(&credential).Error; err != nil || credential.Status != models.OwnerStatusActive {
			utils.RespondError(c, http.StatusForbidden, "OWNER_DISABLED", "owner access is not active")
			c.Abort()
			return
		}
		c.Set(OwnerUserIDKey, userID)
		c.Next()
	}
}
