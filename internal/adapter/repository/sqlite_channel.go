package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

type ChannelRepository struct {
	db *sql.DB
}

func NewChannelRepository(db *sql.DB) *ChannelRepository {
	return &ChannelRepository{db: db}
}

func (r *ChannelRepository) Create(ctx context.Context, channel *domain.Channel) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO channels (id, name, type, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		channel.ID, channel.Name, channel.Type,
		channel.CreatedAt.UTC().Format(time.RFC3339),
		channel.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create channel: %w", err)
	}
	return nil
}

func (r *ChannelRepository) GetByID(ctx context.Context, id string) (*domain.Channel, error) {
	return r.scanChannel(r.db.QueryRowContext(ctx,
		`SELECT id, name, type, created_at, updated_at FROM channels WHERE id = ?`, id,
	))
}

func (r *ChannelRepository) GetAll(ctx context.Context) ([]domain.Channel, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, type, created_at, updated_at FROM channels`,
	)
	if err != nil {
		return nil, fmt.Errorf("get all channels: %w", err)
	}
	defer rows.Close()

	var channels []domain.Channel
	for rows.Next() {
		var ch domain.Channel
		var createdAt, updatedAt string
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Type, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		ch.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		ch.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate channels: %w", err)
	}
	return channels, nil
}

func (r *ChannelRepository) Update(ctx context.Context, channel *domain.Channel) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE channels SET name = ?, updated_at = ? WHERE id = ?`,
		channel.Name, channel.UpdatedAt.UTC().Format(time.RFC3339), channel.ID,
	)
	if err != nil {
		return fmt.Errorf("update channel: %w", err)
	}
	return nil
}

func (r *ChannelRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM channels WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}
	return nil
}

func (r *ChannelRepository) scanChannel(row *sql.Row) (*domain.Channel, error) {
	var ch domain.Channel
	var createdAt, updatedAt string

	err := row.Scan(&ch.ID, &ch.Name, &ch.Type, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan channel: %w", err)
	}

	ch.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	ch.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &ch, nil
}
