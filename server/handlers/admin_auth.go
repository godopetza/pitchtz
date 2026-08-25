package handlers

import (
	"encoding/json"
	"errors"
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
	"gorm.io/gorm"
)

type adminLoginInput struct {
	Email    string `json:"email" binding:"required,email,max=254"`
	Password string `json:"password" binding:"required,max=128"`
}

type createAdminInput struct {
	Name     string   `json:"name" binding:"required,min=2,max=100"`
	Email    string   `json:"email" binding:"required,email,max=254"`
	Password string   `json:"temporary_password" binding:"required,min=12,max=128"`
	Role     string   `json:"role" binding:"required"`
	Scopes   []string `json:"scopes"`
}

type changePasswordInput struct {
	CurrentPassword string `json:"current_password" binding:"required,max=128"`
	NewPassword     string `json:"new_password" binding:"required,min=12,max=128"`
}

type adminDTO struct {
	ID                 uuid.UUID  `json:"id"`
	Name               string     `json:"name"`
	Email              string     `json:"email"`
	Role               string     `json:"role"`
	Status             string     `json:"status"`
	Scopes             []string   `json:"scopes"`
	MustChangePassword bool       `json:"must_change_password"`
	LastActiveAt       *time.Time `json:"last_active_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

func AdminLogin(c *gin.Context) {
	if initializers.DB == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "DATABASE_REQUIRED", "admin login is unavailable")
		return
	}
	var input adminLoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "a valid email and password are required")
		return
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	var user models.User
	if err := initializers.DB.WithContext(c.Request.Context()).Where("LOWER(email) = ?", email).First(&user).Error; err != nil {
		respondInvalidAdminCredentials(c)
		return
	}
	var staff models.AdminStaff
	var credential models.AdminCredential
	if err := initializers.DB.WithContext(c.Request.Context()).Where("user_id = ?", user.ID).First(&staff).Error; err != nil {
		respondInvalidAdminCredentials(c)
		return
	}
	if staff.Status != models.AdminStatusActive {
		utils.RespondError(c, http.StatusForbidden, "ADMIN_DISABLED", "admin access is not active")
		return
	}
	if err := initializers.DB.WithContext(c.Request.Context()).First(&credential, "user_id = ?", user.ID).Error; err != nil {
		respondInvalidAdminCredentials(c)
		return
	}
	if credential.LockedUntil != nil && credential.LockedUntil.After(time.Now()) {
		utils.RespondError(c, http.StatusTooManyRequests, "ACCOUNT_LOCKED", "too many failed attempts; try again later")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(credential.PasswordHash), []byte(input.Password)); err != nil {
		recordFailedAdminLogin(user.ID, credential.FailedLoginCount+1)
		respondInvalidAdminCredentials(c)
		return
	}

	now := time.Now().UTC()
	initializers.DB.Model(&models.AdminCredential{}).Where("user_id = ?", user.ID).Updates(map[string]interface{}{"failed_login_count": 0, "locked_until": nil})
	initializers.DB.Model(&models.AdminStaff{}).Where("user_id = ?", user.ID).Update("last_active_at", now)
	token, expiresAt, err := utils.IssueAdminToken(user.ID, staff.Role)
	if err != nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "AUTH_NOT_CONFIGURED", "admin authentication is not configured")
		return
	}
	staff.LastActiveAt = &now
	utils.RespondSuccess(c, http.StatusOK, gin.H{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_at":   expiresAt,
		"admin":        toAdminDTO(user, staff, credential),
	}, "")
}

func AdminMe(c *gin.Context) {
	userID, ok := adminUserID(c)
	if !ok {
		return
	}
	var user models.User
	var staff models.AdminStaff
	var credential models.AdminCredential
	if err := initializers.DB.WithContext(c.Request.Context()).First(&user, "id = ?", userID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "ADMIN_NOT_FOUND", "admin account was not found")
		return
	}
	initializers.DB.WithContext(c.Request.Context()).First(&staff, "user_id = ?", userID)
	initializers.DB.WithContext(c.Request.Context()).First(&credential, "user_id = ?", userID)
	utils.RespondSuccess(c, http.StatusOK, toAdminDTO(user, staff, credential), "")
}

func ListAdmins(c *gin.Context) {
	type row struct {
		models.AdminStaff
		Name               string
		Email              *string
		MustChangePassword bool
	}
	var rows []row
	err := initializers.DB.WithContext(c.Request.Context()).
		Table("admin_staffs").
		Select("admin_staffs.*, users.name, users.email, admin_credentials.must_change_password").
		Joins("JOIN users ON users.id = admin_staffs.user_id").
		Joins("JOIN admin_credentials ON admin_credentials.user_id = admin_staffs.user_id").
		Order("admin_staffs.created_at ASC").
		Scan(&rows).Error
	if err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "ADMIN_LIST_FAILED", "could not load admins")
		return
	}
	result := make([]adminDTO, 0, len(rows))
	for _, row := range rows {
		user := models.User{Base: models.Base{ID: row.UserID, CreatedAt: row.CreatedAt}, Name: row.Name, Email: row.Email}
		credential := models.AdminCredential{MustChangePassword: row.MustChangePassword}
		result = append(result, toAdminDTO(user, row.AdminStaff, credential))
	}
	utils.RespondSuccess(c, http.StatusOK, result, "")
}

func CreateAdmin(c *gin.Context) {
	var input createAdminInput
	if err := c.ShouldBindJSON(&input); err != nil || !models.IsAdminRole(input.Role) {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "name, email, a 12+ character temporary password, and a valid role are required")
		return
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), 12)
	if err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "PASSWORD_HASH_FAILED", "could not secure the temporary password")
		return
	}
	scopes, _ := json.Marshal(input.Scopes)
	user := models.User{Email: &email, Name: strings.TrimSpace(input.Name), AuthProvider: "password", Language: "en", Role: "admin"}
	staff := models.AdminStaff{Role: input.Role, Status: models.AdminStatusActive, Scopes: scopes}
	credential := models.AdminCredential{PasswordHash: string(hash), MustChangePassword: true}
	err = initializers.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		staff.UserID = user.ID
		credential.UserID = user.ID
		if err := tx.Create(&staff).Error; err != nil {
			return err
		}
		return tx.Create(&credential).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			utils.RespondError(c, http.StatusConflict, "EMAIL_EXISTS", "an account with this email already exists")
			return
		}
		utils.RespondError(c, http.StatusInternalServerError, "ADMIN_CREATE_FAILED", "could not create the admin")
		return
	}
	utils.RespondSuccess(c, http.StatusCreated, toAdminDTO(user, staff, credential), "Admin created. Share the temporary password securely.")
}

func ChangeAdminPassword(c *gin.Context) {
	userID, ok := adminUserID(c)
	if !ok {
		return
	}
	var input changePasswordInput
	if err := c.ShouldBindJSON(&input); err != nil || input.CurrentPassword == input.NewPassword {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "current password and a different 12+ character new password are required")
		return
	}
	var credential models.AdminCredential
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
	if err := initializers.DB.WithContext(c.Request.Context()).Model(&models.AdminCredential{}).Where("user_id = ?", userID).Updates(map[string]interface{}{
		"password_hash": string(hash), "must_change_password": false, "password_changed_at": now, "failed_login_count": 0, "locked_until": nil,
	}).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "PASSWORD_CHANGE_FAILED", "could not change the password")
		return
	}
	utils.RespondSuccess(c, http.StatusOK, gin.H{"password_changed": true}, "Password changed.")
}

func recordFailedAdminLogin(userID uuid.UUID, attempts int) {
	updates := map[string]interface{}{"failed_login_count": attempts}
	if attempts >= 5 {
		lockedUntil := time.Now().UTC().Add(15 * time.Minute)
		updates["locked_until"] = lockedUntil
		updates["failed_login_count"] = 0
	}
	initializers.DB.Model(&models.AdminCredential{}).Where("user_id = ?", userID).Updates(updates)
}

func respondInvalidAdminCredentials(c *gin.Context) {
	utils.RespondError(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "email or password is incorrect")
}

func adminUserID(c *gin.Context) (uuid.UUID, bool) {
	value, exists := c.Get(middleware.AdminUserIDKey)
	userID, ok := value.(uuid.UUID)
	if !exists || !ok {
		utils.RespondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "admin authentication is required")
		return uuid.Nil, false
	}
	return userID, true
}

func toAdminDTO(user models.User, staff models.AdminStaff, credential models.AdminCredential) adminDTO {
	email := ""
	if user.Email != nil {
		email = *user.Email
	}
	scopes := make([]string, 0)
	_ = json.Unmarshal(staff.Scopes, &scopes)
	return adminDTO{ID: user.ID, Name: user.Name, Email: email, Role: staff.Role, Status: staff.Status, Scopes: scopes, MustChangePassword: credential.MustChangePassword, LastActiveAt: staff.LastActiveAt, CreatedAt: staff.CreatedAt}
}
