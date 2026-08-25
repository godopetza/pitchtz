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

const (
	AdminUserIDKey = "admin_user_id"
	AdminStaffKey  = "admin_staff"
)

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		parts := strings.Fields(header)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			utils.RespondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "a bearer access token is required")
			c.Abort()
			return
		}
		claims, err := utils.ParseAdminToken(parts[1])
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
		var staff models.AdminStaff
		if err := initializers.DB.WithContext(c.Request.Context()).Where("user_id = ?", userID).First(&staff).Error; err != nil || staff.Status != models.AdminStatusActive {
			utils.RespondError(c, http.StatusForbidden, "ADMIN_DISABLED", "admin access is not active")
			c.Abort()
			return
		}
		c.Set(AdminUserIDKey, userID)
		c.Set(AdminStaffKey, staff)
		c.Next()
	}
}

func RequireAdminRoles(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(c *gin.Context) {
		value, exists := c.Get(AdminStaffKey)
		staff, ok := value.(models.AdminStaff)
		if !exists || !ok {
			utils.RespondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "admin authentication is required")
			c.Abort()
			return
		}
		if _, ok := allowed[staff.Role]; !ok {
			utils.RespondError(c, http.StatusForbidden, "INSUFFICIENT_ROLE", "this admin role cannot perform the action")
			c.Abort()
			return
		}
		c.Next()
	}
}
