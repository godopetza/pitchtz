package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type Favorite struct {
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey" json:"userId"`
	VenueID   uuid.UUID `gorm:"type:uuid;primaryKey" json:"venueId"`
	CreatedAt time.Time `json:"createdAt"`
}

type Team struct {
	Base
	Name       string     `gorm:"not null" json:"name"`
	Tag        string     `gorm:"not null;index" json:"tag"`
	CaptainID  uuid.UUID  `gorm:"type:uuid;not null;index" json:"captainId"`
	CityID     uuid.UUID  `gorm:"type:uuid;not null;index" json:"cityId"`
	Area       string     `json:"area"`
	Bio        string     `json:"bio"`
	BadgeColor string     `gorm:"not null;default:'#0e3b2c'" json:"badgeColor"`
	Format     string     `gorm:"not null;index" json:"format"`
	Recruiting bool       `gorm:"not null;default:false" json:"recruiting"`
	Needs      string     `json:"needs"`
	LeagueID   *uuid.UUID `gorm:"type:uuid;index" json:"leagueId,omitempty"`
}

// Challenge is an open "who wants a game?" post from a team: any other team
// in the city can accept it, which creates a Match between the two.
type Challenge struct {
	Base
	TeamID           uuid.UUID  `gorm:"type:uuid;not null;index" json:"teamId"`
	CityID           uuid.UUID  `gorm:"type:uuid;not null;index" json:"cityId"`
	Format           string     `gorm:"not null" json:"format"`
	Note             string     `json:"note"`
	ProposedAt       *time.Time `json:"proposedAt,omitempty"`
	Status           string     `gorm:"not null;default:open;index" json:"status"`
	AcceptedByTeamID *uuid.UUID `gorm:"type:uuid" json:"acceptedByTeamId,omitempty"`
	MatchID          *uuid.UUID `gorm:"type:uuid" json:"matchId,omitempty"`
}

type TeamMember struct {
	TeamID   uuid.UUID `gorm:"type:uuid;primaryKey" json:"teamId"`
	UserID   uuid.UUID `gorm:"type:uuid;primaryKey" json:"userId"`
	Role     string    `gorm:"not null;default:player" json:"role"`
	Status   string    `gorm:"not null;default:requested;index" json:"status"`
	JoinedAt time.Time `json:"joinedAt"`
}

type Match struct {
	Base
	HomeTeamID uuid.UUID  `gorm:"type:uuid;not null;index" json:"homeTeamId"`
	AwayTeamID uuid.UUID  `gorm:"type:uuid;not null;index" json:"awayTeamId"`
	BookingID  *uuid.UUID `gorm:"type:uuid;uniqueIndex" json:"bookingId,omitempty"`
	Status     string     `gorm:"not null;default:challenge_sent;index" json:"status"`
	HomeScore  *int       `json:"homeScore,omitempty"`
	AwayScore  *int       `json:"awayScore,omitempty"`
	PlayedAt   *time.Time `json:"playedAt,omitempty"`
}

type League struct {
	Base
	Name   string    `gorm:"not null" json:"name"`
	CityID uuid.UUID `gorm:"type:uuid;not null;index" json:"cityId"`
	Season string    `gorm:"not null" json:"season"`
}

type Standing struct {
	LeagueID uuid.UUID `gorm:"type:uuid;primaryKey" json:"leagueId"`
	TeamID   uuid.UUID `gorm:"type:uuid;primaryKey" json:"teamId"`
	Played   int       `gorm:"not null;default:0" json:"played"`
	Won      int       `gorm:"not null;default:0" json:"won"`
	Drawn    int       `gorm:"not null;default:0" json:"drawn"`
	Lost     int       `gorm:"not null;default:0" json:"lost"`
	Points   int       `gorm:"not null;default:0" json:"points"`
}

type Tournament struct {
	Base
	Name        string         `gorm:"not null" json:"name"`
	Kind        string         `gorm:"not null;index" json:"kind"`
	VenueID     *uuid.UUID     `gorm:"type:uuid;index" json:"venueId,omitempty"`
	EntryFeeTZS int64          `gorm:"not null;default:0" json:"entryFeeTzs"`
	Bracket     datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"bracket"`
	PotTZS      int64          `gorm:"not null;default:0" json:"potTzs"`
	WinnerID    *uuid.UUID     `gorm:"type:uuid" json:"winnerId,omitempty"`
}

type TournamentEntry struct {
	TournamentID uuid.UUID `gorm:"type:uuid;primaryKey" json:"tournamentId"`
	UserID       uuid.UUID `gorm:"type:uuid;primaryKey" json:"userId"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Promotion struct {
	Base
	Code           string `gorm:"not null;uniqueIndex" json:"code"`
	DiscountBPS    int    `gorm:"not null" json:"discountBps"`
	Scope          string `gorm:"not null" json:"scope"`
	Status         string `gorm:"not null;default:draft;index" json:"status"`
	Uses           int    `gorm:"not null;default:0" json:"uses"`
	SpendDrivenTZS int64  `gorm:"not null;default:0" json:"spendDrivenTzs"`
}
