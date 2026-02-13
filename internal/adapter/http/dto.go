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

type ChannelResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func ChannelToResponse(ch *domain.Channel) ChannelResponse {
	return ChannelResponse{
		ID:        ch.ID,
		Name:      ch.Name,
		Type:      string(ch.Type),
		CreatedAt: ch.CreatedAt.Format(time.RFC3339),
		UpdatedAt: ch.UpdatedAt.Format(time.RFC3339),
	}
}

func ChannelsToResponse(channels []domain.Channel) []ChannelResponse {
	res := make([]ChannelResponse, len(channels))
	for i := range channels {
		res[i] = ChannelToResponse(&channels[i])
	}
	return res
}
