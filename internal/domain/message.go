package domain

import (
	"context"
	"time"
)

type Message struct {
	ID string `json:"id"`
	// UserID is the author, empty when the author's account was deleted
	// (the message is kept, shown as "Deleted user").
	UserID    string    `json:"userId"`
	ChannelID string    `json:"channelId"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type MessageRepository interface {
	Create(ctx context.Context, message *Message) error
	GetByID(ctx context.Context, id string) (*Message, error)
	GetByChannelID(ctx context.Context, channelID string) ([]Message, error)
	Update(ctx context.Context, message *Message) error
	Delete(ctx context.Context, id string) error
}
