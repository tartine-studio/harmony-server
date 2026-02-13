package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO users (id, username, email, password, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		user.ID, user.Username, user.Email, user.Password,
		user.CreatedAt.UTC().Format(time.RFC3339),
		user.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (r *UserRepository) GetByID(ctx context.Context, id string) (*domain.User, error) {
	return r.scanUser(r.db.QueryRowContext(ctx,
		`SELECT id, username, email, password, created_at, updated_at FROM users WHERE id = ?`, id,
	))
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	return r.scanUser(r.db.QueryRowContext(ctx,
		`SELECT id, username, email, password, created_at, updated_at FROM users WHERE email = ?`, email,
	))
}

func (r *UserRepository) GetAll(ctx context.Context) ([]domain.User, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, username, email, password, created_at, updated_at FROM users`,
	)
	if err != nil {
		return nil, fmt.Errorf("get all users: %w", err)
	}
	defer rows.Close()

	var users []domain.User
	for rows.Next() {
		var u domain.User
		var createdAt, updatedAt string
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Password, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		u.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

func (r *UserRepository) Update(ctx context.Context, user *domain.User) error {
	user.UpdatedAt = time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET username = ?, email = ?, updated_at = ? WHERE id = ?`,
		user.Username, user.Email, user.UpdatedAt.Format(time.RFC3339), user.ID,
	)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

func (r *UserRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

func (r *UserRepository) scanUser(row *sql.Row) (*domain.User, error) {
	var u domain.User
	var createdAt, updatedAt string

	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.Password, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}

	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	u.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &u, nil
}
