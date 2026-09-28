package handlers

import (
	"net/http"
	"time"

	"tablescore-api/middleware"
	"tablescore-api/ws"

	"github.com/gin-gonic/gin"
)

// NewRouter builds the fully wired Gin engine: global middleware, every REST
// route, the WebSocket upgrade and the embedded SPA fallback. It is the one
// place the route table lives, so tests exercise the same wiring as main.
//
// gin.SetMode is process-global and stays in main (or the test).
func Mount(r *gin.Engine, d Deps) {
	r.GET("/verify-email", AuthLinkPage(false))
	r.GET("/reset-password", AuthLinkPage(true))
	h := New(d)
	limitBody := func(c *gin.Context) { c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10); c.Next() }

	// Auth routes (public) — rate limited
	authGroup := r.Group("/api/auth")
	authGroup.Use(limitBody)
	authGroup.Use(middleware.RateLimit(20, 1*time.Minute)) // 20 requests/min per IP
	{
		authGroup.POST("/register", h.Register)
		authGroup.POST("/login", h.Login)
		authGroup.POST("/verify-email", h.VerifyEmail)
		authGroup.POST("/forgot-password", h.ForgotPassword)
		authGroup.POST("/reset-password", h.ResetPassword)
		authGroup.GET("/:provider", h.OAuthBegin)
		authGroup.GET("/:provider/callback", h.OAuthCallback)
		authGroup.POST("/refresh", h.RefreshToken)
		authGroup.POST("/logout", h.Logout)
	}

	// Protected auth routes
	protected := r.Group("/api/auth")
	protected.Use(middleware.RequireAuth(d.Cfg.Auth.JWTSecret))
	{
		protected.GET("/me", h.GetCurrentUser)
		// Route-level limiter, not on the whole group: /me must stay
		// unthrottled, but resend already has a 60s per-user cooldown and
		// this adds a per-IP ceiling on top of it.
		protected.POST("/resend-verification", middleware.RateLimit(5, 1*time.Minute), h.ResendVerification)
	}

	// Public settings endpoint (needed before login, no auth required)
	r.GET("/api/settings", h.ListSettings)

	// Admin routes (requires auth + admin role)
	admin := r.Group("/api/admin")
	admin.Use(limitBody)
	admin.Use(middleware.RequireAuth(d.Cfg.Auth.JWTSecret))
	admin.Use(middleware.RequireAdmin(d.DB))
	{
		admin.GET("/users", h.ListUsers)
		admin.POST("/users", h.CreateUser)
		admin.PATCH("/users/:id", h.UpdateUser)
		admin.PATCH("/users/:id/role", h.UpdateUserRole)
		admin.DELETE("/users/:id", h.DeleteUser)
		admin.GET("/ws/connections", h.ListWSConnections)
		admin.PUT("/settings/:key", h.UpdateSetting)
	}

	// WebSocket upgrade — in prod only base_url's own origin may open a socket;
	// in dev the allowlist is empty, which accepts any origin.
	wsHandlers := &ws.Handlers{Hub: d.Hub, JWTSecret: d.Cfg.Auth.JWTSecret, AllowedOrigins: d.Cfg.WSOrigins()}
	r.GET("/ws", wsHandlers.Upgrade)

}
