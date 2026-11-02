package application

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

func TestChannelService_CRUD(t *testing.T) {
	ctx := context.Background()
	listener := &recordingDeletionListener{}
	svc := NewChannelService(newFakeChannelRepo(), listener)

	ch, err := svc.Create(ctx, "general", domain.ChannelTypeText)
	require.NoError(t, err)
	require.NotEmpty(t, ch.ID)
	require.Equal(t, domain.ChannelTypeText, ch.Type)

	got, err := svc.GetByID(ctx, ch.ID)
	require.NoError(t, err)
	require.Equal(t, "general", got.Name)

	updated, err := svc.Update(ctx, ch.ID, "lobby")
	require.NoError(t, err)
	require.Equal(t, "lobby", updated.Name)

	all, err := svc.GetAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)

	require.NoError(t, svc.Delete(ctx, ch.ID))
	require.Equal(t, []string{ch.ID}, listener.deleted, "deletion listener must be notified")

	_, err = svc.GetByID(ctx, ch.ID)
	require.ErrorIs(t, err, ErrChannelNotFound)
	_, err = svc.Update(ctx, ch.ID, "x")
	require.ErrorIs(t, err, ErrChannelNotFound)
	require.ErrorIs(t, svc.Delete(ctx, ch.ID), ErrChannelNotFound)
	require.Len(t, listener.deleted, 1, "no notification for a missing channel")
}

func TestMessageService(t *testing.T) {
	ctx := context.Background()
	channels := newFakeChannelRepo(domain.Channel{ID: "general", Type: domain.ChannelTypeText})
	svc := NewMessageService(newFakeMessageRepo(), channels)

	_, err := svc.Create(ctx, "alice", "missing", "hi")
	require.ErrorIs(t, err, ErrChannelNotFound)
	_, err = svc.GetByChannelID(ctx, "missing")
	require.ErrorIs(t, err, ErrChannelNotFound)

	msg, err := svc.Create(ctx, "alice", "general", "hello")
	require.NoError(t, err)
	require.Equal(t, "alice", msg.UserID)
	require.Equal(t, "general", msg.ChannelID)

	list, err := svc.GetByChannelID(ctx, "general")
	require.NoError(t, err)
	require.Len(t, list, 1)

	t.Run("only the author can edit", func(t *testing.T) {
		_, err := svc.Update(ctx, "bob", msg.ID, "hacked")
		require.ErrorIs(t, err, ErrMessageForbidden)

		edited, err := svc.Update(ctx, "alice", msg.ID, "hello!")
		require.NoError(t, err)
		require.Equal(t, "hello!", edited.Content)
		require.False(t, edited.UpdatedAt.Before(edited.CreatedAt))
	})

	t.Run("only the author can delete", func(t *testing.T) {
		require.ErrorIs(t, svc.Delete(ctx, "bob", msg.ID), ErrMessageForbidden)
		require.NoError(t, svc.Delete(ctx, "alice", msg.ID))
		require.ErrorIs(t, svc.Delete(ctx, "alice", msg.ID), ErrMessageNotFound)
	})

	t.Run("unknown message", func(t *testing.T) {
		_, err := svc.Update(ctx, "alice", "nope", "x")
		require.ErrorIs(t, err, ErrMessageNotFound)
	})
}

func TestUserService(t *testing.T) {
	ctx := context.Background()
	svc := NewUserService(newFakeUserRepo(testUser("alice"), testUser("bob")))

	_, err := svc.Update(ctx, "alice", "", "bob@example.com")
	require.ErrorIs(t, err, ErrEmailTaken, "email already used by someone else")
	_, err = svc.Update(ctx, "alice", "", "alice@example.com")
	require.NoError(t, err, "keeping one's own email is fine")

	u, err := svc.Update(ctx, "alice", "", "new@example.com")
	require.NoError(t, err)
	require.Equal(t, "alice", u.Username, "empty fields are left unchanged")
	require.Equal(t, "new@example.com", u.Email)

	_, err = svc.GetByID(ctx, "carol")
	require.ErrorIs(t, err, ErrUserNotFound)
	_, err = svc.Update(ctx, "carol", "x", "")
	require.ErrorIs(t, err, ErrUserNotFound)

	require.NoError(t, svc.Delete(ctx, "alice"))
	require.ErrorIs(t, svc.Delete(ctx, "alice"), ErrUserNotFound)
}
