package application

import (
	"context"
	"sync"
	"time"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

// In-memory fakes of the domain ports, shared by the service tests.

type fakeUserRepo struct {
	mu    sync.Mutex
	users map[string]domain.User
}

func newFakeUserRepo(users ...domain.User) *fakeUserRepo {
	r := &fakeUserRepo{users: map[string]domain.User{}}
	for _, u := range users {
		r.users[u.ID] = u
	}
	return r
}

func (r *fakeUserRepo) Create(_ context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users[u.ID] = *u
	return nil
}

func (r *fakeUserRepo) GetAll(context.Context) ([]domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.User, 0, len(r.users))
	for _, u := range r.users {
		out = append(out, u)
	}
	return out, nil
}

func (r *fakeUserRepo) GetByID(_ context.Context, id string) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.users[id]; ok {
		return &u, nil
	}
	return nil, nil
}

func (r *fakeUserRepo) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.Email == email {
			return &u, nil
		}
	}
	return nil, nil
}

func (r *fakeUserRepo) Update(_ context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users[u.ID] = *u
	return nil
}

func (r *fakeUserRepo) SetInvisible(_ context.Context, id string, invisible bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u := r.users[id]
	u.Invisible = invisible
	r.users[id] = u
	return nil
}

func (r *fakeUserRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.users, id)
	return nil
}

type fakeChannelRepo struct {
	mu       sync.Mutex
	channels map[string]domain.Channel
}

func newFakeChannelRepo(channels ...domain.Channel) *fakeChannelRepo {
	r := &fakeChannelRepo{channels: map[string]domain.Channel{}}
	for _, c := range channels {
		r.channels[c.ID] = c
	}
	return r
}

func (r *fakeChannelRepo) Create(_ context.Context, c *domain.Channel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channels[c.ID] = *c
	return nil
}

func (r *fakeChannelRepo) GetAll(context.Context) ([]domain.Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.Channel, 0, len(r.channels))
	for _, c := range r.channels {
		out = append(out, c)
	}
	return out, nil
}

func (r *fakeChannelRepo) GetByID(_ context.Context, id string) (*domain.Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.channels[id]; ok {
		return &c, nil
	}
	return nil, nil
}

func (r *fakeChannelRepo) Update(_ context.Context, c *domain.Channel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channels[c.ID] = *c
	return nil
}

func (r *fakeChannelRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.channels, id)
	return nil
}

type fakeMessageRepo struct {
	mu       sync.Mutex
	messages map[string]domain.Message
}

func newFakeMessageRepo() *fakeMessageRepo {
	return &fakeMessageRepo{messages: map[string]domain.Message{}}
}

func (r *fakeMessageRepo) Create(_ context.Context, m *domain.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages[m.ID] = *m
	return nil
}

func (r *fakeMessageRepo) GetByID(_ context.Context, id string) (*domain.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.messages[id]; ok {
		return &m, nil
	}
	return nil, nil
}

func (r *fakeMessageRepo) GetByChannelID(_ context.Context, channelID string) ([]domain.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.Message
	for _, m := range r.messages {
		if m.ChannelID == channelID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (r *fakeMessageRepo) Update(_ context.Context, m *domain.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages[m.ID] = *m
	return nil
}

func (r *fakeMessageRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.messages, id)
	return nil
}

// presenceCall is one PresenceNotifier.PublishPresence call.
type presenceCall struct {
	self   domain.Presence
	public *domain.Presence
}

type fakePresenceNotifier struct {
	mu    sync.Mutex
	calls []presenceCall
}

func (n *fakePresenceNotifier) PublishPresence(self domain.Presence, public *domain.Presence) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, presenceCall{self: self, public: public})
}

func (n *fakePresenceNotifier) all() []presenceCall {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]presenceCall(nil), n.calls...)
}

func (n *fakePresenceNotifier) last() presenceCall {
	calls := n.all()
	if len(calls) == 0 {
		return presenceCall{}
	}
	return calls[len(calls)-1]
}

type disconnectCall struct {
	sessionID string
	reason    domain.VoiceDisconnectReason
}

type speakingCall struct {
	userID   string
	speaking bool
}

type fakeVoiceNotifier struct {
	mu          sync.Mutex
	states      []domain.VoiceState
	disconnects []disconnectCall
	speaking    []speakingCall
}

func (n *fakeVoiceNotifier) PublishVoiceState(s domain.VoiceState) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.states = append(n.states, s)
}

func (n *fakeVoiceNotifier) NotifyVoiceDisconnected(sessionID string, reason domain.VoiceDisconnectReason) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.disconnects = append(n.disconnects, disconnectCall{sessionID, reason})
}

func (n *fakeVoiceNotifier) PublishSpeaking(userID string, speaking bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.speaking = append(n.speaking, speakingCall{userID, speaking})
}

func (n *fakeVoiceNotifier) lastState() domain.VoiceState {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.states[len(n.states)-1]
}

// fakeMedia records VoiceMedia calls; addErr makes AddPeer fail.
type fakeMedia struct {
	mu      sync.Mutex
	peers   map[string]domain.VoiceState // by session
	added   []domain.VoiceState
	removed []string
	updated []domain.VoiceState
	answers []string
	addErr  error
}

func newFakeMedia() *fakeMedia {
	return &fakeMedia{peers: map[string]domain.VoiceState{}}
}

func (m *fakeMedia) AddPeer(s domain.VoiceState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.addErr != nil {
		return m.addErr
	}
	m.peers[s.SessionID] = s
	m.added = append(m.added, s)
	return nil
}

func (m *fakeMedia) RemovePeer(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.peers, sessionID)
	m.removed = append(m.removed, sessionID)
}

func (m *fakeMedia) HandleAnswer(_, sdp string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.answers = append(m.answers, sdp)
	return nil
}

func (m *fakeMedia) AddICECandidate(string, domain.ICECandidate) error { return nil }

func (m *fakeMedia) UpdatePeer(s domain.VoiceState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updated = append(m.updated, s)
}

func (m *fakeMedia) hasPeer(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.peers[sessionID]
	return ok
}

type recordingDeletionListener struct{ deleted []string }

func (l *recordingDeletionListener) ChannelDeleted(id string) { l.deleted = append(l.deleted, id) }

func testUser(id string) domain.User {
	now := time.Now().UTC()
	return domain.User{ID: id, Username: id, Email: id + "@example.com", CreatedAt: now, UpdatedAt: now}
}
