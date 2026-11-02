package sfu

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
	"go.uber.org/zap"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

// subscription is another participant's audio sent to this peer, on a
// dedicated track whose stream ID is the source user ID (msid) so clients can
// map each received track to a user.
type subscription struct {
	sender *webrtc.RTPSender
	track  *webrtc.TrackLocalStaticRTP
}

// fanoutTarget is a subscriber of this peer's audio.
type fanoutTarget struct {
	dst   *peer
	track *webrtc.TrackLocalStaticRTP
}

// peer is one voice session: one PeerConnection that receives the user's mic
// and sends one track per other participant in the channel.
type peer struct {
	sfu       *SFU
	sessionID string
	userID    string
	channelID string
	pc        *webrtc.PeerConnection

	connectTimer *time.Timer
	muted        atomic.Bool // muted peers' audio is dropped, never forwarded
	deafened     atomic.Bool // deafened peers receive nothing

	speaking   atomic.Bool
	lastSpeech atomic.Int64 // unix nanos of the last packet above the speaking threshold

	// fanout lists where this peer's audio goes. Copy-on-write, only mutated
	// under sfu.mu; read lock-free on every packet.
	fanout atomic.Pointer[[]fanoutTarget]

	mu            sync.Mutex               // guards the fields below
	subscriptions map[string]*subscription // by source session ID
	closed        bool

	negMu       sync.Mutex // serializes offer/answer exchanges
	negotiating bool       // an offer was sent, waiting for the answer
	pending     bool       // another negotiation is needed once the answer arrives
}

func (s *SFU) newPeer(sessionID, userID, channelID string) (*peer, error) {
	pc, err := s.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, fmt.Errorf("new peer connection: %w", err)
	}

	// Slot for the user's microphone.
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		pc.Close()
		return nil, fmt.Errorf("add mic transceiver: %w", err)
	}

	p := &peer{
		sfu:           s,
		sessionID:     sessionID,
		userID:        userID,
		channelID:     channelID,
		pc:            pc,
		subscriptions: make(map[string]*subscription),
	}
	p.fanout.Store(&[]fanoutTarget{})

	pc.OnTrack(p.forward)
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		s.logger.Debug("voice peer state", zap.String("session", sessionID), zap.String("state", state.String()))
		switch state {
		case webrtc.PeerConnectionStateConnected:
			p.connectTimer.Stop()
		case webrtc.PeerConnectionStateFailed:
			s.fail(p)
		}
	})
	p.connectTimer = time.AfterFunc(s.connectTimeout, func() {
		if pc.ConnectionState() != webrtc.PeerConnectionStateConnected {
			s.logger.Warn("voice peer did not connect in time", zap.String("session", sessionID))
			s.fail(p)
		}
	})
	return p, nil
}

// forward copies every RTP packet from the user's mic to each subscriber that
// is not deafened.
func (p *peer) forward(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	if remote.Kind() != webrtc.RTPCodecTypeAudio {
		return
	}
	p.sfu.logger.Info("voice track started", zap.String("session", p.sessionID), zap.String("user", p.userID))

	levelExtID := audioLevelExtID(receiver)
	if levelExtID == 0 {
		p.sfu.logger.Warn("client did not negotiate the audio level extension, speaking detection disabled",
			zap.String("session", p.sessionID))
	}

	for {
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				p.sfu.logger.Debug("voice track ended", zap.String("session", p.sessionID), zap.Error(err))
			}
			return
		}
		// Server-side guarantee: nothing leaves a muted mic, even if the client
		// keeps sending (bug, race with the mute toggle...).
		if p.muted.Load() {
			continue
		}
		if levelExtID != 0 {
			p.detectSpeech(pkt, levelExtID)
		}
		for _, t := range *p.fanout.Load() {
			if t.dst.deafened.Load() {
				continue
			}
			if err := t.track.WriteRTP(pkt); err != nil && !errors.Is(err, io.ErrClosedPipe) {
				p.sfu.logger.Debug("forward rtp", zap.String("session", t.dst.sessionID), zap.Error(err))
			}
		}
	}
}

// apply sets the mute/deafen flags read by the forwarding loops.
func (p *peer) apply(state domain.VoiceState) {
	p.muted.Store(state.Muted())
	p.deafened.Store(state.Deafened)
}

func (p *peer) detectSpeech(pkt *rtp.Packet, extID uint8) {
	raw := pkt.GetExtension(extID)
	if raw == nil {
		return
	}
	var level rtp.AudioLevelExtension
	if err := level.Unmarshal(raw); err != nil || level.Level > speakingThreshold {
		return
	}
	p.lastSpeech.Store(time.Now().UnixNano())
	if p.speaking.CompareAndSwap(false, true) {
		p.sfu.emitSpeaking(p, true)
	}
}

