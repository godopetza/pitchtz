package models

import (
	"time"

	"github.com/google/uuid"
)

type Booking struct {
	Base
	Code           string     `gorm:"not null;uniqueIndex" json:"code"`
	PitchID        uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:idx_booking_pitch_start,priority:1" json:"pitchId"`
	UserID         *uuid.UUID `gorm:"type:uuid;index" json:"userId,omitempty"`
	TeamID         *uuid.UUID `gorm:"type:uuid;index" json:"teamId,omitempty"`
	StartsAt       time.Time  `gorm:"not null;uniqueIndex:idx_booking_pitch_start,priority:2" json:"startsAt"`
	EndsAt         time.Time  `gorm:"not null;index" json:"endsAt"`
	Source         string     `gorm:"not null;default:app" json:"source"`
	Status         string     `gorm:"not null;default:pending;index" json:"status"`
	RepeatRule     string     `gorm:"not null;default:once" json:"repeatRule"`
	RepeatParentID *uuid.UUID `gorm:"type:uuid;index" json:"repeatParentId,omitempty"`
	PitchFeeTZS    int64      `gorm:"not null" json:"pitchFeeTzs"`
	ServiceFeeTZS  int64      `gorm:"not null;default:3000" json:"serviceFeeTzs"`
	TotalTZS       int64      `gorm:"not null" json:"totalTzs"`
	// BalanceAtVenue: the player paid a deposit online and settles the rest
	// in cash at the gate — a part-paid booking with this flag is confirmed.
	BalanceAtVenue bool       `gorm:"not null;default:false" json:"balanceAtVenue"`
	CheckedInAt    *time.Time `json:"checkedInAt,omitempty"`
	CancelledAt    *time.Time `json:"cancelledAt,omitempty"`
}

type BookingExtra struct {
	BookingID    uuid.UUID `gorm:"type:uuid;primaryKey" json:"bookingId"`
	ExtraID      uuid.UUID `gorm:"type:uuid;primaryKey" json:"extraId"`
	Quantity     int       `gorm:"not null" json:"quantity"`
	PriceTZSEach int64     `gorm:"not null" json:"priceTzsEach"`
	CreatedAt    time.Time `json:"createdAt"`
}

type PaymentShare struct {
	Base
	BookingID   uuid.UUID  `gorm:"type:uuid;not null;index" json:"bookingId"`
	PayerUserID *uuid.UUID `gorm:"type:uuid;index" json:"payerUserId,omitempty"`
	PayerPhone  string     `json:"payerPhone"`
	AmountTZS   int64      `gorm:"not null" json:"amountTzs"`
	Kind        string     `gorm:"not null" json:"kind"`
	QRSessionID *uuid.UUID `gorm:"type:uuid;uniqueIndex" json:"qrSessionId,omitempty"`
	DueAt       *time.Time `json:"dueAt,omitempty"`
	Status      string     `gorm:"not null;default:unpaid;index" json:"status"`
	PaidAt      *time.Time `json:"paidAt,omitempty"`
}

type PaymentTransaction struct {
	Base
	ShareID        uuid.UUID  `gorm:"type:uuid;not null;index" json:"shareId"`
	Provider       string     `gorm:"not null" json:"provider"`
	Operator       string     `json:"operator"`
	ProviderRef    string     `gorm:"index" json:"providerRef"`
	IdempotencyKey string     `gorm:"not null;uniqueIndex" json:"idempotencyKey"`
	AmountTZS      int64      `gorm:"not null" json:"amountTzs"`
	Direction      string     `gorm:"not null;default:charge" json:"direction"`
	Status         string     `gorm:"not null;default:initiated;index" json:"status"`
	WebhookAt      *time.Time `json:"webhookAt,omitempty"`
	ReconciledAt   *time.Time `json:"reconciledAt,omitempty"`
}
