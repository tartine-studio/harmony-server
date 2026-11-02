package ws

import "go.uber.org/zap"

type Hub struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	logger     *zap.Logger
}

func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 256),
		logger:     logger,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			h.logger.Info("client connected", zap.String("userID", client.userID))

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.close()
				h.logger.Info("client disconnected", zap.String("userID", client.userID))
			}

		case message := <-h.broadcast:
			for client := range h.clients {
				if !client.SendJSON(message) {
					delete(h.clients, client)
					client.close()
				}
			}
		}
	}
}

func (h *Hub) BroadcastJSON(data []byte) {
	h.broadcast <- data
}
