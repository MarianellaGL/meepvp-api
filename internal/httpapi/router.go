package httpapi

import (
	"net/http"
	"tablescore-api/docs"
	"tablescore-api/handlers"
	"time"

	"github.com/gin-gonic/gin"
)

// Handler wires the API while preserving its net/http handler boundary.
func (a *API) Handler() http.Handler {
	r := gin.New()
	if a.foundation != nil && len(a.foundation.Cfg.Server.TrustedProxies) > 0 {
		_ = r.SetTrustedProxies(a.foundation.Cfg.Server.TrustedProxies)
	}
	// Gin's default trusts all proxies, matching the deployment's proxy setup.
	r.Use(requestID(), requestLogger(), recovery(), cors())
	docs.Mount(r)
	r.NoRoute(func(c *gin.Context) { writeError(c.Writer, http.StatusNotFound, "route not found") })
	r.GET("/health", func(c *gin.Context) {
		writeJSON(c.Writer, http.StatusOK, map[string]string{"status": "ok"})
	})

	v1 := r.Group("/v1")
	v1.GET("/rulebooks", rateLimit(20, time.Minute, time.Now), a.searchRulebooks)
	v1.POST("/rulebooks/:id/extract", rateLimit(3, time.Minute, time.Now), a.extractRulebook)
	auth := v1.Group("/auth")
	limited := rateLimit(20, time.Minute, time.Now)
	auth.POST("/signup", limited, gin.WrapF(a.signUp))
	auth.POST("/login", limited, gin.WrapF(a.logIn))
	auth.POST("/logout", gin.WrapF(a.logOut))
	v1.GET("/me", gin.WrapF(a.getMe))
	v1.GET("/me/sessions", gin.WrapF(a.getMySessions))
	v1.GET("/me/tables", gin.WrapF(a.getMyTables))
	v1.GET("/me/avatar", gin.WrapF(a.getMyAvatar))
	v1.PUT("/me/avatar", gin.WrapF(a.saveMyAvatar))
	v1.DELETE("/me/avatar", gin.WrapF(a.deleteMyAvatar))
	v1.GET("/me/stats", gin.WrapF(a.getMyStats))
	v1.POST("/me/claim-session", gin.WrapF(a.claimSession))
	v1.POST("/me/claim-table", gin.WrapF(a.claimTable))

	v1.POST("/tables", gin.WrapF(a.createTable))
	v1.GET("/tables/:code/current-session", gin.WrapF(a.currentTableSession))
	v1.POST("/tables/:code/sessions", gin.WrapF(a.createSession))
	v1.GET("/tables/:code/scheduled-games", gin.WrapF(a.scheduledGames))
	v1.POST("/tables/:code/scheduled-games", gin.WrapF(a.scheduledGames))
	v1.PATCH("/scheduled-games/:id/rule", gin.WrapF(a.setScheduledGameRule))
	v1.PATCH("/scheduled-games/:id/session", gin.WrapF(a.setScheduledGameSession))
	v1.PUT("/scheduled-games/:id", gin.WrapF(a.updateScheduledGame))
	v1.DELETE("/scheduled-games/:id", gin.WrapF(a.deleteScheduledGame))

	v1.POST("/scoring-rules", gin.WrapF(a.createRule))
	v1.GET("/scoring-rules", func(c *gin.Context) { a.listRules(c.Writer) })
	v1.GET("/community/scoring-rules", gin.WrapF(a.searchPublicRules))
	v1.POST("/scoring-rules/:id/pdf-imports", gin.WrapF(a.createPDFImport))
	v1.GET("/pdf-imports/:id", gin.WrapF(a.getPDFImport))
	v1.GET("/bgg/collections/:username", gin.WrapF(a.getBGGCollection))
	v1.GET("/bgg/search", rateLimit(15, time.Minute, time.Now), gin.WrapF(a.searchBGG))
	v1.GET("/bgg/games/:id/rules", gin.WrapF(a.getBGGRules))
	v1.POST("/pdf/extract", rateLimit(5, time.Minute, time.Now), gin.WrapF(a.extractPDF))
	v1.POST("/ocr/scoring-text", rateLimit(10, time.Minute, time.Now), gin.WrapF(a.extractScoringText))

	sessions := v1.Group("/sessions/:id")
	sessions.GET("", gin.WrapF(a.getSession))
	sessions.GET("/board-photo", gin.WrapF(a.getBoardPhoto))
	sessions.POST("/board-photo", gin.WrapF(a.saveBoardPhoto))
	sessions.POST("/players", gin.WrapF(a.addPlayer))
	sessions.PUT("/scores", gin.WrapF(a.updateScores))
	sessions.PATCH("/scores", gin.WrapF(a.setScore))
	sessions.POST("/points", gin.WrapF(a.adjustPoints))
	sessions.POST("/finish", gin.WrapF(a.finishSession))
	sessions.POST("/pause", gin.WrapF(a.pauseSession))
	sessions.POST("/resume", gin.WrapF(a.resumeSession))
	sessions.POST("/reopen", gin.WrapF(a.reopenSession))
	if a.foundation != nil {
		handlers.Mount(r, *a.foundation)
	}
	return r
}
