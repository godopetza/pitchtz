package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/utils"
)

// Venue-run shops: owners sell their own merchandise and consumables from
// their venue page. The platform shop (venue_id NULL) stays superadmin-only.

type ownerProductInput struct {
	Name        string `json:"name" binding:"required,min=2,max=120"`
	Description string `json:"description" binding:"max=500"`
	PriceTZS    int64  `json:"price_tzs" binding:"required,gt=0"`
	ImageURL    string `json:"image_url" binding:"max=500"`
	Stock       int    `json:"stock" binding:"gte=0"`
	Active      *bool  `json:"active"`
}

func OwnerListProducts(c *gin.Context) {
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	venueID, ok := parseID(c, "id")
	if !ok {
		return
	}
	if !ownerOwnsVenue(c, ownerID, venueID) {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_VENUE", "this venue is not on your account")
		return
	}
	var products []models.ShopProduct
	initializers.DB.WithContext(c.Request.Context()).
		Where("venue_id = ?", venueID).Order("created_at DESC").Find(&products)
	utils.RespondSuccess(c, http.StatusOK, products, "")
}

func OwnerCreateProduct(c *gin.Context) {
	ownerID, ok := ownerUserID(c)
	if !ok {
		return
	}
	venueID, ok := parseID(c, "id")
	if !ok {
		return
	}
	if !ownerOwnsVenue(c, ownerID, venueID) {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_VENUE", "this venue is not on your account")
		return
	}
	var input ownerProductInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "name and a positive price_tzs are required")
		return
	}
	active := true
	if input.Active != nil {
		active = *input.Active
	}
	product := models.ShopProduct{
		VenueID: &venueID, Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description),
		PriceTZS: input.PriceTZS, ImageURL: strings.TrimSpace(input.ImageURL), Stock: input.Stock, Active: active,
	}
	if err := initializers.DB.WithContext(c.Request.Context()).Create(&product).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "PRODUCT_CREATE_FAILED", "could not create the product")
		return
	}
	utils.RespondSuccess(c, http.StatusCreated, product, "Product added to your venue shop.")
}

func ownerOwnedProduct(c *gin.Context) (*models.ShopProduct, bool) {
	ownerID, ok := ownerUserID(c)
	if !ok {
		return nil, false
	}
	productID, ok := parseID(c, "id")
	if !ok {
		return nil, false
	}
	var product models.ShopProduct
	if err := initializers.DB.WithContext(c.Request.Context()).First(&product, "id = ?", productID).Error; err != nil || product.VenueID == nil {
		utils.RespondError(c, http.StatusNotFound, "PRODUCT_NOT_FOUND", "product was not found")
		return nil, false
	}
	if !ownerOwnsVenue(c, ownerID, *product.VenueID) {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_VENUE", "this product is not on your account")
		return nil, false
	}
	return &product, true
}

func OwnerUpdateProduct(c *gin.Context) {
	product, ok := ownerOwnedProduct(c)
	if !ok {
		return
	}
	var input ownerProductInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "name and a positive price_tzs are required")
		return
	}
	updates := map[string]any{
		"name": strings.TrimSpace(input.Name), "description": strings.TrimSpace(input.Description),
		"price_tzs": input.PriceTZS, "stock": input.Stock,
	}
	if strings.TrimSpace(input.ImageURL) != "" {
		updates["image_url"] = strings.TrimSpace(input.ImageURL)
	}
	if input.Active != nil {
		updates["active"] = *input.Active
	}
	initializers.DB.WithContext(c.Request.Context()).Model(&models.ShopProduct{}).
		Where("id = ?", product.ID).Updates(updates)
	utils.RespondSuccess(c, http.StatusOK, gin.H{"updated": true}, "Product updated.")
}

func OwnerDeleteProduct(c *gin.Context) {
	product, ok := ownerOwnedProduct(c)
	if !ok {
		return
	}
	initializers.DB.WithContext(c.Request.Context()).Delete(&models.ShopProduct{}, "id = ?", product.ID)
	utils.RespondSuccess(c, http.StatusOK, gin.H{"deleted": true}, "Product removed.")
}

// ListVenueProducts is the public storefront on a venue page.
func ListVenueProducts(c *gin.Context) {
	venueID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var products []models.ShopProduct
	initializers.DB.WithContext(c.Request.Context()).
		Where("venue_id = ? AND active = ?", venueID, true).Order("created_at DESC").Find(&products)
	utils.RespondSuccess(c, http.StatusOK, products, "")
}
