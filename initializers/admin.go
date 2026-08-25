package initializers

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/godopetza/pitchtz/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// BootstrapAdmin creates the first super admin exactly once. Once an admin
// exists, bootstrap credentials are ignored and should be removed from the
// deployment environment.
func BootstrapAdmin() (bool, error) {
	email := strings.ToLower(strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL")))
	password := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	name := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_NAME"))
	if email == "" && password == "" && name == "" {
		return false, nil
	}
	if DB == nil {
		return false, fmt.Errorf("bootstrap admin requires a database")
	}
	if email == "" || password == "" {
		return false, fmt.Errorf("BOOTSTRAP_ADMIN_EMAIL and BOOTSTRAP_ADMIN_PASSWORD must both be set")
	}
	if len(password) < 12 {
		return false, fmt.Errorf("BOOTSTRAP_ADMIN_PASSWORD must be at least 12 characters")
	}
	if name == "" {
		name = "PitchTZ Super Admin"
	}

	var adminCount int64
	if err := DB.Model(&models.AdminStaff{}).Count(&adminCount).Error; err != nil {
		return false, fmt.Errorf("count admins: %w", err)
	}
	if adminCount > 0 {
		return false, nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return false, fmt.Errorf("hash bootstrap password: %w", err)
	}
	scopes, _ := json.Marshal([]string{"*"})
	now := time.Now().UTC()
	user := models.User{Email: &email, Name: name, AuthProvider: "password", Language: "en", Role: "admin"}
	staff := models.AdminStaff{Role: models.AdminRoleSuperAdmin, Status: models.AdminStatusActive, Scopes: scopes}
	credential := models.AdminCredential{PasswordHash: string(hash), MustChangePassword: false, PasswordChangedAt: &now}

	err = DB.Transaction(func(tx *gorm.DB) error {
		var existing int64
		if err := tx.Model(&models.User{}).Where("LOWER(email) = ?", email).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return fmt.Errorf("a user with BOOTSTRAP_ADMIN_EMAIL already exists")
		}
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
		return false, fmt.Errorf("create bootstrap admin: %w", err)
	}
	return true, nil
}
