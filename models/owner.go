package models

import (
	"time"

	"github.com/google/uuid"
)

// OwnerCredential authenticates a venue owner. The owner's venues are found
// via Venue.OwnerID = User.ID — no separate membership table is needed since
// one owner can hold several venues directly.
type OwnerCredential struct {
	UserID             uuid.UUID  `gorm:"type:uuid;primaryKey" json:"userId"`
	Status             string     `gorm:"not null;default:active;index" json:"status"`
	PasswordHash       string     `gorm:"not null" json:"-"`
	MustChangePassword bool       `gorm:"not null;default:true" json:"mustChangePassword"`
	PasswordChangedAt  *time.Time `json:"passwordChangedAt,omitempty"`
	FailedLoginCount   int        `gorm:"not null;default:0" json:"-"`
	LockedUntil        *time.Time `json:"-"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

const (
	OwnerStatusActive   = "active"
	OwnerStatusDisabled = "disabled"
)
