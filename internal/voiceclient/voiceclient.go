// Package voiceclient is a minimal headless Harmony voice client: the WebRTC
// side of the voice protocol (answering server offers, trickling candidates,
// sending Opus RTP with an RFC 6464 audio level, counting received packets).
//
// It is transport-agnostic: signaling messages go out through a Signaler and
// incoming voiceOffer SDPs are fed to HandleOffer. Used by cmd/voicebot and
// the end-to-end tests, and meant as a reference for client implementers.
package voiceclient

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
)

// LevelSilent is the RFC 6464 audio level for silence (-127 dBov).
const LevelSilent uint8 = 127

// opusSilence is a valid 20ms Opus frame. The SFU never decodes audio, so its
// content does not matter; only the RTP flow and the audio level do.
var opusSilence = []byte{0xf8, 0xff, 0xfe}

var opusCodec = webrtc.RTPCodecCapability{
	MimeType:    webrtc.MimeTypeOpus,
	ClockRate:   48000,
	Channels:    2,
	SDPFmtpLine: "minptime=10;useinbandfec=1",
}

// Signaler sends a client → server WebSocket event (voiceAnswer,
// voiceCandidate). data is marshalled as the envelope's "data" field.
type Signaler func(eventType string, data any)

// Client is one voice session's peer connection.
type Client struct {
	pc     *webrtc.PeerConnection
	send   Signaler
	mic    *webrtc.TrackLocalStaticRTP // nil when not speaking
	level  atomic.Uint32               // audio level sent with each packet
	logf   func(format string, args ...any)
	debug  func(format string, args ...any)
	micMu  sync.Mutex
	micSet bool

	pendMu   sync.Mutex
	answered bool
	pending  []webrtc.ICECandidateInit

	counts sync.Map // speaker userID -> *atomic.Int64
}

// Options configures a Client.
type Options struct {
	// Speak adds a microphone track (attached to the server's recvonly slot
	// on the first offer). Audio is only sent while SendAudio runs.
	Speak bool
	// Level is the initial audio level (-dBov, 0 loudest, 127 silent).
	Level uint8
	// Logf receives connection state changes and new incoming tracks. Optional.
	Logf func(format string, args ...any)
	// DebugSDP also sends offers and local ICE candidates to Logf.
	DebugSDP bool
}

// New creates the peer connection. Nothing is sent until the first offer.
func New(send Signaler, opts Options) (*Client, error) {
	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: opusCodec, PayloadType: 111}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, fmt.Errorf("register opus: %w", err)
	}
	if err := m.RegisterHeaderExtension(webrtc.RTPHeaderExtensionCapability{URI: sdp.AudioLevelURI}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, fmt.Errorf("register audio level: %w", err)
	}
	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, registry); err != nil {
		return nil, fmt.Errorf("register interceptors: %w", err)
	}
	pc, err := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(registry)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, fmt.Errorf("new peer connection: %w", err)
	}

	c := &Client{pc: pc, send: send, logf: opts.Logf}
	if c.logf == nil {
		c.logf = func(string, ...any) {}
	}
	c.debug = func(string, ...any) {}
	if opts.DebugSDP {
		c.debug = c.logf
	}
	c.level.Store(uint32(opts.Level))

	if opts.Speak {
		mic, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "mic", "voiceclient")
		if err != nil {
			pc.Close()
			return nil, fmt.Errorf("new mic track: %w", err)
		}
		c.mic = mic
	}

	pc.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			return
		}
		init := cand.ToJSON()
		c.debug("local candidate: %s", init.Candidate)
		c.pendMu.Lock()
		defer c.pendMu.Unlock()
		// The answer must reach the server before any candidate.
		if c.answered {
			c.send("voiceCandidate", init)
		} else {
			c.pending = append(c.pending, init)
		}
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		c.logf("peer connection: %s", s)
	})
	pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		// The server sets each forwarded track's msid stream ID to the
		// speaker's userId.
		userID := t.StreamID()
		c.logf("receiving audio track from %s", userID)
		n, _ := c.counts.LoadOrStore(userID, &atomic.Int64{})
		for {
			if _, _, err := t.ReadRTP(); err != nil {
				return
			}
			n.(*atomic.Int64).Add(1)
		}
	})
	return c, nil
}

