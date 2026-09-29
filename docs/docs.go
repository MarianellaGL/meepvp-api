// Package docs embeds the public API contract and its interactive explorer.
package docs

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed openapi.json
var specification []byte

//go:embed index.html
var page []byte

// Mount serves docs from the binary, including in the production Docker image.
func Mount(r *gin.Engine) {
	spec := func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Data(http.StatusOK, "application/json; charset=utf-8", specification)
	}
	ui := func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Data(http.StatusOK, "text/html; charset=utf-8", page)
	}
	r.GET("/openapi.json", spec)
	r.GET("/docs", ui)
	r.GET("/docs/", ui)
}
