package ws

import "go.uber.org/zap"

// userMessage is delivered as self to every session of userID and as others
// (when non-nil) to every other client.
type userMessage struct {
	userID string
	self   []byte
	others []byte
}

// sessionMessage is delivered to a single WebSocket session.
type sessionMessage struct {
	sessionID string
	data      []byte
}

type Hub struct {
	clients    map[*Client]bool
	sessions   map[string]*Client
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	targeted   chan userMessage
	direct     chan sessionMessage
	logger     *zap.Logger
}

func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		sessions:   make(map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 256),
		targeted:   make(chan userMessage, 256),
		direct:     make(chan sessionMessage, 256),
		logger:     logger,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			h.sessions[client.id] = client
			h.logger.Info("client connected", zap.String("userID", client.userID))

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				h.remove(client)
				h.logger.Info("client disconnected", zap.String("userID", client.userID))
			}

		case message := <-h.broadcast:
			for client := range h.clients {
				h.deliver(client, message)
			}

		case msg := <-h.direct:
			if client, ok := h.sessions[msg.sessionID]; ok {
				h.deliver(client, msg.data)
			}

		case msg := <-h.targeted:
			for client := range h.clients {
				data := msg.others
				if client.userID == msg.userID {
					data = msg.self
				}
				if data != nil {
					h.deliver(client, data)
				}
			}
		}
	}
}

// deliver queues data for client, dropping clients whose buffer is full.
func (h *Hub) deliver(client *Client, data []byte) {
	if !client.SendJSON(data) {
		h.remove(client)
	}
}

func (h *Hub) remove(client *Client) {
	delete(h.clients, client)
	delete(h.sessions, client.id)
	client.close()
}

// SendToSession queues data for a single WebSocket session, if still connected.
func (h *Hub) SendToSession(sessionID string, data []byte) {
	h.direct <- sessionMessage{sessionID: sessionID, data: data}
}

func (h *Hub) BroadcastJSON(data []byte) {
	h.broadcast <- data
}
