package models

// CareerApplication is a public "work with us" submission, reviewed from the
// superadmin portal's Staff section.
type CareerApplication struct {
	Base
	Name         string `gorm:"not null" json:"name"`
	Email        string `gorm:"not null;index" json:"email"`
	Phone        string `json:"phone"`
	RoleInterest string `gorm:"not null" json:"roleInterest"`
	Message      string `json:"message"`
	Status       string `gorm:"not null;default:new;index" json:"status"`
}
