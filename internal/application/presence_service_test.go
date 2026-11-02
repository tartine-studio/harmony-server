package application

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

const (
	testIdleTimeout  = 5 * time.Minute
	testOfflineGrace = 10 * time.Second
)

// All presence tests run inside a synctest bubble: time is virtual, so the
// idle ticker and the offline grace timer fire instantly and deterministically.

func newPresence(users ...domain.User) (*PresenceService, *fakePresenceNotifier) {
	n := &fakePresenceNotifier{}
	return NewPresenceService(newFakeUserRepo(users...), n, testIdleTimeout, testOfflineGrace), n
}

func status(p *domain.Presence) domain.PresenceStatus {
	if p == nil {
		return ""
	}
	return p.Status
}

func TestPresence_ConnectPublishesOnline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, n := newPresence(testUser("alice"))
		require.NoError(t, svc.Connect(context.Background(), "alice"))

		last := n.last()
		require.Equal(t, domain.PresenceOnline, last.self.Status)
		require.Equal(t, domain.PresenceOnline, status(last.public))

		require.ErrorIs(t, svc.Connect(context.Background(), "ghost"), ErrUserNotFound)
	})
}

func TestPresence_IdleAndBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		svc, n := newPresence(testUser("alice"))
		go svc.Run(ctx)
		require.NoError(t, svc.Connect(ctx, "alice"))

		time.Sleep(testIdleTimeout - time.Second)
		synctest.Wait()
		require.Equal(t, domain.PresenceOnline, n.last().self.Status, "not idle before the timeout")

		// The idle check runs every 15s.
		time.Sleep(presenceIdleCheckInterval + time.Second)
		synctest.Wait()
		require.Equal(t, domain.PresenceIdle, n.last().self.Status)
		require.Equal(t, domain.PresenceIdle, status(n.last().public))

		svc.Touch("alice")
		require.Equal(t, domain.PresenceOnline, n.last().self.Status)
	})
}

func TestPresence_OfflineAfterGrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, n := newPresence(testUser("alice"))
		require.NoError(t, svc.Connect(context.Background(), "alice"))
		calls := len(n.all())

		svc.Disconnect("alice")
		time.Sleep(testOfflineGrace - time.Second)
		synctest.Wait()
		require.Len(t, n.all(), calls, "nothing published during the grace period")

		time.Sleep(2 * time.Second)
		synctest.Wait()
		require.Equal(t, domain.PresenceOffline, n.last().self.Status)
		require.Equal(t, domain.PresenceOffline, status(n.last().public))
		require.Empty(t, svc.Snapshot("bob"))
	})
}

func TestPresence_ReconnectWithinGraceDoesNotFlap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, n := newPresence(testUser("alice"))
		require.NoError(t, svc.Connect(context.Background(), "alice"))
		calls := len(n.all())

		svc.Disconnect("alice")
		time.Sleep(testOfflineGrace / 2)
		require.NoError(t, svc.Connect(context.Background(), "alice"))
		time.Sleep(2 * testOfflineGrace)
		synctest.Wait()

		require.Len(t, n.all(), calls, "no offline/online flap")
		require.Equal(t, domain.PresenceOnline, svc.Snapshot("bob")[0].Status)
	})
}

func TestPresence_MultipleSessions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, n := newPresence(testUser("alice"))
		ctx := context.Background()
		require.NoError(t, svc.Connect(ctx, "alice"))
		require.NoError(t, svc.Connect(ctx, "alice"))

		svc.Disconnect("alice")
		time.Sleep(2 * testOfflineGrace)
		synctest.Wait()
		require.Equal(t, domain.PresenceOnline, n.last().self.Status, "online while one session remains")

		svc.Disconnect("alice")
		time.Sleep(2 * testOfflineGrace)
		synctest.Wait()
		require.Equal(t, domain.PresenceOffline, n.last().self.Status)
	})
}

func TestPresence_Invisible(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		hidden := testUser("alice")
		hidden.Invisible = true
		svc, n := newPresence(hidden, testUser("bob"))

		require.NoError(t, svc.Connect(ctx, "alice"))
		last := n.last()
		require.Nil(t, last.public, "an invisible user's connection is not broadcast")
		require.True(t, last.self.Invisible)
		require.Equal(t, domain.PresenceOnline, last.self.Status, "self sees the real status")

		require.NoError(t, svc.Connect(ctx, "bob"))
		require.Equal(t, []domain.Presence{{UserID: "bob", Status: domain.PresenceOnline}}, svc.Snapshot("bob"),
			"others don't see the invisible user")

		self := svc.Snapshot("alice")
		require.Contains(t, self, domain.Presence{UserID: "alice", Status: domain.PresenceOnline, Invisible: true})

		// Turning visible publishes the real status to everyone.
		require.NoError(t, svc.SetInvisible(ctx, "alice", false))
		require.Equal(t, domain.PresenceOnline, status(n.last().public))

		// Turning invisible again looks like going offline to others.
		require.NoError(t, svc.SetInvisible(ctx, "alice", true))
		last = n.last()
		require.Equal(t, domain.PresenceOffline, status(last.public))
		require.Equal(t, domain.PresenceOnline, last.self.Status)
		require.False(t, last.public.Invisible, "invisible is never exposed to others")
	})
}

func TestPresence_InvisibleIdleIsNotBroadcast(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		hidden := testUser("alice")
		hidden.Invisible = true
		svc, n := newPresence(hidden)
		go svc.Run(ctx)
		require.NoError(t, svc.Connect(ctx, "alice"))

		time.Sleep(testIdleTimeout + presenceIdleCheckInterval)
		synctest.Wait()
		last := n.last()
		require.Equal(t, domain.PresenceIdle, last.self.Status)
		require.Nil(t, last.public, "no activity leak while invisible")
	})
}
