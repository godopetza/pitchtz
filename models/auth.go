package models

import (
	"time"

	"github.com/google/uuid"
)

const (
	PasswordResetAudienceAdmin = "admin"
	PasswordResetAudienceOwner = "owner"
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
