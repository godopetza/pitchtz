package server

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/server/handlers"
	"github.com/godopetza/pitchtz/server/middleware"
	"github.com/godopetza/pitchtz/store"
	"github.com/godopetza/pitchtz/utils"
)

type Deps struct {
	Catalog  store.CatalogStore
	Waitlist store.WaitlistStore
}

func NewRouter() *gin.Engine {
	return NewRouterWithDeps(Deps{})
}

func NewRouterWithDeps(deps Deps) *gin.Engine {
	if deps.Catalog == nil || deps.Waitlist == nil {
		var fallback interface {
			store.CatalogStore
			store.WaitlistStore
		}
		if initializers.DB != nil {
			fallback = store.NewGormStore(initializers.DB)
		} else {
			fallback = store.NewMemoryStore()
		}
		if deps.Catalog == nil {
			deps.Catalog = fallback
		}
		if deps.Waitlist == nil {
			deps.Waitlist = fallback
		}
	}

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	configureTrustedProxies(router)
	router.Use(cors.New(corsConfig()))

	publicAPI := &handlers.PublicAPI{Catalog: deps.Catalog, Waitlist: deps.Waitlist}
	waitlistLimiter := middleware.NewIPRateLimiter(
		utils.EnvInt("WAITLIST_RATE_LIMIT_PER_MIN", 5),
		utils.EnvInt("WAITLIST_RATE_LIMIT_BURST", 5),
	)
	adminLoginLimiter := middleware.NewIPRateLimiter(
		utils.EnvInt("ADMIN_LOGIN_RATE_LIMIT_PER_MIN", 10),
		utils.EnvInt("ADMIN_LOGIN_RATE_LIMIT_BURST", 10),
	)
	ownerLoginLimiter := middleware.NewIPRateLimiter(
		utils.EnvInt("OWNER_LOGIN_RATE_LIMIT_PER_MIN", 10),
		utils.EnvInt("OWNER_LOGIN_RATE_LIMIT_BURST", 10),
	)
	enrollLimiter := middleware.NewIPRateLimiter(
		utils.EnvInt("ENROLL_RATE_LIMIT_PER_MIN", 5),
		utils.EnvInt("ENROLL_RATE_LIMIT_BURST", 5),
	)
	clientAuthLimiter := middleware.NewIPRateLimiter(
		utils.EnvInt("CLIENT_AUTH_RATE_LIMIT_PER_MIN", 10),
		utils.EnvInt("CLIENT_AUTH_RATE_LIMIT_BURST", 10),
	)

	router.GET("/health", handlers.Health)
	router.GET("/docs", SwaggerUI)
	router.GET("/docs/", SwaggerUI)
	router.GET("/openapi.yaml", OpenAPISpec)
	v1 := router.Group("/v1")
	{
		v1.GET("/cities", publicAPI.ListCities)
		v1.GET("/venues", publicAPI.ListVenues)
		v1.GET("/venues/:id", publicAPI.GetVenue)
		v1.GET("/venues/:id/availability", publicAPI.GetVenueAvailability)
		v1.GET("/venues/:id/reviews", publicAPI.ListVenueReviews)
		v1.GET("/venues/:id/extras", publicAPI.ListVenueExtras)
		v1.POST("/waitlist", waitlistLimiter.Middleware(), publicAPI.JoinWaitlist)
		v1.POST("/venues/enroll", enrollLimiter.Middleware(), publicAPI.EnrollVenue)
		v1.POST("/careers", enrollLimiter.Middleware(), handlers.SubmitCareerApplication)
		// Public by necessity; authenticated by the Malipo HMAC signature.
		v1.POST("/payments/callback", handlers.MalipoPaymentCallback)
		v1.GET("/auth/google/callback", handlers.GoogleCallback)
		v1.POST("/auth/apple/callback", handlers.AppleCallback)
		v1.GET("/auth/google/start", handlers.ClientGoogleStart)
		v1.GET("/auth/apple/start", handlers.ClientAppleStart)
		v1.POST("/auth/email/start", clientAuthLimiter.Middleware(), handlers.ClientEmailStart)
		v1.POST("/auth/email/verify", clientAuthLimiter.Middleware(), handlers.ClientEmailVerify)
		clientAuthed := v1.Group("/auth")
		clientAuthed.Use(middleware.RequireClient())
		clientAuthed.GET("/me", handlers.ClientMe)
		clientAuthed.POST("/refresh", handlers.RefreshClientToken)
		v1.POST("/venues/:id/reviews", middleware.RequireClient(), handlers.CreateVenueReview)

		// Self-service bookings + payments (full, split, and QR pay links).
		v1.POST("/bookings", middleware.RequireClient(), handlers.ClientCreateBooking)
		v1.GET("/bookings", middleware.RequireClient(), handlers.ClientListBookings)
		v1.GET("/bookings/:id", middleware.RequireClient(), handlers.ClientGetBooking)
		v1.POST("/bookings/:id/pay", middleware.RequireClient(), handlers.ClientPayBooking)
		v1.POST("/bookings/:id/split", middleware.RequireClient(), handlers.ClientSplitBooking)
		// Public pay-a-share endpoints: the unguessable share id is the capability.
		v1.GET("/pay/shares/:id", handlers.GetPublicShare)
		v1.POST("/pay/shares/:id/pay", clientAuthLimiter.Middleware(), handlers.PayPublicShare)

		// Teams: explore is public (personalised when signed in); actions need auth.
		v1.GET("/teams", middleware.OptionalClient(), handlers.ListTeams)
		v1.GET("/teams/:id", middleware.OptionalClient(), handlers.GetTeam)
		v1.GET("/challenges", handlers.ListChallenges)
		v1.GET("/fixtures", middleware.OptionalClient(), handlers.ListFixtures)
		v1.GET("/me/favorite-teams", middleware.RequireClient(), handlers.ListFavoriteTeams)
		v1.PUT("/me/favorite-teams", middleware.RequireClient(), handlers.SetFavoriteTeams)
		v1.POST("/teams", middleware.RequireClient(), handlers.CreateTeam)
		v1.GET("/me/teams", middleware.RequireClient(), handlers.MyTeams)
		v1.POST("/teams/:id/join", middleware.RequireClient(), handlers.RequestJoinTeam)
		v1.POST("/teams/:id/decide", middleware.RequireClient(), handlers.DecideJoinRequest)
		v1.POST("/teams/:id/challenges", middleware.RequireClient(), handlers.CreateChallenge)
		v1.POST("/challenges/:id/accept", middleware.RequireClient(), handlers.AcceptChallenge)

		// Shop: public storefront + customer orders.
		v1.GET("/shop/products", handlers.ListShopProducts)
		v1.POST("/shop/orders", middleware.RequireClient(), handlers.CreateShopOrder)
		v1.GET("/shop/orders/:id", middleware.RequireClient(), handlers.GetShopOrder)
		v1.POST("/shop/orders/:id/pay", middleware.RequireClient(), handlers.PayShopOrder)

		admin := v1.Group("/admin")
		admin.POST("/auth/login", adminLoginLimiter.Middleware(), handlers.AdminLogin)
		admin.POST("/auth/forgot-password", adminLoginLimiter.Middleware(), handlers.AdminForgotPassword)
		admin.POST("/auth/reset-password", adminLoginLimiter.Middleware(), handlers.AdminResetPassword)
		admin.GET("/auth/google/start", handlers.AdminGoogleStart)
		admin.GET("/auth/apple/start", handlers.AdminAppleStart)
		protectedAdmin := admin.Group("")
		protectedAdmin.Use(middleware.RequireAdmin())
		protectedAdmin.GET("/auth/me", handlers.AdminMe)
		protectedAdmin.POST("/auth/refresh", handlers.RefreshAdminToken)
		protectedAdmin.POST("/auth/change-password", handlers.ChangeAdminPassword)
		protectedAdmin.GET("/users", middleware.RequireAdminRoles(models.AdminRoleSuperAdmin), handlers.ListAdmins)
		protectedAdmin.GET("/careers", middleware.RequireAdminRoles(models.AdminRoleSuperAdmin, models.AdminRoleOperations), handlers.AdminListCareerApplications)
		protectedAdmin.POST("/users", middleware.RequireAdminRoles(models.AdminRoleSuperAdmin), handlers.CreateAdmin)
		venueReviewRoles := middleware.RequireAdminRoles(models.AdminRoleSuperAdmin, models.AdminRoleOperations)
		protectedAdmin.GET("/venues", venueReviewRoles, handlers.ListVenuesForAdmin)
		protectedAdmin.POST("/venues/:id/approve", venueReviewRoles, handlers.ApproveVenue)
		protectedAdmin.POST("/venues", venueReviewRoles, handlers.AdminCreateVenue)
		protectedAdmin.PATCH("/venues/:id/status", venueReviewRoles, handlers.AdminSetVenueStatus)
		protectedAdmin.PATCH("/pitches/:id/status", venueReviewRoles, handlers.AdminSetPitchStatus)
		protectedAdmin.DELETE("/venues/:id", middleware.RequireAdminRoles(models.AdminRoleSuperAdmin), handlers.AdminDeleteVenue)
		protectedAdmin.GET("/stats", handlers.AdminPlatformStats)
		protectedAdmin.GET("/notifications", handlers.AdminNotifications)
		protectedAdmin.GET("/platform-users", venueReviewRoles, handlers.AdminListPlatformUsers)
		protectedAdmin.GET("/bookings", handlers.AdminListBookings)
		protectedAdmin.GET("/disputes", handlers.AdminListDisputes)
		protectedAdmin.GET("/audit", middleware.RequireAdminRoles(models.AdminRoleSuperAdmin), handlers.AdminListAudit)
		shopAdmin := middleware.RequireAdminRoles(models.AdminRoleSuperAdmin)
		protectedAdmin.GET("/shop/products", shopAdmin, handlers.AdminListShopProducts)
		protectedAdmin.POST("/shop/products", shopAdmin, handlers.AdminCreateShopProduct)
		protectedAdmin.PATCH("/shop/products/:id", shopAdmin, handlers.AdminUpdateShopProduct)
		protectedAdmin.GET("/shop/orders", shopAdmin, handlers.AdminListShopOrders)
		protectedAdmin.POST("/shop/orders/:id/fulfill", shopAdmin, handlers.AdminFulfillShopOrder)

		owner := v1.Group("/owner")
		owner.POST("/auth/login", ownerLoginLimiter.Middleware(), handlers.OwnerLogin)
		owner.POST("/auth/forgot-password", ownerLoginLimiter.Middleware(), handlers.OwnerForgotPassword)
		owner.POST("/auth/reset-password", ownerLoginLimiter.Middleware(), handlers.OwnerResetPassword)
		owner.GET("/auth/google/start", handlers.OwnerGoogleStart)
		owner.GET("/auth/apple/start", handlers.OwnerAppleStart)
		protectedOwner := owner.Group("")
		protectedOwner.Use(middleware.RequireOwner())
		protectedOwner.GET("/auth/me", handlers.OwnerMe)
		protectedOwner.POST("/auth/refresh", handlers.RefreshOwnerToken)
		protectedOwner.POST("/auth/change-password", handlers.ChangeOwnerPassword)
		protectedOwner.POST("/bookings", handlers.CreateBooking)
		protectedOwner.POST("/bookings/:id/pay", handlers.RequestBookingPayment)
		protectedOwner.POST("/reviews/:id/reply", handlers.OwnerReplyToReview)
		protectedOwner.GET("/venues", handlers.OwnerListVenues)
		protectedOwner.POST("/venues/:id/pitches", handlers.OwnerCreatePitch)
		protectedOwner.PATCH("/pitches/:id", handlers.OwnerUpdatePitch)
		protectedOwner.GET("/venues/:id/bookings", handlers.OwnerVenueBookings)
		protectedOwner.GET("/venues/:id/payouts", handlers.OwnerVenuePayouts)
		protectedOwner.POST("/venues/:id/photos", handlers.OwnerAddVenuePhoto)
		protectedOwner.PATCH("/venues/:id/photos/order", handlers.OwnerReorderVenuePhotos)
		protectedOwner.PATCH("/venues/:id/photos/alt", handlers.OwnerSetVenuePhotoAlt)
		protectedOwner.PATCH("/venues/:id/hours", handlers.OwnerSetVenueHours)
		protectedOwner.DELETE("/venues/:id/photos", handlers.OwnerDeleteVenuePhoto)
		protectedOwner.GET("/venues/:id/extras", handlers.OwnerListExtras)
		protectedOwner.POST("/venues/:id/extras", handlers.OwnerCreateExtra)
		protectedOwner.PATCH("/extras/:id", handlers.OwnerUpdateExtra)
		protectedOwner.DELETE("/extras/:id", handlers.OwnerDeleteExtra)
	}

	router.NoRoute(func(c *gin.Context) {
		utils.RespondError(c, http.StatusNotFound, "NOT_FOUND", "route not found")
	})
	return router
}

func corsConfig() cors.Config {
	config := cors.DefaultConfig()
	allowed := make(map[string]struct{})
	allowAll := false
	for _, value := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		origin := strings.TrimRight(strings.TrimSpace(value), "/")
		if origin == "*" {
			allowAll = true
		}
		if origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	config.AllowOriginFunc = func(origin string) bool {
		origin = strings.TrimRight(origin, "/")
		if allowAll {
			return true
		}
		if _, ok := allowed[origin]; ok {
			return true
		}
		return strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:")
	}
	config.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"Origin", "Content-Type", "Authorization", "Accept", "Idempotency-Key"}
	config.ExposeHeaders = []string{"X-Request-ID"}
	config.AllowCredentials = false
	config.MaxAge = 12 * time.Hour
	return config
}

func configureTrustedProxies(router *gin.Engine) {
	value := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	if value == "" {
		_ = router.SetTrustedProxies(nil)
		return
	}
	proxies := make([]string, 0)
	for _, proxy := range strings.Split(value, ",") {
		if proxy = strings.TrimSpace(proxy); proxy != "" {
			proxies = append(proxies, proxy)
		}
	}
	_ = router.SetTrustedProxies(proxies)
}