// HandleOffer applies a voiceOffer from the server (initial or renegotiation)
// and sends back the voiceAnswer, then any candidates gathered meanwhile.
func (c *Client) HandleOffer(offer string) error {
	c.debug("offer:\n%s", offer)
	if err := c.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		return fmt.Errorf("set remote: %w", err)
	}
	c.micMu.Lock()
	if c.mic != nil && !c.micSet {
		// Attaches to the recvonly slot the server offered for our mic.
		if _, err := c.pc.AddTrack(c.mic); err != nil {
			c.micMu.Unlock()
			return fmt.Errorf("add mic: %w", err)
		}
		c.micSet = true
	}
	c.micMu.Unlock()

	answer, err := c.pc.CreateAnswer(nil)
	if err != nil {
		return fmt.Errorf("create answer: %w", err)
	}
	if err := c.pc.SetLocalDescription(answer); err != nil {
		return fmt.Errorf("set local: %w", err)
	}

	c.pendMu.Lock()
	defer c.pendMu.Unlock()
	c.send("voiceAnswer", map[string]string{"sdp": c.pc.LocalDescription().SDP})
	c.answered = true
	for _, cand := range c.pending {
		c.send("voiceCandidate", cand)
	}
	c.pending = nil
	return nil
}

// SetLevel changes the audio level sent with the next packets (0 loudest,
// LevelSilent for silence).
func (c *Client) SetLevel(level uint8) {
	c.level.Store(uint32(level))
}

// SendAudio sends one Opus packet every 20ms while connected, until ctx is
// done. Requires Options.Speak.
func (c *Client) SendAudio(ctx context.Context) {
	if c.mic == nil {
		return
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var seq uint16
	var ts uint32
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if c.pc.ConnectionState() != webrtc.PeerConnectionStateConnected {
			continue
		}
		level := uint8(c.level.Load())
		pkt := &rtp.Packet{
			Header:  rtp.Header{Version: 2, PayloadType: 111, SequenceNumber: seq, Timestamp: ts},
			Payload: opusSilence,
		}
		if id := c.audioLevelExtID(); id != 0 {
			ext, _ := rtp.AudioLevelExtension{Level: level, Voice: level < LevelSilent}.Marshal()
			_ = pkt.Header.SetExtension(id, ext)
		}
		if err := c.mic.WriteRTP(pkt); err != nil {
			c.logf("write rtp: %v", err)
		}
		seq++
		ts += 960
	}
}

// audioLevelExtID returns the negotiated ID of the audio-level extension.
func (c *Client) audioLevelExtID() uint8 {
	for _, t := range c.pc.GetTransceivers() {
		if s := t.Sender(); s != nil && s.Track() == c.mic {
			for _, e := range s.GetParameters().HeaderExtensions {
				if e.URI == sdp.AudioLevelURI {
					return uint8(e.ID)
				}
			}
		}
	}
	return 0
}

// Received returns how many RTP packets were received from userID.
func (c *Client) Received(userID string) int64 {
	if n, ok := c.counts.Load(userID); ok {
		return n.(*atomic.Int64).Load()
	}
	return 0
}

// Counts returns the number of packets received per speaker userID.
func (c *Client) Counts() map[string]int64 {
	out := map[string]int64{}
	c.counts.Range(func(k, v any) bool {
		out[k.(string)] = v.(*atomic.Int64).Load()
		return true
	})
	return out
}

// Connected reports whether the peer connection is established.
func (c *Client) Connected() bool {
	return c.pc.ConnectionState() == webrtc.PeerConnectionStateConnected
}

// Close tears down the peer connection.
func (c *Client) Close() error {
	return c.pc.Close()
}
