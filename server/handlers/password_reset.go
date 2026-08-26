package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/services"
	"github.com/godopetza/pitchtz/utils"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type forgotPasswordInput struct {
	Email string `json:"email" binding:"required,email,max=254"`
}

type resetPasswordInput struct {
	Token       string `json:"token" binding:"required,min=32,max=256"`
	NewPassword string `json:"new_password" binding:"required,min=12,max=128"`
}

func AdminForgotPassword(c *gin.Context) { requestPasswordReset(c, models.PasswordResetAudienceAdmin) }
func OwnerForgotPassword(c *gin.Context) { requestPasswordReset(c, models.PasswordResetAudienceOwner) }
func AdminResetPassword(c *gin.Context)  { completePasswordReset(c, models.PasswordResetAudienceAdmin) }
func OwnerResetPassword(c *gin.Context)  { completePasswordReset(c, models.PasswordResetAudienceOwner) }

func requestPasswordReset(c *gin.Context, audience string) {
	var input forgotPasswordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "a valid email is required")
		return
	}
	generic := func() {
		utils.RespondSuccess(c, http.StatusOK, gin.H{"requested": true}, "If an active account exists for that email, a reset link has been sent.")
	}
	if initializers.DB == nil {
		generic()
		return
	}

	email := strings.ToLower(strings.TrimSpace(input.Email))
	var user models.User
	if err := initializers.DB.WithContext(c.Request.Context()).Where("LOWER(email) = ?", email).First(&user).Error; err != nil || user.Email == nil {
		generic()
		return
	}
	if !passwordAccountActive(c, user.ID.String(), audience) {
		generic()
		return
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		log.Printf("password reset: random token failed: %v", err)
		generic()
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	record := models.PasswordResetToken{
		UserID: user.ID, Audience: audience, TokenHash: hex.EncodeToString(digest[:]), ExpiresAt: now.Add(30 * time.Minute),
	}
	err := initializers.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.PasswordResetToken{}).Where("user_id = ? AND audience = ? AND used_at IS NULL", user.ID, audience).Update("used_at", now).Error; err != nil {
			return err
		}
		return tx.Create(&record).Error
	})
	if err != nil {
		log.Printf("password reset: store token failed: %v", err)
		generic()
		return
	}

	appURL := strings.TrimRight(strings.TrimSpace(os.Getenv("OWNER_APP_URL")), "/")
	if audience == models.PasswordResetAudienceAdmin {
		appURL = strings.TrimRight(strings.TrimSpace(os.Getenv("ADMIN_APP_URL")), "/")
	}
	if appURL == "" {
		if audience == models.PasswordResetAudienceAdmin {
			appURL = "http://localhost:3001"
		} else {
			appURL = "http://localhost:3002"
		}
	}
	resetURL := appURL + "/?reset_token=" + url.QueryEscape(token)
	if err := services.SendPasswordReset(c.Request.Context(), *user.Email, user.Name, resetURL, audience, "password-reset-"+record.ID.String()); err != nil {
		log.Printf("password reset: email delivery failed for user %s: %v", user.ID, err)
	}
	generic()
}

func passwordAccountActive(c *gin.Context, userID, audience string) bool {
	if audience == models.PasswordResetAudienceAdmin {
		var staff models.AdminStaff
		var credential models.AdminCredential
		return initializers.DB.WithContext(c.Request.Context()).First(&staff, "user_id = ? AND status = ?", userID, models.AdminStatusActive).Error == nil &&
			initializers.DB.WithContext(c.Request.Context()).First(&credential, "user_id = ?", userID).Error == nil
	}
	var credential models.OwnerCredential
	return initializers.DB.WithContext(c.Request.Context()).First(&credential, "user_id = ? AND status = ?", userID, models.OwnerStatusActive).Error == nil
}

func completePasswordReset(c *gin.Context, audience string) {
	if initializers.DB == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "DATABASE_REQUIRED", "password reset is unavailable")
		return
	}
	var input resetPasswordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "a valid reset token and 12+ character password are required")
		return
	}
	digest := sha256.Sum256([]byte(input.Token))
	tokenHash := hex.EncodeToString(digest[:])
	hash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), 12)
	if err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "PASSWORD_HASH_FAILED", "could not secure the new password")
		return
	}

	now := time.Now().UTC()
	errInvalid := gorm.ErrRecordNotFound
	err = initializers.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var token models.PasswordResetToken
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ? AND audience = ? AND used_at IS NULL AND expires_at > ?", tokenHash, audience, now).First(&token).Error; err != nil {
			return errInvalid
		}
		updates := map[string]interface{}{"password_hash": string(hash), "must_change_password": false, "password_changed_at": now, "failed_login_count": 0, "locked_until": nil}
		var result *gorm.DB
		if audience == models.PasswordResetAudienceAdmin {
			result = tx.Model(&models.AdminCredential{}).Where("user_id = ?", token.UserID).Updates(updates)
		} else {
			result = tx.Model(&models.OwnerCredential{}).Where("user_id = ?", token.UserID).Updates(updates)
		}
		if result.Error != nil || result.RowsAffected != 1 {
			return errInvalid
		}
		return tx.Model(&models.PasswordResetToken{}).Where("user_id = ? AND audience = ? AND used_at IS NULL", token.UserID, audience).Update("used_at", now).Error
	})
	if err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_RESET_TOKEN", "this reset link is invalid or has expired")
		return
	}
	utils.RespondSuccess(c, http.StatusOK, gin.H{"password_reset": true}, "Password reset complete.")
}
