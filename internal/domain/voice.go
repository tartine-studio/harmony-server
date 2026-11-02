package domain

// VoiceState is a user's presence in a voice channel. A user has at most one
// voice session (one device) at a time.
type VoiceState struct {
	UserID    string
	SessionID string // WebSocket session that joined; never exposed to other users
	ChannelID string // empty when the user just left voice
	SelfMuted bool
	Deafened  bool
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
}
