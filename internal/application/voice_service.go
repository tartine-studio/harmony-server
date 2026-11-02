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
	media           domain.VoiceMedia
	maxParticipants int
}

func NewVoiceService(channelRepo domain.ChannelRepository, notifier domain.VoiceNotifier, media domain.VoiceMedia, maxParticipants int) *VoiceService {
	return &VoiceService{
		byUser:          make(map[string]*domain.VoiceState),
		channelRepo:     channelRepo,
		notifier:        notifier,
		media:           media,
		maxParticipants: maxParticipants,
	}
}

// Join puts the session's user in channelID. If the user is already in voice
// from another session, that session is kicked (one device at a time).
// Moving between channels keeps the mute/deafen state. Re-joining the same
// channel from the same session restarts the media connection.
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
		if err := s.media.AddPeer(sessionID, userID, channelID, cur.Deafened); err != nil {
			s.removeLocked(cur)
			return fmt.Errorf("restart voice peer: %w", err)
		}
		return nil
	}
	if s.countLocked(channelID, userID) >= s.maxParticipants {
		return ErrVoiceChannelFull
	}

	next := &domain.VoiceState{UserID: userID, SessionID: sessionID, ChannelID: channelID}
	if cur != nil {
		next.SelfMuted, next.Deafened = cur.SelfMuted, cur.Deafened
	}

	// Replaces this session's previous peer when moving between channels.
	if err := s.media.AddPeer(sessionID, userID, channelID, next.Deafened); err != nil {
		if cur != nil && cur.SessionID == sessionID {
			s.removeLocked(cur) // its peer is gone
		}
		return fmt.Errorf("add voice peer: %w", err)
	}
	if cur != nil && cur.SessionID != sessionID {
		s.media.RemovePeer(cur.SessionID)
		s.notifier.NotifyVoiceDisconnected(cur.SessionID, domain.VoiceDisconnectOtherDevice)
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
	if next.Deafened != cur.Deafened {
		s.media.SetDeafened(sessionID, next.Deafened)
	}
	*cur = next
	s.notifier.PublishVoiceState(next)
	return nil
}

// HandleAnswer forwards the session's SDP answer to the media plane.
func (s *VoiceService) HandleAnswer(userID, sessionID, sdp string) error {
	if !s.isCurrentSession(userID, sessionID) {
		return ErrNotInVoice
	}
	return s.media.HandleAnswer(sessionID, sdp)
}

// AddICECandidate forwards a trickled client ICE candidate to the media plane.
func (s *VoiceService) AddICECandidate(userID, sessionID string, c domain.ICECandidate) error {
	if !s.isCurrentSession(userID, sessionID) {
		return ErrNotInVoice
	}
	return s.media.AddICECandidate(sessionID, c)
}

// PeerFailed implements domain.VoiceMediaEvents: the session's media
// connection failed or never established, so it leaves voice.
func (s *VoiceService) PeerFailed(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, st := range s.byUser {
		if st.SessionID == sessionID {
			s.notifier.NotifyVoiceDisconnected(sessionID, domain.VoiceDisconnectConnectionFailed)
			s.removeLocked(st)
			return
		}
	}
}

func (s *VoiceService) isCurrentSession(userID, sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.byUser[userID]
	return cur != nil && cur.SessionID == sessionID
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
	s.media.RemovePeer(st.SessionID)
	left := *st
	left.ChannelID = ""
	s.notifier.PublishVoiceState(left)
}
