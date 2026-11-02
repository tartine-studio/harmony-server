package domain

// PresenceStatus is always derived by the server: online/idle from client
// activity, offline from the absence of connections. Users cannot pick it.
type PresenceStatus string

const (
	PresenceOnline  PresenceStatus = "online"
	PresenceIdle    PresenceStatus = "idle"
	PresenceOffline PresenceStatus = "offline"
)

type Presence struct {
	UserID    string
	Status    PresenceStatus
	Invisible bool
}

// PresenceNotifier pushes presence changes to connected clients.
// self is delivered to the user's own sessions (real status + invisible flag).
// public, when non-nil, is delivered to every other client.
type PresenceNotifier interface {
	PublishPresence(self Presence, public *Presence)
}
