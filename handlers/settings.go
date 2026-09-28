package handlers

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ListSettings is public: the login page reads the provider flags before any
// session exists.
func (h *Handlers) ListSettings(c *gin.Context) {
	c.JSON(http.StatusOK, toSettingResponses(h.Settings.All()))
}

// UpdateSetting changes one app setting. Admin only (enforced by the router).
func (h *Handlers) UpdateSetting(c *gin.Context) {
	key := c.Param("key")
	var req UpdateSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorJSON(c, http.StatusBadRequest, err.Error())
		return
	}

	row, err := h.Settings.Set(key, req.Value)
	if err != nil {
		serviceError(c, err, "failed to update setting")
		return
	}

	slog.Info("admin action: setting updated",
		"actor", actorEmail(c), "key", key, "new_value", req.Value)

	c.JSON(http.StatusOK, toSettingResponse(row))
}
