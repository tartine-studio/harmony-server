package application

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

var (
	ErrNotVoiceChannel  = errors.New("not a voice channel")
	ErrVoiceChannelFull = errors.New("voice channel full")
	ErrNotInVoice       = errors.New("not in a voice channel")
)

// VoiceService owns who is in which voice channel. State is in-memory: voice
// sessions do not survive a server restart.
type VoiceService struct {
	mu              sync.Mutex
	byUser          map[string]*domain.VoiceState
	channelRepo     domain.ChannelRepository
	notifier        domain.VoiceNotifier
	maxParticipants int
}

func NewVoiceService(channelRepo domain.ChannelRepository, notifier domain.VoiceNotifier, maxParticipants int) *VoiceService {
	return &VoiceService{
		byUser:          make(map[string]*domain.VoiceState),
		channelRepo:     channelRepo,
		notifier:        notifier,
		maxParticipants: maxParticipants,
	}
}

// Join puts the session's user in channelID. If the user is already in voice
// from another session, that session is kicked (one device at a time).
// Moving between channels keeps the mute/deafen state.
func (s *VoiceService) Join(ctx context.Context, userID, sessionID, channelID string) error {
	channel, err := s.channelRepo.GetByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("get channel: %w", err)
	}
	if channel == nil {
		return ErrChannelNotFound
	}
	if channel.Type != domain.ChannelTypeVoice {
		return ErrNotVoiceChannel
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cur := s.byUser[userID]
	if cur != nil && cur.SessionID == sessionID && cur.ChannelID == channelID {
		return nil
	}
	if s.countLocked(channelID, userID) >= s.maxParticipants {
		return ErrVoiceChannelFull
	}

	next := &domain.VoiceState{UserID: userID, SessionID: sessionID, ChannelID: channelID}
	if cur != nil {
		next.SelfMuted, next.Deafened = cur.SelfMuted, cur.Deafened
		if cur.SessionID != sessionID {
			s.notifier.NotifyVoiceDisconnected(cur.SessionID, domain.VoiceDisconnectOtherDevice)
		}
	}
	s.byUser[userID] = next
	s.notifier.PublishVoiceState(*next)
	return nil
}

// Leave removes the user from voice, only if sessionID is the session that
// joined (a stale device must not kick the current one).
func (s *VoiceService) Leave(userID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cur := s.byUser[userID]
	if cur == nil || cur.SessionID != sessionID {
		return
	}
	s.removeLocked(cur)
}

// SetState updates mute/deafen. Nil fields are left unchanged.
func (s *VoiceService) SetState(userID, sessionID string, selfMuted, deafened *bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cur := s.byUser[userID]
	if cur == nil || cur.SessionID != sessionID {
		return ErrNotInVoice
	}

	next := *cur
	if selfMuted != nil {
		next.SelfMuted = *selfMuted
	}
	if deafened != nil {
		next.Deafened = *deafened
	}
	if next == *cur {
		return nil
	}
	*cur = next
	s.notifier.PublishVoiceState(next)
	return nil
}

// ChannelDeleted kicks everyone out of a deleted channel.
func (s *VoiceService) ChannelDeleted(channelID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, st := range s.byUser {
		if st.ChannelID == channelID {
			s.notifier.NotifyVoiceDisconnected(st.SessionID, domain.VoiceDisconnectChannelDeleted)
			s.removeLocked(st)
		}
	}
}

// Snapshot returns every user currently in voice.
func (s *VoiceService) Snapshot() []domain.VoiceState {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]domain.VoiceState, 0, len(s.byUser))
	for _, st := range s.byUser {
		out = append(out, *st)
	}
	return out
}

func (s *VoiceService) countLocked(channelID, excludeUserID string) int {
	n := 0
	for uid, st := range s.byUser {
		if st.ChannelID == channelID && uid != excludeUserID {
			n++
		}
	}
	return n
}

// removeLocked deletes st and broadcasts the leave. Safe to call while ranging
// over s.byUser.
func (s *VoiceService) removeLocked(st *domain.VoiceState) {
	delete(s.byUser, st.UserID)
	left := *st
	left.ChannelID = ""
	s.notifier.PublishVoiceState(left)
}
