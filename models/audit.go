package models

import "github.com/google/uuid"

// AuditLog records who did what to what — every admin mutation (status
// change, delete) writes one row, so destructive actions are attributable.
type AuditLog struct {
	Base
	ActorUserID uuid.UUID `gorm:"type:uuid;not null;index" json:"actorUserId"`
	ActorEmail  string    `gorm:"not null" json:"actorEmail"`
	Action      string    `gorm:"not null;index" json:"action"`
	TargetKind  string    `gorm:"not null" json:"targetKind"`
	TargetID    string    `gorm:"not null;index" json:"targetId"`
	Detail      string    `json:"detail"`
}
