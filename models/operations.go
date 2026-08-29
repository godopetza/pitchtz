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
	Status       string         `gorm:"not null;default:active;index" json:"status"`
	Scopes       datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"scopes"`
	LastActiveAt *time.Time     `json:"lastActiveAt,omitempty"`
}

type AdminCredential struct {
	UserID             uuid.UUID  `gorm:"type:uuid;primaryKey" json:"userId"`
	PasswordHash       string     `gorm:"not null" json:"-"`
	MustChangePassword bool       `gorm:"not null;default:true" json:"mustChangePassword"`
	PasswordChangedAt  *time.Time `json:"passwordChangedAt,omitempty"`
	FailedLoginCount   int        `gorm:"not null;default:0" json:"-"`
	LockedUntil        *time.Time `json:"-"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

const (
	AdminStatusActive   = "active"
	AdminStatusDisabled = "disabled"

	AdminRoleSuperAdmin = "super_admin"
	AdminRoleOperations = "operations"
	AdminRoleFinance    = "finance"
	AdminRoleTrust      = "trust_safety"
	AdminRoleSupport    = "support"
	AdminRoleMarketing  = "marketing"
	AdminRoleAnalyst    = "analyst"
)

func IsAdminRole(role string) bool {
	switch role {
	case AdminRoleSuperAdmin, AdminRoleOperations, AdminRoleFinance, AdminRoleTrust, AdminRoleSupport, AdminRoleMarketing, AdminRoleAnalyst:
		return true
	default:
		return false
	}
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

// Fixture is a scraped match time shown on the client site — Tanzania's Ligi
// Kuu Bara plus the top five European leagues. No paid API: a scheduled
// scrape fills this table, and a failed scrape emails the superadmin.
type Fixture struct {
	Base
	ExternalID string    `gorm:"not null;uniqueIndex" json:"externalId"`
	Sport      string    `gorm:"not null;default:football;index" json:"sport"`
	League     string    `gorm:"not null;index" json:"league"`
	Country    string    `gorm:"not null;index" json:"country"`
	Home       string    `gorm:"not null" json:"home"`
	Away       string    `gorm:"not null" json:"away"`
	KickoffAt  time.Time `gorm:"not null;index" json:"kickoffAt"`
	Status     string    `gorm:"not null;default:NS" json:"status"`
	HomeScore  string    `json:"homeScore"`
	AwayScore  string    `json:"awayScore"`
	// Timeline: compact goal/card events captured while the match is live —
	// [{"m":24,"p":"Ndoye","s":"0-1","t":"goal"}] — served straight from the
	// list endpoint so clients never need a per-match request for scorers.
	Timeline datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"timeline"`
}

// WatchSpot is a place to WATCH the game — bar, lounge, hall. Publicly
// submitted, superadmin-approved before it appears on the client site.
type WatchSpot struct {
	Base
	Name         string         `gorm:"not null" json:"name"`
	Area         string         `gorm:"not null;index" json:"area"`
	Address      string         `json:"address"`
	Latitude     float64        `json:"latitude"`
	Longitude    float64        `json:"longitude"`
	Screens      int            `gorm:"not null;default:1" json:"screens"`
	Capacity     string         `json:"capacity"`
	EntryTZS     int64          `gorm:"not null;default:0" json:"entryTzs"`
	Features     datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"features"`
	PhotoR2Key   string         `json:"photoR2Key"`
	ContactName  string         `json:"contactName"`
	ContactPhone string         `json:"contactPhone"`
	Status       string         `gorm:"not null;default:pending;index" json:"status"`
}