func audioLevelExtID(receiver *webrtc.RTPReceiver) uint8 {
	for _, ext := range receiver.GetParameters().HeaderExtensions {
		if ext.URI == sdp.AudioLevelURI {
			return uint8(ext.ID)
		}
	}
	return 0
}

// subscribe makes p receive src's audio (effective at the next negotiation).
// Must be called with sfu.mu held.
func (p *peer) subscribe(src *peer) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errPeerClosed
	}

	track, err := webrtc.NewTrackLocalStaticRTP(opusCodec, "audio", src.userID)
	if err != nil {
		return fmt.Errorf("new local track: %w", err)
	}
	sender, err := p.pc.AddTrack(track)
	if err != nil {
		return fmt.Errorf("add track: %w", err)
	}
	p.subscriptions[src.sessionID] = &subscription{sender: sender, track: track}
	src.setFanout(append(src.targets(), fanoutTarget{dst: p, track: track}))

	// Drain RTCP so interceptors (receiver reports...) keep working.
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(buf); err != nil {
				return
			}
		}
	}()
	return nil
}

// unsubscribe stops sending src's audio to p. Must be called with sfu.mu held.
func (p *peer) unsubscribe(src *peer) {
	targets := src.targets()
	kept := make([]fanoutTarget, 0, len(targets))
	for _, t := range targets {
		if t.dst != p {
			kept = append(kept, t)
		}
	}
	src.setFanout(kept)

	p.mu.Lock()
	defer p.mu.Unlock()
	sub, ok := p.subscriptions[src.sessionID]
	if !ok {
		return
	}
	delete(p.subscriptions, src.sessionID)
	if p.closed {
		return
	}
	if err := p.pc.RemoveTrack(sub.sender); err != nil {
		p.sfu.logger.Debug("remove track", zap.String("session", p.sessionID), zap.Error(err))
	}
}

// targets returns a copy of the fanout list.
func (p *peer) targets() []fanoutTarget {
	cur := *p.fanout.Load()
	return append(make([]fanoutTarget, 0, len(cur)+1), cur...)
}

func (p *peer) setFanout(targets []fanoutTarget) {
	p.fanout.Store(&targets)
}

// negotiate sends a new offer, or queues one if an exchange is in flight.
func (p *peer) negotiate() {
	p.negMu.Lock()
	defer p.negMu.Unlock()

	if p.negotiating {
		p.pending = true
		return
	}
	if err := p.sendOfferLocked(); err != nil && !errors.Is(err, errPeerClosed) {
		p.sfu.logger.Error("voice negotiation failed", zap.String("session", p.sessionID), zap.Error(err))
		p.sfu.fail(p)
	}
}

func (p *peer) sendOfferLocked() error {
	if p.isClosed() {
		return errPeerClosed
	}

	offer, err := p.pc.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("create offer: %w", err)
	}
	gathered := webrtc.GatheringCompletePromise(p.pc)
	if err := p.pc.SetLocalDescription(offer); err != nil {
		return fmt.Errorf("set local description: %w", err)
	}
	// Server candidates are known up front (single UDP port): ship them inside
	// the offer instead of trickling.
	select {
	case <-gathered:
	case <-time.After(gatheringTimeout):
		p.sfu.logger.Warn("ice gathering timeout, sending partial offer", zap.String("session", p.sessionID))
	}

	p.negotiating = true
	p.sfu.signaler.SendVoiceOffer(p.sessionID, p.pc.LocalDescription().SDP)
	return nil
}

func (p *peer) handleAnswer(sdp string) error {
	p.negMu.Lock()
	defer p.negMu.Unlock()

	if !p.negotiating {
		return fmt.Errorf("%w: no pending offer", domain.ErrInvalidSignaling)
	}
	if err := p.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalidSignaling, err)
	}
	p.negotiating = false

	if p.pending {
		p.pending = false
		if err := p.sendOfferLocked(); err != nil && !errors.Is(err, errPeerClosed) {
			p.sfu.logger.Error("voice renegotiation failed", zap.String("session", p.sessionID), zap.Error(err))
			p.sfu.fail(p)
		}
	}
	return nil
}

func (p *peer) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *peer) close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()

	p.connectTimer.Stop()
	if err := p.pc.Close(); err != nil {
		p.sfu.logger.Debug("close peer connection", zap.String("session", p.sessionID), zap.Error(err))
	}
}
