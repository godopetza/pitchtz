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

const ClientUserIDKey = "client_user_id"

// RequireClient authenticates a booking customer on the public client app.
// Unlike owner/admin, there is no separate credential/status row to check —
// any user record is a valid session subject.
func RequireClient() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		parts := strings.Fields(header)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			utils.RespondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "a bearer access token is required")
			c.Abort()
			return
		}
		claims, err := utils.ParseClientToken(parts[1])
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
		var user models.User
		if err := initializers.DB.WithContext(c.Request.Context()).First(&user, "id = ?", userID).Error; err != nil {
			utils.RespondError(c, http.StatusUnauthorized, "INVALID_TOKEN", "account was not found")
			c.Abort()
			return
		}
		c.Set(ClientUserIDKey, userID)
		c.Next()
	}
}

// OptionalClient parses a bearer token when one is present, so public pages
// can personalise (e.g. team membership) — but never rejects anonymous
// visitors.
func OptionalClient() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		parts := strings.Fields(header)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			if claims, err := utils.ParseClientToken(parts[1]); err == nil {
				if userID, err := uuid.Parse(claims.Subject); err == nil {
					c.Set(ClientUserIDKey, userID)
				}
			}
		}
		c.Next()
	}
}
