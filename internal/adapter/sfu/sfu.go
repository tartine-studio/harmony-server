// Package sfu is the Pion-based media plane: it receives each participant's
// Opus track and forwards RTP packets, untouched, to the other participants of
// the same voice channel. No decoding, no mixing.
package sfu

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
	"go.uber.org/zap"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

const (
	defaultConnectTimeout = 30 * time.Second
	gatheringTimeout      = 2 * time.Second

	// speakingThreshold is the audio level (-dBov, lower is louder) at or
	// under which a packet counts as speech.
	speakingThreshold = 50
	// speakingHold is how long without speech before "stopped speaking", so
	// short pauses between words don't flicker the indicator.
	speakingHold = 300 * time.Millisecond
	// speakingCheckInterval drives the stop detection, which also covers a
	// client that stops sending packets entirely.
	speakingCheckInterval = 50 * time.Millisecond

	eventQueueSize = 1024
)

// opusCodec is the only codec negotiated.
var opusCodec = webrtc.RTPCodecCapability{
	MimeType:    webrtc.MimeTypeOpus,
	ClockRate:   48000,
	Channels:    2,
	SDPFmtpLine: "minptime=10;useinbandfec=1",
}

type Config struct {
	UDPPort  int
	PublicIP string // optional, announced in addition to local addresses
	// ConnectTimeout bounds the time between the first offer and an established
	// connection. Zero means defaultConnectTimeout.
	ConnectTimeout time.Duration
}

// SFU implements domain.VoiceMedia.
type SFU struct {
	api            *webrtc.API
	udpConn        net.PacketConn
	signaler       domain.VoiceSignaler
	events         domain.VoiceMediaEvents
	logger         *zap.Logger
	connectTimeout time.Duration

	// eventQueue delivers events to the application one at a time, in order
	// (e.g. speaking on/off must not be reordered).
	eventQueue chan func()
	done       chan struct{}
	closeOnce  sync.Once

	mu    sync.Mutex
	peers map[string]*peer            // by session ID
	rooms map[string]map[string]*peer // channel ID -> session ID -> peer
}

func New(cfg Config, signaler domain.VoiceSignaler, logger *zap.Logger) (*SFU, error) {
	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: opusCodec, PayloadType: 111}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, fmt.Errorf("register opus: %w", err)
	}
	// Clients put their audio level in every packet (RFC 6464): used for
	// server-side speaking detection.
	if err := m.RegisterHeaderExtension(webrtc.RTPHeaderExtensionCapability{URI: sdp.AudioLevelURI}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, fmt.Errorf("register audio level extension: %w", err)
	}

	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, registry); err != nil {
		return nil, fmt.Errorf("register interceptors: %w", err)
	}

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{Port: cfg.UDPPort})
	if err != nil {
		return nil, fmt.Errorf("listen voice udp port %d: %w", cfg.UDPPort, err)
	}

	se := webrtc.SettingEngine{}
	// The server is always reachable on its own address: ICE-Lite, no STUN/TURN.
	se.SetLite(true)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
	se.SetICEUDPMux(webrtc.NewICEUDPMux(logging.NewDefaultLoggerFactory().NewLogger("udpmux"), udpConn))
	if cfg.PublicIP != "" {
		// Append, not replace: LAN clients keep using local addresses.
		if err := se.SetICEAddressRewriteRules(webrtc.ICEAddressRewriteRule{
			External:        []string{cfg.PublicIP},
			AsCandidateType: webrtc.ICECandidateTypeHost,
			Mode:            webrtc.ICEAddressRewriteAppend,
		}); err != nil {
			udpConn.Close()
			return nil, fmt.Errorf("set public ip: %w", err)
		}
	}

	timeout := cfg.ConnectTimeout
	if timeout == 0 {
		timeout = defaultConnectTimeout
	}

	s := &SFU{
		api:            webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(se), webrtc.WithInterceptorRegistry(registry)),
		udpConn:        udpConn,
		signaler:       signaler,
		logger:         logger,
		connectTimeout: timeout,
		eventQueue:     make(chan func(), eventQueueSize),
		done:           make(chan struct{}),
		peers:          make(map[string]*peer),
		rooms:          make(map[string]map[string]*peer),
	}
	go s.dispatchEvents()
	go s.watchSpeaking()
	return s, nil
}

func (s *SFU) dispatchEvents() {
	for {
		select {
		case <-s.done:
			return
		case fn := <-s.eventQueue:
			fn()
		}
	}
}

// watchSpeaking turns off the speaking flag of peers that went quiet.
func (s *SFU) watchSpeaking() {
	ticker := time.NewTicker(speakingCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
		}
		cutoff := time.Now().Add(-speakingHold).UnixNano()
		s.mu.Lock()
		for _, p := range s.peers {
			if p.lastSpeech.Load() < cutoff && p.speaking.CompareAndSwap(true, false) {
				s.emitSpeaking(p, false)
			}
		}
		s.mu.Unlock()
	}
}

