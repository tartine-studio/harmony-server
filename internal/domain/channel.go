package domain

import (
	"context"
	"time"
)

type ChannelType string

const (
	ChannelTypeText  ChannelType = "text"
	ChannelTypeVoice ChannelType = "voice"
)

type Channel struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Type      ChannelType `json:"type"`
	CreatedAt time.Time   `json:"createdAt"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

type ChannelRepository interface {
	Create(ctx context.Context, channel *Channel) error
	GetAll(ctx context.Context) ([]Channel, error)
	GetByID(ctx context.Context, id string) (*Channel, error)
	Update(ctx context.Context, channel *Channel) error
	Delete(ctx context.Context, id string) error
}
