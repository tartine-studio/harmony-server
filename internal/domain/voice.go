package domain

import "errors"

// VoiceState is a user's presence in a voice channel. A user has at most one
// voice session (one device) at a time.
type VoiceState struct {
	UserID    string
	SessionID string // WebSocket session that joined; never exposed to other users
	ChannelID string // empty when the user just left voice
	SelfMuted bool
	Deafened  bool
	Speaking  bool // server-detected from the RTP audio level; never true while muted
}

// Muted is the effective mute state: deafened users are always muted.
func (s VoiceState) Muted() bool {
	return s.SelfMuted || s.Deafened
}

type VoiceDisconnectReason string

const (
	VoiceDisconnectOtherDevice      VoiceDisconnectReason = "otherDevice"
	VoiceDisconnectChannelDeleted   VoiceDisconnectReason = "channelDeleted"
	VoiceDisconnectConnectionFailed VoiceDisconnectReason = "connectionFailed"
)

// VoiceNotifier pushes voice state changes to connected clients.
type VoiceNotifier interface {
	// PublishVoiceState tells every client about a join, leave (empty ChannelID)
	// or mute/deafen change.
	PublishVoiceState(state VoiceState)
	// NotifyVoiceDisconnected tells a single session it was removed from voice.
	NotifyVoiceDisconnected(sessionID string, reason VoiceDisconnectReason)
	// PublishSpeaking tells every client a user started or stopped speaking.
	PublishSpeaking(userID string, speaking bool)
}

// ErrInvalidSignaling is returned for SDP/ICE messages the media plane rejects
// (malformed SDP, answer without a pending offer, unknown session...).
var ErrInvalidSignaling = errors.New("invalid signaling")

// ICECandidate is a trickled ICE candidate from a client.
type ICECandidate struct {
	Candidate        string
	SDPMid           *string
	SDPMLineIndex    *uint16
	UsernameFragment *string
}

// VoiceMedia is the media plane port (the SFU): one peer per voice session.
// Implementations must not call back into the application synchronously.
type VoiceMedia interface {
	// AddPeer creates the peer for state.SessionID in state.ChannelID and sends
	// it an offer, applying state's mute/deafen. An existing peer for the same
	// session is replaced.
	AddPeer(state VoiceState) error
	RemovePeer(sessionID string)
	HandleAnswer(sessionID, sdp string) error
	AddICECandidate(sessionID string, candidate ICECandidate) error
	// UpdatePeer applies mute/deafen: a muted peer's audio is dropped (nothing
	// leaves a muted mic, whatever the client sends), a deafened peer receives
	// nothing.
	UpdatePeer(state VoiceState)
}

// VoiceSignaler delivers server-initiated signaling to a single session.
type VoiceSignaler interface {
	SendVoiceOffer(sessionID, sdp string)
}

// VoiceMediaEvents is how the media plane reports back to the application.
// Called asynchronously.
type VoiceMediaEvents interface {
	// PeerFailed reports a peer that failed or never connected; it has already
	// been removed from the media plane.
	PeerFailed(sessionID string)
	// SpeakingChanged reports a speaking transition detected on the session's
	// audio. Events for a session are delivered in order.
	SpeakingChanged(sessionID string, speaking bool)
}
