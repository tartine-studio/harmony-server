package http

import (
	"time"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

type UserResponse struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func UserToResponse(u *domain.User) UserResponse {
	return UserResponse{
		ID:        u.ID,
		Username:  u.Username,
		Email:     u.Email,
		CreatedAt: u.CreatedAt.Format(time.RFC3339),
		UpdatedAt: u.UpdatedAt.Format(time.RFC3339),
	}
}

func UsersToResponse(users []domain.User) []UserResponse {
	res := make([]UserResponse, len(users))
	for i := range users {
		res[i] = UserToResponse(&users[i])
	}
	return res
}
