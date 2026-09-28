package handlers

import "github.com/gin-gonic/gin"

// ErrorJSON writes the API's standard error body.
func ErrorJSON(c *gin.Context, status int, msg string) {
	c.JSON(status, ErrorResponse{Error: msg})
}
