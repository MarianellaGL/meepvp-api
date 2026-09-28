package handlers

import (
	"time"

	"tablescore-api/models"
)

// Request bodies. The binding tags are the ones the anonymous structs
// carried before. The Key: prefix in a failed binding's validation message
// now carries the exported struct name (e.g. "RegisterRequest.Email"
// instead of the old anonymous struct), which is accepted because the
// names are needed for TypeScript generation.

type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Name     string `json:"name" binding:"required"`
	Password string `json:"password" binding:"required,min=8,max=1024"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,max=1024"`
}

type CreateUserRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Name     string `json:"name" binding:"required"`
	Password string `json:"password" binding:"required,min=8,max=1024"`
	Role     string `json:"role" binding:"required,oneof=admin user" tstype:"'admin' | 'user'"`
}

type UpdateUserRequest struct {
	Email string `json:"email" binding:"omitempty,email"`
	Name  string `json:"name" binding:"omitempty"`
}

type UpdateUserRoleRequest struct {
	Role string `json:"role" binding:"required,oneof=admin user" tstype:"'admin' | 'user'"`
}

type UpdateSettingRequest struct {
	Value string `json:"value" binding:"required"`
}

type VerifyEmailRequest struct {
	Token string `json:"token" binding:"required"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email"`
}

type ResetPasswordRequest struct {
	Token    string `json:"token" binding:"required"`
	Password string `json:"password" binding:"required,min=8,max=1024"`
}

// Responses.

type UserResponse struct {
	Username      string    `json:"username"`
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	Role          string    `json:"role" tstype:"'admin' | 'user'"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type PaginatedUsersResponse struct {
	Data       []UserResponse `json:"data"`
	Total      int64          `json:"total"`
	Page       int            `json:"page"`
	PerPage    int            `json:"per_page"`
	TotalPages int            `json:"total_pages"`
}

type SettingResponse struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type MessageResponse struct {
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type HealthResponse struct {
	Status string `json:"status"`
}

func toUserResponse(u *models.User) UserResponse {
	return UserResponse{
		ID:            u.ID,
		Username:      u.Username,
		Email:         u.Email,
		Name:          u.Name,
		Role:          u.Role,
		EmailVerified: u.EmailVerifiedAt != nil,
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
	}
}

func toUserResponses(users []models.User) []UserResponse {
	out := make([]UserResponse, 0, len(users))
	for i := range users {
		out = append(out, toUserResponse(&users[i]))
	}
	return out
}

func toSettingResponse(r models.AppConfig) SettingResponse {
	return SettingResponse{ID: r.ID, Key: r.Key, Name: r.Name, Value: r.Value}
}

func toSettingResponses(rows []models.AppConfig) []SettingResponse {
	out := make([]SettingResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, toSettingResponse(r))
	}
	return out
}
