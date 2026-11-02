package ws

import "github.com/tartine-studio/harmony-server/internal/domain"

type presenceEvent struct {
	UserID    string `json:"userId"`
	Status    string `json:"status"`
	Invisible *bool  `json:"invisible,omitempty"` // only sent to the user's own sessions
}

type presenceSnapshotEvent struct {
	Presences []presenceEvent `json:"presences"`
}

type presenceUpdateData struct {
	Invisible *bool `json:"invisible"`
}

func toPresenceEvent(p domain.Presence, self bool) presenceEvent {
	ev := presenceEvent{UserID: p.UserID, Status: string(p.Status)}
	if self {
		inv := p.Invisible
		ev.Invisible = &inv
	}
	return ev
}

func presenceSnapshotFor(viewerID string, presences []domain.Presence) []byte {
	events := make([]presenceEvent, len(presences))
	for i, p := range presences {
		events[i] = toPresenceEvent(p, p.UserID == viewerID)
	}
	return envelope("presenceSnapshot", presenceSnapshotEvent{Presences: events})
}

// PublishPresence implements domain.PresenceNotifier.
func (h *Hub) PublishPresence(self domain.Presence, public *domain.Presence) {
	msg := userMessage{
		userID: self.UserID,
		self:   envelope("presenceBroadcast", toPresenceEvent(self, true)),
	}
	if public != nil {
		msg.others = envelope("presenceBroadcast", toPresenceEvent(*public, false))
	}
	h.targeted <- msg
}

func envelope(eventType string, data any) []byte {
	return mustMarshal(Envelope{Type: eventType, Data: mustMarshal(data)})
}