// emitSpeaking queues a speaking transition. Dropped (not blocking the media
// path) if the queue is full; the next transition corrects the state.
func (s *SFU) emitSpeaking(p *peer, speaking bool) {
	if s.events == nil {
		return
	}
	event := func() {
		// Ignore events from a peer that was replaced or removed meanwhile.
		if s.peer(p.sessionID) == p {
			s.events.SpeakingChanged(p.sessionID, speaking)
		}
	}
	select {
	case s.eventQueue <- event:
	default:
		s.logger.Warn("voice event queue full, dropping speaking event", zap.String("session", p.sessionID))
	}
}

// SetEvents sets the application callbacks. Must be called before use (it
// breaks the construction cycle between the SFU and the voice service).
func (s *SFU) SetEvents(events domain.VoiceMediaEvents) {
	s.events = events
}

// Close tears down every peer and releases the UDP port.
func (s *SFU) Close() error {
	s.closeOnce.Do(func() { close(s.done) })
	s.mu.Lock()
	peers := make([]*peer, 0, len(s.peers))
	for _, p := range s.peers {
		peers = append(peers, p)
	}
	s.peers = make(map[string]*peer)
	s.rooms = make(map[string]map[string]*peer)
	s.mu.Unlock()

	for _, p := range peers {
		p.close()
	}
	return s.udpConn.Close()
}

// AddPeer implements domain.VoiceMedia. An existing peer for the same session
// is replaced (channel move or reconnect).
func (s *SFU) AddPeer(state domain.VoiceState) error {
	sessionID, channelID := state.SessionID, state.ChannelID
	s.RemovePeer(sessionID)

	p, err := s.newPeer(sessionID, state.UserID, channelID)
	if err != nil {
		return err
	}
	p.apply(state)

	s.mu.Lock()
	room := s.rooms[channelID]
	if room == nil {
		room = make(map[string]*peer)
		s.rooms[channelID] = room
	}
	toNegotiate := []*peer{p}
	for _, other := range room {
		if err := other.subscribe(p); err != nil {
			s.logger.Error("subscribe to new peer", zap.String("session", other.sessionID), zap.Error(err))
		}
		if err := p.subscribe(other); err != nil {
			s.logger.Error("subscribe new peer", zap.String("session", sessionID), zap.Error(err))
		}
		toNegotiate = append(toNegotiate, other)
	}
	room[sessionID] = p
	s.peers[sessionID] = p
	s.mu.Unlock()

	for _, q := range toNegotiate {
		go q.negotiate()
	}
	return nil
}

// RemovePeer implements domain.VoiceMedia.
func (s *SFU) RemovePeer(sessionID string) {
	s.mu.Lock()
	p, ok := s.peers[sessionID]
	if !ok {
		s.mu.Unlock()
		return
	}
	others := s.detachLocked(p)
	s.mu.Unlock()

	p.close()
	for _, q := range others {
		go q.negotiate()
	}
}

// HandleAnswer implements domain.VoiceMedia.
func (s *SFU) HandleAnswer(sessionID, sdp string) error {
	p := s.peer(sessionID)
	if p == nil {
		return fmt.Errorf("%w: no voice peer for session", domain.ErrInvalidSignaling)
	}
	return p.handleAnswer(sdp)
}

// AddICECandidate implements domain.VoiceMedia.
func (s *SFU) AddICECandidate(sessionID string, c domain.ICECandidate) error {
	p := s.peer(sessionID)
	if p == nil {
		return fmt.Errorf("%w: no voice peer for session", domain.ErrInvalidSignaling)
	}
	err := p.pc.AddICECandidate(webrtc.ICECandidateInit{
		Candidate:        c.Candidate,
		SDPMid:           c.SDPMid,
		SDPMLineIndex:    c.SDPMLineIndex,
		UsernameFragment: c.UsernameFragment,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalidSignaling, err)
	}
	return nil
}

// UpdatePeer implements domain.VoiceMedia.
func (s *SFU) UpdatePeer(state domain.VoiceState) {
	if p := s.peer(state.SessionID); p != nil {
		p.apply(state)
	}
}

func (s *SFU) peer(sessionID string) *peer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peers[sessionID]
}

// detachLocked removes p from the maps and from its room's other peers.
// Returns the peers that need renegotiation.
func (s *SFU) detachLocked(p *peer) []*peer {
	delete(s.peers, p.sessionID)
	room := s.rooms[p.channelID]
	delete(room, p.sessionID)
	if len(room) == 0 {
		delete(s.rooms, p.channelID)
	}

	others := make([]*peer, 0, len(room))
	for _, q := range room {
		q.unsubscribe(p) // q stops receiving p
		p.unsubscribe(q) // and q stops feeding p
		others = append(others, q)
	}
	return others
}

// fail removes p (if still current) and reports it to the application.
func (s *SFU) fail(p *peer) {
	s.mu.Lock()
	if s.peers[p.sessionID] != p {
		s.mu.Unlock()
		return
	}
	others := s.detachLocked(p)
	s.mu.Unlock()

	p.close()
	for _, q := range others {
		go q.negotiate()
	}
	if s.events != nil {
		select {
		case s.eventQueue <- func() { s.events.PeerFailed(p.sessionID) }:
		case <-s.done:
		}
	}
}

var errPeerClosed = errors.New("peer closed")
