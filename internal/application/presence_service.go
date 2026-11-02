package application

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

const presenceIdleCheckInterval = 15 * time.Second

type presenceEntry struct {
	conns        int
	status       domain.PresenceStatus
	invisible    bool
	lastActivity time.Time
	offlineTimer *time.Timer // pending offline transition after the last session closed
	offlineGen   uint64      // identifies the current offlineTimer; stale callbacks compare against it
}

// PresenceService tracks who is connected, in memory. Status is derived by the
// server (online/idle/offline); only the invisible flag is user-controlled and
// persisted. Invisible users appear offline to everyone but their own sessions.
type PresenceService struct {
	mu           sync.Mutex
	users        map[string]*presenceEntry
	userRepo     domain.UserRepository
	notifier     domain.PresenceNotifier
	idleTimeout  time.Duration
	offlineGrace time.Duration
	now          func() time.Time
}

func NewPresenceService(userRepo domain.UserRepository, notifier domain.PresenceNotifier, idleTimeout, offlineGrace time.Duration) *PresenceService {
	return &PresenceService{
		users:        make(map[string]*presenceEntry),
		userRepo:     userRepo,
		notifier:     notifier,
		idleTimeout:  idleTimeout,
		offlineGrace: offlineGrace,
		now:          time.Now,
	}
}

// Run periodically moves inactive users to idle. Blocks until ctx is done.
func (s *PresenceService) Run(ctx context.Context) {
	ticker := time.NewTicker(presenceIdleCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.markIdle()
		}
	}
}

// Connect registers a new session for userID.
func (s *PresenceService) Connect(ctx context.Context, userID string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		return ErrUserNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.users[userID]; ok {
		// Additional session, or reconnect within the grace period: no offline flap.
		e.conns++
		if e.offlineTimer != nil {
			e.offlineTimer.Stop()
			e.offlineTimer = nil
		}
		s.touchLocked(userID, e)
		return nil
	}

	e := &presenceEntry{
		conns:        1,
		status:       domain.PresenceOnline,
		invisible:    user.Invisible,
		lastActivity: s.now(),
	}
	s.users[userID] = e
	s.publishLocked(userID, e, !e.invisible)
	return nil
}

// Disconnect closes a session. When the last one closes, the user goes offline
// after the grace period unless they reconnect in the meantime.
func (s *PresenceService) Disconnect(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.users[userID]
	if !ok || e.conns == 0 {
		return
	}
	e.conns--
	if e.conns > 0 {
		return
	}

	e.offlineGen++
	gen := e.offlineGen
	e.offlineTimer = time.AfterFunc(s.offlineGrace, func() { s.expire(userID, e, gen) })
}

// Touch records user activity, bringing an idle user back online.
func (s *PresenceService) Touch(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.users[userID]; ok {
		s.touchLocked(userID, e)
	}
}

// SetInvisible persists the invisible flag and republishes the user's presence.
func (s *PresenceService) SetInvisible(ctx context.Context, userID string, invisible bool) error {
	if err := s.userRepo.SetInvisible(ctx, userID, invisible); err != nil {
		return fmt.Errorf("set invisible: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.users[userID]
	if !ok || e.invisible == invisible {
		return nil
	}
	e.invisible = invisible
	// Others see offline when turning invisible, the real status when turning visible.
	s.publishLocked(userID, e, true)
	return nil
}

// Snapshot returns the presence of every user visible to viewerID. Offline
// users are omitted; the viewer's own entry carries the real status.
func (s *PresenceService) Snapshot(viewerID string) []domain.Presence {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]domain.Presence, 0, len(s.users))
	for id, e := range s.users {
		if id == viewerID {
			out = append(out, domain.Presence{UserID: id, Status: e.status, Invisible: e.invisible})
			continue
		}
		if e.invisible {
			continue
		}
		out = append(out, domain.Presence{UserID: id, Status: e.status})
	}
	return out
}

func (s *PresenceService) expire(userID string, e *presenceEntry, gen uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ignore stale timers (user reconnected, or a newer timer replaced this one).
	if s.users[userID] != e || e.offlineGen != gen || e.conns > 0 {
		return
	}
	delete(s.users, userID)
	e.status = domain.PresenceOffline
	s.publishLocked(userID, e, !e.invisible)
}

func (s *PresenceService) markIdle() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	for id, e := range s.users {
		if e.status == domain.PresenceOnline && now.Sub(e.lastActivity) >= s.idleTimeout {
			e.status = domain.PresenceIdle
			s.publishLocked(id, e, !e.invisible)
		}
	}
}

func (s *PresenceService) touchLocked(userID string, e *presenceEntry) {
	e.lastActivity = s.now()
	if e.status == domain.PresenceIdle {
		e.status = domain.PresenceOnline
		s.publishLocked(userID, e, !e.invisible)
	}
}

// publishLocked must be called with s.mu held so updates are emitted in order.
func (s *PresenceService) publishLocked(userID string, e *presenceEntry, toOthers bool) {
	self := domain.Presence{UserID: userID, Status: e.status, Invisible: e.invisible}

	var public *domain.Presence
	if toOthers {
		p := domain.Presence{UserID: userID, Status: e.status}
		if e.invisible {
			p.Status = domain.PresenceOffline
		}
		public = &p
	}
	s.notifier.PublishPresence(self, public)
}
