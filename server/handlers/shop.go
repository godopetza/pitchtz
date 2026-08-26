package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ---------- public ----------

// ListShopProducts is the public storefront: active platform-shop products.
// Venue shops will filter by ?venue_id= once owner management ships.
func ListShopProducts(c *gin.Context) {
	if initializers.DB == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "DATABASE_REQUIRED", "the shop is unavailable")
		return
	}
	var products []models.ShopProduct
	initializers.DB.WithContext(c.Request.Context()).
		Where("active = ? AND venue_id IS NULL", true).Order("created_at DESC").Find(&products)
	utils.RespondSuccess(c, http.StatusOK, products, "")
}

// ---------- customer ----------

type shopOrderItemInput struct {
	ProductID uuid.UUID `json:"product_id" binding:"required"`
	Quantity  int       `json:"quantity" binding:"required,min=1,max=50"`
}

type createShopOrderInput struct {
	Items []shopOrderItemInput `json:"items" binding:"required,min=1,max=30,dive"`
}

func shopOrderPayload(c *gin.Context, order models.ShopOrder) gin.H {
	var items []models.ShopOrderItem
	initializers.DB.WithContext(c.Request.Context()).Where("order_id = ?", order.ID).Find(&items)
	return gin.H{
		"id":        order.ID,
		"code":      order.Code,
		"status":    order.Status,
		"total_tzs": order.TotalTZS,
		"paid_at":   order.PaidAt,
		"items":     items,
	}
}

// CreateShopOrder locks in current prices and decrements stock atomically —
// an out-of-stock race fails the whole order rather than overselling.
func CreateShopOrder(c *gin.Context) {
	if initializers.DB == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "DATABASE_REQUIRED", "the shop is unavailable")
		return
	}
	userID, ok := clientUserID(c)
	if !ok {
		return
	}
	var input createShopOrderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "items with product_id and quantity are required")
		return
	}

	order := models.ShopOrder{
		Code:   fmt.Sprintf("PSH-%s", strings.ToUpper(uuid.NewString()[:6])),
		UserID: userID,
		Status: models.ShopOrderStatusPending,
	}
	err := initializers.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		for _, item := range input.Items {
			var product models.ShopProduct
			if err := tx.First(&product, "id = ? AND active = ?", item.ProductID, true).Error; err != nil {
				return fmt.Errorf("PRODUCT_NOT_FOUND")
			}
			decremented := tx.Model(&models.ShopProduct{}).
				Where("id = ? AND stock >= ?", product.ID, item.Quantity).
				UpdateColumn("stock", gorm.Expr("stock - ?", item.Quantity))
			if decremented.Error != nil {
				return decremented.Error
			}
			if decremented.RowsAffected == 0 {
				return fmt.Errorf("OUT_OF_STOCK:%s", product.Name)
			}
			if err := tx.Create(&models.ShopOrderItem{
				OrderID: order.ID, ProductID: product.ID, Quantity: item.Quantity,
				PriceTZSEach: product.PriceTZS, Name: product.Name,
			}).Error; err != nil {
				return err
			}
			order.TotalTZS += product.PriceTZS * int64(item.Quantity)
		}
		return tx.Model(&models.ShopOrder{}).Where("id = ?", order.ID).Update("total_tzs", order.TotalTZS).Error
	})
	if err != nil {
		message := err.Error()
		if strings.HasPrefix(message, "OUT_OF_STOCK:") {
			utils.RespondError(c, http.StatusConflict, "OUT_OF_STOCK", strings.TrimPrefix(message, "OUT_OF_STOCK:")+" is out of stock")
			return
		}
		if message == "PRODUCT_NOT_FOUND" {
			utils.RespondError(c, http.StatusNotFound, "PRODUCT_NOT_FOUND", "one of the products is no longer available")
			return
		}
		utils.RespondError(c, http.StatusInternalServerError, "ORDER_CREATE_FAILED", "could not create the order")
		return
	}
	utils.RespondSuccess(c, http.StatusCreated, shopOrderPayload(c, order), "Order created. Pay to confirm it.")
}

func clientOwnedOrder(c *gin.Context) (models.ShopOrder, bool) {
	userID, ok := clientUserID(c)
	if !ok {
		return models.ShopOrder{}, false
	}
	orderID, ok := parseID(c, "id")
	if !ok {
		return models.ShopOrder{}, false
	}
	var order models.ShopOrder
	if err := initializers.DB.WithContext(c.Request.Context()).First(&order, "id = ?", orderID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "ORDER_NOT_FOUND", "order was not found")
		return models.ShopOrder{}, false
	}
	if order.UserID != userID {
		utils.RespondError(c, http.StatusForbidden, "NOT_YOUR_ORDER", "this order belongs to another account")
		return models.ShopOrder{}, false
	}
	return order, true
}

