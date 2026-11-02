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

type MessageResponse struct {
	ID        string  `json:"id"`
	UserID    *string `json:"userId"` // null when the author was deleted
	ChannelID string  `json:"channelId"`
	Content   string  `json:"content"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

func MessageToResponse(m *domain.Message) MessageResponse {
	return MessageResponse{
		ID:        m.ID,
		UserID:    authorID(m.UserID),
		ChannelID: m.ChannelID,
		Content:   m.Content,
		CreatedAt: m.CreatedAt.Format(time.RFC3339),
		UpdatedAt: m.UpdatedAt.Format(time.RFC3339),
	}
}

// authorID maps a deleted author (empty ID) to JSON null.
func authorID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

func MessagesToResponse(messages []domain.Message) []MessageResponse {
	res := make([]MessageResponse, len(messages))
	for i := range messages {
		res[i] = MessageToResponse(&messages[i])
	}
	return res
}
