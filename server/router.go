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

		admin := v1.Group("/admin")
		admin.POST("/auth/login", adminLoginLimiter.Middleware(), handlers.AdminLogin)
		protectedAdmin := admin.Group("")
		protectedAdmin.Use(middleware.RequireAdmin())
		protectedAdmin.GET("/auth/me", handlers.AdminMe)
		protectedAdmin.POST("/auth/change-password", handlers.ChangeAdminPassword)
		protectedAdmin.GET("/users", middleware.RequireAdminRoles(models.AdminRoleSuperAdmin), handlers.ListAdmins)
		protectedAdmin.POST("/users", middleware.RequireAdminRoles(models.AdminRoleSuperAdmin), handlers.CreateAdmin)
		venueReviewRoles := middleware.RequireAdminRoles(models.AdminRoleSuperAdmin, models.AdminRoleOperations)
		protectedAdmin.GET("/venues", venueReviewRoles, handlers.ListVenuesForAdmin)
		protectedAdmin.POST("/venues/:id/approve", venueReviewRoles, handlers.ApproveVenue)

		owner := v1.Group("/owner")
		owner.POST("/auth/login", ownerLoginLimiter.Middleware(), handlers.OwnerLogin)
		protectedOwner := owner.Group("")
		protectedOwner.Use(middleware.RequireOwner())
		protectedOwner.GET("/auth/me", handlers.OwnerMe)
		protectedOwner.POST("/auth/change-password", handlers.ChangeOwnerPassword)
		protectedOwner.POST("/bookings", handlers.CreateBooking)
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
