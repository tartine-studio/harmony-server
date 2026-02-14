package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

var (
	ErrMessageNotFound  = errors.New("message not found")
	ErrMessageForbidden = errors.New("forbidden")
)

type MessageService struct {
	repo        domain.MessageRepository
	channelRepo domain.ChannelRepository
}

func NewMessageService(repo domain.MessageRepository, channelRepo domain.ChannelRepository) *MessageService {
	return &MessageService{repo: repo, channelRepo: channelRepo}
}

func (s *MessageService) Create(ctx context.Context, userID, channelID, content string) (*domain.Message, error) {
	channel, err := s.channelRepo.GetByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}
	if channel == nil {
		return nil, ErrChannelNotFound
	}

	now := time.Now().UTC()
	message := &domain.Message{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Content:   content,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.repo.Create(ctx, message); err != nil {
		return nil, fmt.Errorf("create message: %w", err)
	}
	return message, nil
}

func (s *MessageService) GetByChannelID(ctx context.Context, channelID string) ([]domain.Message, error) {
	channel, err := s.channelRepo.GetByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}
	if channel == nil {
		return nil, ErrChannelNotFound
	}

	messages, err := s.repo.GetByChannelID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	return messages, nil
}

func (s *MessageService) Update(ctx context.Context, userID, messageID, content string) (*domain.Message, error) {
	message, err := s.repo.GetByID(ctx, messageID)
	if err != nil {
		return nil, fmt.Errorf("get message: %w", err)
	}
	if message == nil {
		return nil, ErrMessageNotFound
	}
	if message.UserID != userID {
		return nil, ErrMessageForbidden
	}

	message.Content = content
	message.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(ctx, message); err != nil {
		return nil, fmt.Errorf("update message: %w", err)
	}
	return message, nil
}

func (s *MessageService) Delete(ctx context.Context, userID, messageID string) error {
	message, err := s.repo.GetByID(ctx, messageID)
	if err != nil {
		return fmt.Errorf("get message: %w", err)
	}
	if message == nil {
		return ErrMessageNotFound
	}
	if message.UserID != userID {
		return ErrMessageForbidden
	}

	if err := s.repo.Delete(ctx, messageID); err != nil {
		return fmt.Errorf("delete message: %w", err)
	}
	return nil
}