func GetShopOrder(c *gin.Context) {
	order, ok := clientOwnedOrder(c)
	if !ok {
		return
	}
	utils.RespondSuccess(c, http.StatusOK, shopOrderPayload(c, order), "")
}

// PayShopOrder collects the order total through Malipo, mirroring the booking
// flow: the money is only trusted when the signed callback lands.
func PayShopOrder(c *gin.Context) {
	order, ok := clientOwnedOrder(c)
	if !ok {
		return
	}
	if !requireMalipo(c) {
		return
	}
	if order.Status != models.ShopOrderStatusPending {
		utils.RespondError(c, http.StatusConflict, "ORDER_NOT_PAYABLE", "this order is not awaiting payment")
		return
	}
	var input clientPayInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "provider, phone, and operator are required")
		return
	}
	payShopOrderViaMalipo(c, order, input)
}

// ---------- admin (superadmin only, enforced in router) ----------

type upsertProductInput struct {
	Name        string `json:"name" binding:"required,min=2,max=140"`
	Description string `json:"description" binding:"max=2000"`
	PriceTZS    int64  `json:"price_tzs" binding:"required,gt=0"`
	ImageURL    string `json:"image_url" binding:"max=500"`
	Stock       int    `json:"stock" binding:"min=0"`
	Active      *bool  `json:"active"`
}

func AdminListShopProducts(c *gin.Context) {
	var products []models.ShopProduct
	initializers.DB.WithContext(c.Request.Context()).
		Where("venue_id IS NULL").Order("created_at DESC").Find(&products)
	utils.RespondSuccess(c, http.StatusOK, products, "")
}

func AdminCreateShopProduct(c *gin.Context) {
	var input upsertProductInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "name and price_tzs are required")
		return
	}
	product := models.ShopProduct{
		Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description),
		PriceTZS: input.PriceTZS, ImageURL: strings.TrimSpace(input.ImageURL), Stock: input.Stock, Active: true,
	}
	if input.Active != nil {
		product.Active = *input.Active
	}
	if err := initializers.DB.WithContext(c.Request.Context()).Create(&product).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "PRODUCT_SAVE_FAILED", "could not save the product")
		return
	}
	utils.RespondSuccess(c, http.StatusCreated, product, "Product added to the shop.")
}

func AdminUpdateShopProduct(c *gin.Context) {
	productID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input upsertProductInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "name and price_tzs are required")
		return
	}
	updates := map[string]interface{}{
		"name": strings.TrimSpace(input.Name), "description": strings.TrimSpace(input.Description),
		"price_tzs": input.PriceTZS, "image_url": strings.TrimSpace(input.ImageURL), "stock": input.Stock,
	}
	if input.Active != nil {
		updates["active"] = *input.Active
	}
	result := initializers.DB.WithContext(c.Request.Context()).
		Model(&models.ShopProduct{}).Where("id = ? AND venue_id IS NULL", productID).Updates(updates)
	if result.Error != nil || result.RowsAffected == 0 {
		utils.RespondError(c, http.StatusNotFound, "PRODUCT_NOT_FOUND", "product was not found")
		return
	}
	var product models.ShopProduct
	initializers.DB.WithContext(c.Request.Context()).First(&product, "id = ?", productID)
	utils.RespondSuccess(c, http.StatusOK, product, "Product updated.")
}

func AdminListShopOrders(c *gin.Context) {
	var orders []models.ShopOrder
	initializers.DB.WithContext(c.Request.Context()).Order("created_at DESC").Limit(100).Find(&orders)
	items := make([]gin.H, 0, len(orders))
	for _, order := range orders {
		items = append(items, shopOrderPayload(c, order))
	}
	utils.RespondSuccess(c, http.StatusOK, items, "")
}

func AdminFulfillShopOrder(c *gin.Context) {
	orderID, ok := parseID(c, "id")
	if !ok {
		return
	}
	now := time.Now().UTC()
	result := initializers.DB.WithContext(c.Request.Context()).Model(&models.ShopOrder{}).
		Where("id = ? AND status = ?", orderID, models.ShopOrderStatusPaid).
		Updates(map[string]interface{}{"status": models.ShopOrderStatusFulfilled, "fulfilled_at": now})
	if result.Error != nil || result.RowsAffected == 0 {
		utils.RespondError(c, http.StatusConflict, "ORDER_NOT_FULFILLABLE", "only paid orders can be fulfilled")
		return
	}
	utils.RespondSuccess(c, http.StatusOK, gin.H{"fulfilled": true}, "Order marked fulfilled.")
}
