package models

import (
	"time"

	"github.com/google/uuid"
)

const (
	ShopOrderStatusPending   = "pending"
	ShopOrderStatusPaid      = "paid"
	ShopOrderStatusFulfilled = "fulfilled"
	ShopOrderStatusCancelled = "cancelled"
)

// ShopProduct is one item for sale. VenueID nil means the PitchTZ platform
// shop (superadmin-run); a set VenueID makes it a venue's own shop item — the
// per-venue shop feature only needs owner endpoints, not a schema change.
type ShopProduct struct {
	Base
	VenueID     *uuid.UUID `gorm:"type:uuid;index" json:"venueId,omitempty"`
	Name        string     `gorm:"not null" json:"name"`
	Description string     `json:"description"`
	PriceTZS    int64      `gorm:"not null" json:"priceTzs"`
	ImageURL    string     `json:"imageUrl"`
	Stock       int        `gorm:"not null;default:0" json:"stock"`
	Active      bool       `gorm:"not null;default:true;index" json:"active"`
}

type ShopOrder struct {
	Base
	Code       string     `gorm:"not null;uniqueIndex" json:"code"`
	UserID     uuid.UUID  `gorm:"type:uuid;not null;index" json:"userId"`
	Status     string     `gorm:"not null;default:pending;index" json:"status"`
	TotalTZS   int64      `gorm:"not null" json:"totalTzs"`
	Phone      string     `json:"phone"`
	PaidAt     *time.Time `json:"paidAt,omitempty"`
	FulfilledAt *time.Time `json:"fulfilledAt,omitempty"`
}

type ShopOrderItem struct {
	OrderID      uuid.UUID `gorm:"type:uuid;primaryKey" json:"orderId"`
	ProductID    uuid.UUID `gorm:"type:uuid;primaryKey" json:"productId"`
	Quantity     int       `gorm:"not null" json:"quantity"`
	PriceTZSEach int64     `gorm:"not null" json:"priceTzsEach"`
	Name         string    `gorm:"not null" json:"name"`
	CreatedAt    time.Time `json:"createdAt"`
}
