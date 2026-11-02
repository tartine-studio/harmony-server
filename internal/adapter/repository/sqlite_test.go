package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

// These tests exercise the repositories through the domain interfaces, so a
// future PostgreSQL adapter can run the same suite.

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "harmony.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func ts(sec int) time.Time {
	return time.Date(2026, 1, 1, 12, 0, sec, 0, time.UTC)
}

func mkUser(t *testing.T, repo domain.UserRepository, id string) *domain.User {
	t.Helper()
	u := &domain.User{ID: id, Username: id, Email: id + "@example.com", Password: "hash", CreatedAt: ts(0), UpdatedAt: ts(0)}
	require.NoError(t, repo.Create(context.Background(), u))
	return u
}

func mkChannel(t *testing.T, repo domain.ChannelRepository, id string, typ domain.ChannelType) *domain.Channel {
	t.Helper()
	c := &domain.Channel{ID: id, Name: id, Type: typ, CreatedAt: ts(0), UpdatedAt: ts(0)}
	require.NoError(t, repo.Create(context.Background(), c))
	return c
}

func TestOpen_RunsMigrationsIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "harmony.db")
	db, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	db, err = Open(path) // second start on an existing database
	require.NoError(t, err)
	defer db.Close()

	var fk int
	require.NoError(t, db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk))
	require.Equal(t, 1, fk, "foreign keys must be enforced")
}

func TestUserRepository(t *testing.T) {
	ctx := context.Background()
	var repo domain.UserRepository = NewUserRepository(openTestDB(t))

	alice := mkUser(t, repo, "alice")

	got, err := repo.GetByID(ctx, "alice")
	require.NoError(t, err)
	require.Equal(t, alice.Email, got.Email)
	require.Equal(t, "hash", got.Password)
	require.Equal(t, ts(0), got.CreatedAt)
	require.False(t, got.Invisible)

	byEmail, err := repo.GetByEmail(ctx, "alice@example.com")
	require.NoError(t, err)
	require.Equal(t, "alice", byEmail.ID)

	missing, err := repo.GetByID(ctx, "nobody")
	require.NoError(t, err)
	require.Nil(t, missing, "not found is (nil, nil)")
	missing, err = repo.GetByEmail(ctx, "nobody@example.com")
	require.NoError(t, err)
	require.Nil(t, missing)

	t.Run("email is unique", func(t *testing.T) {
		dup := &domain.User{ID: "alice2", Username: "a", Email: "alice@example.com", Password: "x", CreatedAt: ts(0), UpdatedAt: ts(0)}
		require.Error(t, repo.Create(ctx, dup))
	})

	t.Run("update", func(t *testing.T) {
		alice.Username = "Alice"
		require.NoError(t, repo.Update(ctx, alice))
		got, _ := repo.GetByID(ctx, "alice")
		require.Equal(t, "Alice", got.Username)
	})

	t.Run("invisible is persisted", func(t *testing.T) {
		require.NoError(t, repo.SetInvisible(ctx, "alice", true))
		got, _ := repo.GetByID(ctx, "alice")
		require.True(t, got.Invisible)
	})

	t.Run("list and delete", func(t *testing.T) {
		mkUser(t, repo, "bob")
		all, err := repo.GetAll(ctx)
		require.NoError(t, err)
		require.Len(t, all, 2)

		require.NoError(t, repo.Delete(ctx, "bob"))
		got, _ := repo.GetByID(ctx, "bob")
		require.Nil(t, got)
	})
}

