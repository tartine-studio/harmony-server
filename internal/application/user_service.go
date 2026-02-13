package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

var ErrUserNotFound = errors.New("user not found")

type UserService struct {
	repo domain.UserRepository
}

func NewUserService(repo domain.UserRepository) *UserService {
	return &UserService{repo: repo}
}

func (s *UserService) GetAll(ctx context.Context) ([]domain.User, error) {
	users, err := s.repo.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("get all users: %w", err)
	}
	return users, nil
}

func (s *UserService) GetByID(ctx context.Context, id string) (*domain.User, error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (s *UserService) Update(ctx context.Context, id, username, email string) (*domain.User, error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		return nil, ErrUserNotFound
	}

	if username != "" {
		user.Username = username
	}
	if email != "" {
		user.Email = email
	}
	user.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}
	return user, nil
}

func (s *UserService) Delete(ctx context.Context, id string) error {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		return ErrUserNotFound
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}
