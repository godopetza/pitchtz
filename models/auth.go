package models

import (
	"time"

	"github.com/google/uuid"
)

const (
	PasswordResetAudienceAdmin  = "admin"
	PasswordResetAudienceOwner  = "owner"
	PasswordResetAudienceClient = "client"
)

// PasswordResetToken stores only a SHA-256 digest. The raw bearer token exists
// briefly in the reset email and is never persisted.
type PasswordResetToken struct {
	Base
	UserID    uuid.UUID  `gorm:"type:uuid;not null;index:idx_password_reset_lookup,priority:1" json:"-"`
	Audience  string     `gorm:"not null;size:20;index:idx_password_reset_lookup,priority:2" json:"-"`
	TokenHash string     `gorm:"not null;size:64;uniqueIndex" json:"-"`
	ExpiresAt time.Time  `gorm:"not null;index" json:"expiresAt"`
	UsedAt    *time.Time `gorm:"index" json:"usedAt,omitempty"`
}

// EmailOTP is a short-lived 6-digit code proving control of an email address,
// used for customer sign-in on the client app (no password, no SMS cost).
// Only the SHA-256 digest of the code is stored.
type EmailOTP struct {
	Base
	UserID         uuid.UUID  `gorm:"type:uuid;not null;index" json:"-"`
	Email          string     `gorm:"not null;index" json:"-"`
	CodeHash       string     `gorm:"not null;size:64" json:"-"`
	ExpiresAt      time.Time  `gorm:"not null" json:"-"`
	ConsumedAt     *time.Time `json:"-"`
	FailedAttempts int        `gorm:"not null;default:0" json:"-"`
	LockedUntil    *time.Time `json:"-"`
}
