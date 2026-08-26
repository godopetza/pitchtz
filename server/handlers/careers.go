package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/utils"
)

type careerApplicationInput struct {
	Name         string `json:"name" binding:"required,min=2,max=120"`
	Email        string `json:"email" binding:"required,email,max=254"`
	Phone        string `json:"phone" binding:"max=32"`
	RoleInterest string `json:"role_interest" binding:"required,min=2,max=120"`
	Message      string `json:"message" binding:"max=3000"`
}

// SubmitCareerApplication is the public hiring form behind the "We're hiring"
// house ad.
func SubmitCareerApplication(c *gin.Context) {
	if initializers.DB == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "DATABASE_REQUIRED", "applications are unavailable")
		return
	}
	var input careerApplicationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "name, email, and role_interest are required")
		return
	}
	application := models.CareerApplication{
		Name:         strings.TrimSpace(input.Name),
		Email:        strings.ToLower(strings.TrimSpace(input.Email)),
		Phone:        strings.TrimSpace(input.Phone),
		RoleInterest: strings.TrimSpace(input.RoleInterest),
		Message:      strings.TrimSpace(input.Message),
		Status:       "new",
	}
	if err := initializers.DB.WithContext(c.Request.Context()).Create(&application).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "APPLICATION_FAILED", "could not submit the application")
		return
	}
	utils.RespondSuccess(c, http.StatusCreated, gin.H{"id": application.ID}, "Asante! We'll be in touch.")
}

func AdminListCareerApplications(c *gin.Context) {
	var applications []models.CareerApplication
	initializers.DB.WithContext(c.Request.Context()).Order("created_at DESC").Limit(200).Find(&applications)
	utils.RespondSuccess(c, http.StatusOK, applications, "")
}
