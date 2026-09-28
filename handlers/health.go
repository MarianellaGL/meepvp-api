package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HealthCheck returns a simple health status response.
func HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, HealthResponse{Status: "ok"})
}
