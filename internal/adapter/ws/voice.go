package ws

import "github.com/tartine-studio/harmony-server/internal/domain"

type joinVoiceData struct {
	ChannelID string `json:"channelId"`
}

type updateVoiceStateData struct {
	Muted    *bool `json:"muted"`
	Deafened *bool `json:"deafened"`
}

type voiceSDPData struct {
	SDP string `json:"sdp"`
}

type voiceCandidateData struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdpMid"`
	SDPMLineIndex    *uint16 `json:"sdpMLineIndex"`
	UsernameFragment *string `json:"usernameFragment"`
}

type voiceStateEvent struct {
	UserID    string  `json:"userId"`
	ChannelID *string `json:"channelId"` // null when the user left voice
	Muted     bool    `json:"muted"`     // effective: true when deafened
	SelfMuted bool    `json:"selfMuted"`
	Deafened  bool    `json:"deafened"`
}

type voiceStateSnapshotEvent struct {
	States []voiceStateEvent `json:"states"`
}

type voiceDisconnectedEvent struct {
	Reason string `json:"reason"`
}

func toVoiceStateEvent(s domain.VoiceState) voiceStateEvent {
	ev := voiceStateEvent{
		UserID:    s.UserID,
		Muted:     s.Muted(),
		SelfMuted: s.SelfMuted,
		Deafened:  s.Deafened,
	}
	if s.ChannelID != "" {
		ch := s.ChannelID
		ev.ChannelID = &ch
	}
	return ev
}

func voiceStateSnapshot(states []domain.VoiceState) []byte {
	events := make([]voiceStateEvent, len(states))
	for i, s := range states {
		events[i] = toVoiceStateEvent(s)
	}
	return envelope("voiceStateSnapshot", voiceStateSnapshotEvent{States: events})
}

// PublishVoiceState implements domain.VoiceNotifier.
func (h *Hub) PublishVoiceState(state domain.VoiceState) {
	h.BroadcastJSON(envelope("voiceStateUpdate", toVoiceStateEvent(state)))
}

// SendVoiceOffer implements domain.VoiceSignaler.
func (h *Hub) SendVoiceOffer(sessionID, sdp string) {
	h.SendToSession(sessionID, envelope("voiceOffer", voiceSDPData{SDP: sdp}))
}

// NotifyVoiceDisconnected implements domain.VoiceNotifier.
func (h *Hub) NotifyVoiceDisconnected(sessionID string, reason domain.VoiceDisconnectReason) {
	h.SendToSession(sessionID, envelope("voiceDisconnected", voiceDisconnectedEvent{Reason: string(reason)}))
}
