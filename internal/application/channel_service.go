package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

var ErrChannelNotFound = errors.New("channel not found")

type ChannelService struct {
	repo domain.ChannelRepository
}

func NewChannelService(repo domain.ChannelRepository) *ChannelService {
	return &ChannelService{repo: repo}
}

func (s *ChannelService) Create(ctx context.Context, name string, channelType domain.ChannelType) (*domain.Channel, error) {
	now := time.Now().UTC()
	channel := &domain.Channel{
		ID:        uuid.New().String(),
		Name:      name,
		Type:      channelType,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.repo.Create(ctx, channel); err != nil {
		return nil, fmt.Errorf("create channel: %w", err)
	}
	return channel, nil
}

func (s *ChannelService) GetAll(ctx context.Context) ([]domain.Channel, error) {
	channels, err := s.repo.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("get all channels: %w", err)
	}
	return channels, nil
}

func (s *ChannelService) GetByID(ctx context.Context, id string) (*domain.Channel, error) {
	channel, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}
	if channel == nil {
		return nil, ErrChannelNotFound
	}
	return channel, nil
}

func (s *ChannelService) Update(ctx context.Context, id, name string) (*domain.Channel, error) {
	channel, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}
	if channel == nil {
		return nil, ErrChannelNotFound
	}

	channel.Name = name
	channel.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(ctx, channel); err != nil {
		return nil, fmt.Errorf("update channel: %w", err)
	}
	return channel, nil
}

func (s *ChannelService) Delete(ctx context.Context, id string) error {
	channel, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get channel: %w", err)
	}
	if channel == nil {
		return ErrChannelNotFound
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}
	return nil
}
