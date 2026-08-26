package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/server/middleware"
	"github.com/godopetza/pitchtz/services"
	"github.com/godopetza/pitchtz/utils"
	"github.com/google/uuid"
)

type clientEmailStartInput struct {
	Email string `json:"email" binding:"required,email,max=254"`
}

type clientEmailVerifyInput struct {
	Email string `json:"email" binding:"required,email,max=254"`
	Code  string `json:"code" binding:"required,len=6"`
}

type clientUserDTO struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	Email           *string    `json:"email,omitempty"`
	Phone           *string    `json:"phone,omitempty"`
	AvatarURL       string     `json:"avatar_url"`
	Language        string     `json:"language"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
}

// ClientEmailStart issues (or re-issues) a 6-digit sign-in code for a
// customer email. It always responds 202 whether or not the address already
// has an account, so the endpoint can't be used to enumerate users.
func ClientEmailStart(c *gin.Context) {
	if initializers.DB == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "DATABASE_REQUIRED", "sign-in is unavailable")
		return
	}
	var input clientEmailStartInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "a valid email is required")
		return
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))

	var lastSent models.EmailOTP
	if err := initializers.DB.WithContext(c.Request.Context()).
		Where("email = ?", email).Order("created_at DESC").First(&lastSent).Error; err == nil {
		if time.Since(lastSent.CreatedAt) < 45*time.Second {
			utils.RespondError(c, http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "please wait before requesting another code")
			return
		}
	}

	var user models.User
	if err := initializers.DB.WithContext(c.Request.Context()).Where("LOWER(email) = ?", email).First(&user).Error; err != nil {
		user = models.User{Email: &email, Name: strings.Split(email, "@")[0], AuthProvider: "email", Language: "sw", Role: "player"}
		if err := initializers.DB.WithContext(c.Request.Context()).Create(&user).Error; err != nil {
			if refetchErr := initializers.DB.WithContext(c.Request.Context()).Where("LOWER(email) = ?", email).First(&user).Error; refetchErr != nil {
				utils.RespondError(c, http.StatusInternalServerError, "ACCOUNT_LOOKUP_FAILED", "could not start sign-in")
				return
			}
		}
	}

	code, err := randomDigits(6)
	if err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "CODE_GENERATION_FAILED", "could not start sign-in")
		return
	}
	otp := models.EmailOTP{
		UserID:    user.ID,
		Email:     email,
		CodeHash:  hashCode(code),
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
	}
	if err := initializers.DB.WithContext(c.Request.Context()).Create(&otp).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "CODE_STORE_FAILED", "could not start sign-in")
		return
	}

	if err := services.SendEmailVerificationCode(c.Request.Context(), email, user.Name, code, otp.ID.String()); err != nil {
		utils.RespondError(c, http.StatusBadGateway, "EMAIL_SEND_FAILED", "could not send the sign-in code")
		return
	}

	utils.RespondSuccess(c, http.StatusAccepted, gin.H{"email": email}, "A sign-in code was sent to your email.")
}

// ClientEmailVerify redeems a code minted by ClientEmailStart for a client
// access token. Five wrong guesses locks that code out for 15 minutes,
// mirroring the admin/owner password lockout.
func ClientEmailVerify(c *gin.Context) {
	if initializers.DB == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "DATABASE_REQUIRED", "sign-in is unavailable")
		return
	}
	var input clientEmailVerifyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "a valid email and 6-digit code are required")
		return
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))

	var otp models.EmailOTP
	if err := initializers.DB.WithContext(c.Request.Context()).
		Where("email = ? AND consumed_at IS NULL", email).Order("created_at DESC").First(&otp).Error; err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_CODE", "that code is invalid or has expired")
		return
	}
	if otp.LockedUntil != nil && otp.LockedUntil.After(time.Now()) {
		utils.RespondError(c, http.StatusTooManyRequests, "CODE_LOCKED", "too many failed attempts; request a new code")
		return
	}
	if otp.ExpiresAt.Before(time.Now()) {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_CODE", "that code is invalid or has expired")
		return
	}
	if otp.CodeHash != hashCode(strings.TrimSpace(input.Code)) {
		updates := map[string]interface{}{"failed_attempts": otp.FailedAttempts + 1}
		if otp.FailedAttempts+1 >= 5 {
			updates["locked_until"] = time.Now().UTC().Add(15 * time.Minute)
		}
		initializers.DB.Model(&models.EmailOTP{}).Where("id = ?", otp.ID).Updates(updates)
		utils.RespondError(c, http.StatusBadRequest, "INVALID_CODE", "that code is invalid or has expired")
		return
	}

	var user models.User
	if err := initializers.DB.WithContext(c.Request.Context()).First(&user, "id = ?", otp.UserID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "ACCOUNT_NOT_FOUND", "account was not found")
		return
	}

	now := time.Now().UTC()
	initializers.DB.Model(&models.EmailOTP{}).Where("id = ?", otp.ID).Update("consumed_at", now)
	userUpdates := map[string]interface{}{"last_login_provider": "email", "last_login_at": now}
	if user.EmailVerifiedAt == nil {
		userUpdates["email_verified_at"] = now
	}
	initializers.DB.Model(&models.User{}).Where("id = ?", user.ID).Updates(userUpdates)

	token, expiresAt, err := utils.IssueClientToken(user.ID)
	if err != nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "AUTH_NOT_CONFIGURED", "sign-in is not configured")
		return
	}
	initializers.DB.WithContext(c.Request.Context()).First(&user, "id = ?", user.ID)
	utils.RespondSuccess(c, http.StatusOK, gin.H{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_at":   expiresAt,
		"user":         toClientUserDTO(user),
	}, "")
}

func ClientMe(c *gin.Context) {
	userID, ok := clientUserID(c)
	if !ok {
		return
	}
	var user models.User
	if err := initializers.DB.WithContext(c.Request.Context()).First(&user, "id = ?", userID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "ACCOUNT_NOT_FOUND", "account was not found")
		return
	}
	utils.RespondSuccess(c, http.StatusOK, toClientUserDTO(user), "")
}

func ClientGoogleStart(c *gin.Context) { startGoogle(c, models.PasswordResetAudienceClient) }
func ClientAppleStart(c *gin.Context)  { startApple(c, models.PasswordResetAudienceClient) }

func clientUserID(c *gin.Context) (uuid.UUID, bool) {
	value, ok := c.Get(middleware.ClientUserIDKey)
	if !ok {
		utils.RespondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "sign-in required")
		return uuid.Nil, false
	}
	id, ok := value.(uuid.UUID)
	if !ok {
		utils.RespondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "sign-in required")
		return uuid.Nil, false
	}
	return id, true
}

func toClientUserDTO(user models.User) clientUserDTO {
	return clientUserDTO{
		ID:              user.ID,
		Name:            user.Name,
		Email:           user.Email,
		Phone:           user.Phone,
		AvatarURL:       user.AvatarURL,
		Language:        user.Language,
		EmailVerifiedAt: user.EmailVerifiedAt,
	}
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func randomDigits(n int) (string, error) {
	digits := make([]byte, n)
	ceiling := big.NewInt(10)
	for i := range digits {
		v, err := rand.Int(rand.Reader, ceiling)
		if err != nil {
			return "", err
		}
		digits[i] = byte('0' + v.Int64())
	}
	return string(digits), nil
}
