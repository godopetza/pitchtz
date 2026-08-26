package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/utils"
)

type reviewReplyInput struct {
	Text string `json:"text" binding:"required,min=2,max=1000"`
}

// OwnerReplyToReview lets the venue owner answer a player review publicly.
func OwnerReplyToReview(c *gin.Context) {
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	reviewID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input reviewReplyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "text is required (2-1000 characters)")
		return
	}
	var review models.Review
	if err := initializers.DB.WithContext(c.Request.Context()).First(&review, "id = ?", reviewID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "REVIEW_NOT_FOUND", "review was not found")
		return
	}
	var venue models.Venue
	if err := initializers.DB.WithContext(c.Request.Context()).First(&venue, "id = ?", review.VenueID).Error; err != nil || venue.OwnerID != ownerID {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_VENUE", "this review is not for your venue")
		return
	}
	now := time.Now().UTC()
	initializers.DB.WithContext(c.Request.Context()).Model(&models.Review{}).Where("id = ?", review.ID).
		Updates(map[string]interface{}{"owner_reply": strings.TrimSpace(input.Text), "replied_at": now})
	utils.RespondSuccess(c, http.StatusOK, gin.H{"replied": true}, "Reply posted.")
}

// OwnerListVenues returns every venue on the owner's account, any status —
// the dashboard onboarding state needs to show pending applications too.
func OwnerListVenues(c *gin.Context) {
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	var venues []models.Venue
	initializers.DB.WithContext(c.Request.Context()).
		Select("id, name, area, status, created_at").
		Where("owner_id = ?", ownerID).Order("created_at ASC").Find(&venues)
	items := make([]gin.H, 0, len(venues))
	for _, venue := range venues {
		items = append(items, gin.H{"id": venue.ID, "name": venue.Name, "area": venue.Area, "status": venue.Status})
	}
	utils.RespondSuccess(c, http.StatusOK, items, "")
}
