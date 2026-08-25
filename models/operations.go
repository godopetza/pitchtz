package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type AdminStaff struct {
	Base
	UserID       uuid.UUID      `gorm:"type:uuid;not null;uniqueIndex" json:"userId"`
	Role         string         `gorm:"not null;index" json:"role"`
	Scopes       datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"scopes"`
	LastActiveAt *time.Time     `json:"lastActiveAt,omitempty"`
}

type VenuePayoutSetting struct {
	VenueID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"venueId"`
	Frequency      string    `gorm:"not null;default:weekly" json:"frequency"`
	WeeklyDay      *int      `json:"weeklyDay,omitempty"`
	PayoutMethodID uuid.UUID `gorm:"type:uuid;not null" json:"payoutMethodId"`
	NextPayoutAt   time.Time `gorm:"not null;index" json:"nextPayoutAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Payout struct {
	Base
	VenueID       uuid.UUID  `gorm:"type:uuid;not null;index" json:"venueId"`
	PeriodStart   time.Time  `gorm:"not null" json:"periodStart"`
	PeriodEnd     time.Time  `gorm:"not null" json:"periodEnd"`
	CutoffAt      time.Time  `gorm:"not null" json:"cutoffAt"`
	Frequency     string     `gorm:"not null" json:"frequency"`
	RunKey        string     `gorm:"not null;uniqueIndex" json:"runKey"`
	GrossTZS      int64      `gorm:"not null" json:"grossTzs"`
	FeeTZS        int64      `gorm:"not null" json:"feeTzs"`
	RefundsTZS    int64      `gorm:"not null" json:"refundsTzs"`
	NetTZS        int64      `gorm:"not null" json:"netTzs"`
	MethodMasked  string     `gorm:"not null" json:"methodMasked"`
	ProviderRef   string     `json:"providerRef"`
	FailureReason string     `json:"failureReason"`
	Status        string     `gorm:"not null;default:scheduled;index" json:"status"`
	PaidAt        *time.Time `json:"paidAt,omitempty"`
}

type PayoutItem struct {
	PayoutID      uuid.UUID `gorm:"type:uuid;primaryKey" json:"payoutId"`
	TransactionID uuid.UUID `gorm:"type:uuid;primaryKey;uniqueIndex" json:"transactionId"`
	BookingID     uuid.UUID `gorm:"type:uuid;not null;index" json:"bookingId"`
	GrossTZS      int64     `gorm:"not null" json:"grossTzs"`
	FeeTZS        int64     `gorm:"not null" json:"feeTzs"`
	RefundTZS     int64     `gorm:"not null" json:"refundTzs"`
	NetTZS        int64     `gorm:"not null" json:"netTzs"`
}

type Dispute struct {
	Base
	BookingID  uuid.UUID      `gorm:"type:uuid;not null;index" json:"bookingId"`
	Kind       string         `gorm:"not null" json:"kind"`
	OpenedBy   uuid.UUID      `gorm:"type:uuid;not null;index" json:"openedBy"`
	Detail     string         `gorm:"not null" json:"detail"`
	Evidence   datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"evidence"`
	Status     string         `gorm:"not null;default:open;index" json:"status"`
	ResolvedBy *uuid.UUID     `gorm:"type:uuid" json:"resolvedBy,omitempty"`
	ResolvedAt *time.Time     `json:"resolvedAt,omitempty"`
}

type Notification struct {
	Base
	UserID      *uuid.UUID     `gorm:"type:uuid;index" json:"userId,omitempty"`
	Phone       string         `json:"phone"`
	Email       string         `json:"email"`
	Channel     string         `gorm:"not null;index" json:"channel"`
	Provider    string         `json:"provider"`
	ProviderRef string         `json:"providerRef"`
	Template    string         `gorm:"not null" json:"template"`
	Payload     datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"payload"`
	Status      string         `gorm:"not null;default:queued;index" json:"status"`
	Attempts    int            `gorm:"not null;default:0" json:"attempts"`
	SentAt      *time.Time     `json:"sentAt,omitempty"`
}
