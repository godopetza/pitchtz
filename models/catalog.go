package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type User struct {
	Base
	Phone             *string    `gorm:"uniqueIndex" json:"phone,omitempty"`
	Email             *string    `gorm:"uniqueIndex" json:"email,omitempty"`
	PhoneVerifiedAt   *time.Time `json:"phoneVerifiedAt,omitempty"`
	EmailVerifiedAt   *time.Time `json:"emailVerifiedAt,omitempty"`
	Name              string     `json:"name"`
	AvatarURL         string     `json:"avatarUrl"`
	AuthProvider      string     `gorm:"not null;default:phone" json:"authProvider"`
	LastLoginProvider string     `gorm:"not null;default:''" json:"lastLoginProvider,omitempty"`
	LastLoginAt       *time.Time `json:"lastLoginAt,omitempty"`
	Language          string     `gorm:"not null;default:sw" json:"language"`
	CityID            *uuid.UUID `gorm:"type:uuid;index" json:"cityId,omitempty"`
	Role              string     `gorm:"not null;default:player;index" json:"role"`
	FPLTeamID         *int64     `json:"fplTeamId,omitempty"`
	WhatsAppOptIn     bool       `gorm:"not null;default:false" json:"whatsappOptIn"`
}

type City struct {
	Base
	Name      string     `gorm:"not null;uniqueIndex" json:"name"`
	Status    string     `gorm:"not null;default:waitlist;index" json:"status"`
	LaunchETA *time.Time `json:"launchEta,omitempty"`
	Latitude  float64    `json:"latitude"`
	Longitude float64    `json:"longitude"`
}

type CityWaitlist struct {
	Base
	CityID     uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:idx_waitlist_city_email,priority:1;uniqueIndex:idx_waitlist_city_phone,priority:1" json:"cityId"`
	Email      *string    `gorm:"uniqueIndex:idx_waitlist_city_email,priority:2" json:"email,omitempty"`
	Phone      *string    `gorm:"uniqueIndex:idx_waitlist_city_phone,priority:2" json:"phone,omitempty"`
	UserID     *uuid.UUID `gorm:"type:uuid;index" json:"userId,omitempty"`
	NotifiedAt *time.Time `json:"notifiedAt,omitempty"`
}

type Venue struct {
	Base
	OwnerID           uuid.UUID      `gorm:"type:uuid;not null;index" json:"ownerId"`
	CityID            uuid.UUID      `gorm:"type:uuid;not null;index" json:"cityId"`
	Name              string         `gorm:"not null" json:"name"`
	Area              string         `gorm:"not null;index" json:"area"`
	Latitude          float64        `gorm:"not null" json:"latitude"`
	Longitude         float64        `gorm:"not null" json:"longitude"`
	Status            string         `gorm:"not null;default:pending;index" json:"status"`
	FeeRateBPS        int            `gorm:"not null;default:1000" json:"feeRateBps"`
	Verified          bool           `gorm:"not null;default:false" json:"verified"`
	Rating            float64        `gorm:"not null;default:0" json:"rating"`
	Amenities         datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"amenities"`
	Rules             datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"rules"`
	PeakMultiplierBPS int            `gorm:"not null;default:10000" json:"peakMultiplierBps"`
	CancelWindowHours int            `gorm:"not null;default:24" json:"cancelWindowHours"`
	AutoConfirm       bool           `gorm:"not null;default:false" json:"autoConfirm"`

	City    City           `gorm:"foreignKey:CityID" json:"city"`
	Pitches []Pitch        `gorm:"foreignKey:VenueID" json:"pitches"`
	Photos  []VenuePhoto   `gorm:"foreignKey:VenueID" json:"photos"`
	Extras  []ExtraCatalog `gorm:"foreignKey:VenueID" json:"extras"`
}

type Pitch struct {
	Base
	VenueID      uuid.UUID      `gorm:"type:uuid;not null;index" json:"venueId"`
	Name         string         `gorm:"not null" json:"name"`
	Format       string         `gorm:"not null;index" json:"format"`
	Surface      string         `gorm:"not null" json:"surface"`
	BasePriceTZS int64          `gorm:"not null" json:"basePriceTzs"`
	PhotoR2Key   string         `json:"photoR2Key"`
	OpenHours    datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"openHours"`
}

type SlotBlock struct {
	Base
	PitchID   uuid.UUID `gorm:"type:uuid;not null;index:idx_slot_blocks_pitch_time" json:"pitchId"`
	StartsAt  time.Time `gorm:"not null;index:idx_slot_blocks_pitch_time" json:"startsAt"`
	EndsAt    time.Time `gorm:"not null" json:"endsAt"`
	Reason    string    `json:"reason"`
	CreatedBy uuid.UUID `gorm:"type:uuid;not null" json:"createdBy"`
}

type VenuePhoto struct {
	Base
	VenueID uuid.UUID `gorm:"type:uuid;not null;index" json:"venueId"`
	R2Key   string    `gorm:"not null" json:"r2Key"`
	Sort    int       `gorm:"not null;default:0" json:"sort"`
	Alt     string    `json:"alt"`
}

type ExtraCatalog struct {
	Base
	VenueID   uuid.UUID `gorm:"type:uuid;not null;index" json:"venueId"`
	Kind      string    `gorm:"not null" json:"kind"`
	Name      string    `gorm:"not null" json:"name"`
	PriceTZS  int64     `gorm:"not null" json:"priceTzs"`
	Unit      string    `gorm:"not null" json:"unit"`
	Stock     int       `gorm:"not null;default:0" json:"stock"`
	Available bool      `gorm:"not null;default:true;index" json:"available"`
}

type Review struct {
	Base
	BookingID  uuid.UUID      `gorm:"type:uuid;not null;uniqueIndex" json:"bookingId"`
	UserID     uuid.UUID      `gorm:"type:uuid;not null;index" json:"userId"`
	VenueID    uuid.UUID      `gorm:"type:uuid;not null;index" json:"venueId"`
	Stars      int            `gorm:"not null" json:"stars"`
	Text       string         `json:"text"`
	Tags       datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"tags"`
	OwnerReply string         `json:"ownerReply"`
	RepliedAt  *time.Time     `json:"repliedAt,omitempty"`
}
