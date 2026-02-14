package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

type MessageRepository struct {
	db *sql.DB
}

func NewMessageRepository(db *sql.DB) *MessageRepository {
	return &MessageRepository{db: db}
}

func (r *MessageRepository) Create(ctx context.Context, message *domain.Message) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO messages (id, user_id, channel_id, content, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		message.ID, message.UserID, message.ChannelID, message.Content,
		message.CreatedAt.UTC().Format(time.RFC3339),
		message.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create message: %w", err)
	}
	return nil
}

func (r *MessageRepository) GetByID(ctx context.Context, id string) (*domain.Message, error) {
	return r.scanMessage(r.db.QueryRowContext(ctx,
		`SELECT id, user_id, channel_id, content, created_at, updated_at FROM messages WHERE id = ?`, id,
	))
}

func (r *MessageRepository) GetByChannelID(ctx context.Context, channelID string) ([]domain.Message, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, channel_id, content, created_at, updated_at FROM messages WHERE channel_id = ? ORDER BY created_at ASC`, channelID,
	)
	if err != nil {
		return nil, fmt.Errorf("get messages by channel: %w", err)
	}
	defer rows.Close()

	var messages []domain.Message
	for rows.Next() {
		var msg domain.Message
		var createdAt, updatedAt string
		if err := rows.Scan(&msg.ID, &msg.UserID, &msg.ChannelID, &msg.Content, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msg.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		msg.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	return messages, nil
}

func (r *MessageRepository) Update(ctx context.Context, message *domain.Message) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE messages SET content = ?, updated_at = ? WHERE id = ?`,
		message.Content, message.UpdatedAt.UTC().Format(time.RFC3339), message.ID,
	)
	if err != nil {
		return fmt.Errorf("update message: %w", err)
	}
	return nil
}

func (r *MessageRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete message: %w", err)
	}
	return nil
}

func (r *MessageRepository) scanMessage(row *sql.Row) (*domain.Message, error) {
	var msg domain.Message
	var createdAt, updatedAt string

	err := row.Scan(&msg.ID, &msg.UserID, &msg.ChannelID, &msg.Content, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan message: %w", err)
	}

	msg.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	msg.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &msg, nil
}
