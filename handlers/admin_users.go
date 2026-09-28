package handlers

import (
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"tablescore-api/auth"
	"tablescore-api/middleware"
	"tablescore-api/services"

	"github.com/gin-gonic/gin"
)

// isSelf reports whether target is the authenticated caller. It fails closed:
// a request that somehow has no Identity counts as self, so the guards that
// call this block the action rather than waving it through.
func isSelf(c *gin.Context, target string) bool {
	id, ok := middleware.IdentityFrom(c)
	if !ok {
		return true
	}
	return id.UserID == target
}

// actorEmail is the authenticated caller's email, for admin audit log lines.
func actorEmail(c *gin.Context) string {
	id, _ := middleware.IdentityFrom(c)
	return id.Email
}

func (h *Handlers) ListWSConnections(c *gin.Context) {
	clients := h.Hub.ConnectedClients()
	c.JSON(http.StatusOK, clients)
}

func (h *Handlers) ListUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	search := c.Query("search")

	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	users, total, err := services.ListUsers(h.DB, page, perPage, search)
	if err != nil {
		ErrorJSON(c, http.StatusInternalServerError, "failed to fetch users")
		return
	}

	c.JSON(http.StatusOK, PaginatedUsersResponse{
		Data:       toUserResponses(users),
		Total:      total,
		Page:       page,
		PerPage:    perPage,
		TotalPages: int(math.Ceil(float64(total) / float64(perPage))),
	})
}

func (h *Handlers) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorJSON(c, http.StatusBadRequest, err.Error())
		return
	}

	if msg := auth.ValidatePasswordComplexity(req.Password); msg != "" {
		ErrorJSON(c, http.StatusBadRequest, msg)
		return
	}

	user, err := services.CreateEmailUser(h.DB, req.Email, req.Name, req.Password)
	if err != nil {
		ErrorJSON(c, http.StatusConflict, "user with this email already exists")
		return
	}

	if req.Role == auth.RoleAdmin {
		if _, err := services.SetUserRole(h.DB, user.ID, auth.RoleAdmin); err != nil {
			ErrorJSON(c, http.StatusInternalServerError, "failed to set role")
			return
		}
		user.Role = auth.RoleAdmin
	}

	actor := actorEmail(c)
	slog.Info("admin action: user created",
		"actor", actor, "target_email", req.Email, "target_role", req.Role)

	c.JSON(http.StatusCreated, toUserResponse(user))
}

func (h *Handlers) UpdateUser(c *gin.Context) {
	id, err := auth.ParseUserID(c.Param("id"))
	if err != nil {
		ErrorJSON(c, http.StatusBadRequest, "invalid user id")
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorJSON(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := services.UpdateUserProfile(h.DB, id, req.Email, req.Name); err != nil {
		serviceError(c, err, "failed to update user")
		return
	}

	// Log only the fields that actually changed — an empty request field leaves
	// the column alone, so logging it would claim an update that never happened.
	actor := actorEmail(c)
	attrs := []any{"actor", actor, "target_id", id}
	if req.Email != "" {
		attrs = append(attrs, "new_email", services.NormalizeEmail(req.Email))
	}
	if req.Name != "" {
		attrs = append(attrs, "new_name", req.Name)
	}
	slog.Info("admin action: user updated", attrs...)

	c.JSON(http.StatusOK, MessageResponse{Message: "user updated"})
}

func (h *Handlers) UpdateUserRole(c *gin.Context) {
	id, err := auth.ParseUserID(c.Param("id"))
	if err != nil {
		ErrorJSON(c, http.StatusBadRequest, "invalid user id")
		return
	}

	if isSelf(c, id) {
		ErrorJSON(c, http.StatusBadRequest, "cannot change your own role")
		return
	}

	var req UpdateUserRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorJSON(c, http.StatusBadRequest, err.Error())
		return
	}

	changed, err := services.SetUserRole(h.DB, id, req.Role)
	if err != nil {
		serviceError(c, err, "failed to update role")
		return
	}

	// The role is baked into short-lived access tokens, so drop every refresh
	// token to force the user back through login with the new role. A no-op
	// write leaves existing tokens valid — they already carry this role.
	if changed {
		if err := services.RevokeAllUserRefreshTokens(h.DB, id); err != nil {
			slog.Warn("failed to revoke refresh tokens after role change", "target_id", id, "error", err)
		}
		// A live socket carries the role it was upgraded with, so close it too.
		h.Hub.DisconnectUser(id)
	}

	actor := actorEmail(c)
	slog.Info("admin action: role updated",
		"actor", actor, "target_id", id, "new_role", req.Role)

	c.JSON(http.StatusOK, MessageResponse{Message: "role updated"})
}

func (h *Handlers) DeleteUser(c *gin.Context) {
	id, err := auth.ParseUserID(c.Param("id"))
	if err != nil {
		ErrorJSON(c, http.StatusBadRequest, "invalid user id")
		return
	}

	if isSelf(c, id) {
		ErrorJSON(c, http.StatusBadRequest, "cannot delete yourself")
		return
	}

	if err := services.SoftDeleteUser(h.DB, id); err != nil {
		serviceError(c, err, "failed to delete user")
		return
	}

	// A soft-deleted user still holds a valid access token, so cut the refresh
	// chain immediately.
	if err := services.RevokeAllUserRefreshTokens(h.DB, id); err != nil {
		slog.Warn("failed to revoke refresh tokens after user delete", "target_id", id, "error", err)
	}
	// A live socket outlives the row it was authenticated against, so close it too.
	h.Hub.DisconnectUser(id)

	actor := actorEmail(c)
	slog.Info("admin action: user deleted",
		"actor", actor, "target_id", id)

	c.JSON(http.StatusOK, MessageResponse{Message: "user deleted"})
}