func TestChannelRepository(t *testing.T) {
	ctx := context.Background()
	var repo domain.ChannelRepository = NewChannelRepository(openTestDB(t))

	mkChannel(t, repo, "general", domain.ChannelTypeText)
	lounge := mkChannel(t, repo, "lounge", domain.ChannelTypeVoice)

	got, err := repo.GetByID(ctx, "lounge")
	require.NoError(t, err)
	require.Equal(t, domain.ChannelTypeVoice, got.Type)

	missing, err := repo.GetByID(ctx, "nope")
	require.NoError(t, err)
	require.Nil(t, missing)

	all, err := repo.GetAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)

	lounge.Name = "Lounge"
	lounge.UpdatedAt = ts(5)
	require.NoError(t, repo.Update(ctx, lounge))
	got, _ = repo.GetByID(ctx, "lounge")
	require.Equal(t, "Lounge", got.Name)
	require.Equal(t, ts(5), got.UpdatedAt)

	require.Error(t, repo.Create(ctx, &domain.Channel{ID: "bad", Name: "bad", Type: "video", CreatedAt: ts(0), UpdatedAt: ts(0)}),
		"only text and voice channels exist")

	require.NoError(t, repo.Delete(ctx, "lounge"))
	got, _ = repo.GetByID(ctx, "lounge")
	require.Nil(t, got)
}

func TestMessageRepository(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	users := NewUserRepository(db)
	channels := NewChannelRepository(db)
	var repo domain.MessageRepository = NewMessageRepository(db)

	mkUser(t, users, "alice")
	mkChannel(t, channels, "general", domain.ChannelTypeText)
	mkChannel(t, channels, "random", domain.ChannelTypeText)

	for i, id := range []string{"m2", "m1", "m3"} {
		// Created out of insertion order: history is sorted by created_at.
		sec := map[string]int{"m1": 1, "m2": 2, "m3": 3}[id]
		require.NoError(t, repo.Create(ctx, &domain.Message{
			ID: id, UserID: "alice", ChannelID: "general", Content: id, CreatedAt: ts(sec), UpdatedAt: ts(sec),
		}), "message %d", i)
	}
	require.NoError(t, repo.Create(ctx, &domain.Message{ID: "other", UserID: "alice", ChannelID: "random", Content: "x", CreatedAt: ts(0), UpdatedAt: ts(0)}))

	list, err := repo.GetByChannelID(ctx, "general")
	require.NoError(t, err)
	require.Len(t, list, 3)
	require.Equal(t, []string{"m1", "m2", "m3"}, []string{list[0].ID, list[1].ID, list[2].ID})

	m, err := repo.GetByID(ctx, "m1")
	require.NoError(t, err)
	m.Content = "edited"
	m.UpdatedAt = ts(10)
	require.NoError(t, repo.Update(ctx, m))
	m, _ = repo.GetByID(ctx, "m1")
	require.Equal(t, "edited", m.Content)
	require.Equal(t, ts(10), m.UpdatedAt)

	require.NoError(t, repo.Delete(ctx, "m1"))
	m, err = repo.GetByID(ctx, "m1")
	require.NoError(t, err)
	require.Nil(t, m)

	t.Run("foreign keys", func(t *testing.T) {
		err := repo.Create(ctx, &domain.Message{ID: "x", UserID: "ghost", ChannelID: "general", Content: "x", CreatedAt: ts(0), UpdatedAt: ts(0)})
		require.Error(t, err, "unknown author")
		err = repo.Create(ctx, &domain.Message{ID: "y", UserID: "alice", ChannelID: "ghost", Content: "x", CreatedAt: ts(0), UpdatedAt: ts(0)})
		require.Error(t, err, "unknown channel")
	})

	t.Run("deleting a channel deletes its messages", func(t *testing.T) {
		require.NoError(t, channels.Delete(ctx, "general"))
		m, err := repo.GetByID(ctx, "m2")
		require.NoError(t, err)
		require.Nil(t, m)
	})

	t.Run("deleting an author keeps their messages", func(t *testing.T) {
		require.NoError(t, users.Delete(ctx, "alice"))
		m, err := repo.GetByID(ctx, "other")
		require.NoError(t, err)
		require.NotNil(t, m, "message is kept")
		require.Empty(t, m.UserID, "author is cleared")

		list, err := repo.GetByChannelID(ctx, "random")
		require.NoError(t, err)
		require.Len(t, list, 1)
		require.Empty(t, list[0].UserID)
	})
}
