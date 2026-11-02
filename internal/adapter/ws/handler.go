package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/tartine-studio/harmony-server/internal/application"
	"github.com/tartine-studio/harmony-server/internal/domain"
)

// Envelope is the top-level WebSocket message structure.
type Envelope struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	RequestID string          `json:"requestId,omitempty"`
}

type sendMessageData struct {
	ChannelID string `json:"channelId"`
	Content   string `json:"content"`
}

type editMessageData struct {
	MessageID string `json:"messageId"`
	Content   string `json:"content"`
}

type deleteMessageData struct {
	MessageID string `json:"messageId"`
}

type messageEvent struct {
	ID        string  `json:"id"`
	UserID    *string `json:"userId"` // null when the author was deleted
	ChannelID string  `json:"channelId"`
	Content   string  `json:"content"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

type messageDeletedEvent struct {
	MessageID string `json:"messageId"`
}

type errorEvent struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // TODO: restrict in production
	},
}

type Handler struct {
	hub         *Hub
	messageSvc  *application.MessageService
	presenceSvc *application.PresenceService
	voiceSvc    *application.VoiceService
	tokenSvc    domain.TokenProvider
	logger      *zap.Logger
}

func NewHandler(hub *Hub, messageSvc *application.MessageService, presenceSvc *application.PresenceService, voiceSvc *application.VoiceService, tokenSvc domain.TokenProvider, logger *zap.Logger) *Handler {
	return &Handler{
		hub:         hub,
		messageSvc:  messageSvc,
		presenceSvc: presenceSvc,
		voiceSvc:    voiceSvc,
		tokenSvc:    tokenSvc,
		logger:      logger,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := extractBearerToken(r)
	if token == "" {
		http.Error(w, "missing authorization", http.StatusUnauthorized)
		return
	}

	claims, err := h.tokenSvc.ValidateToken(token)
	if err != nil || claims.Type != domain.AccessToken {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}

	if err := h.presenceSvc.Connect(r.Context(), claims.UserID); err != nil {
		if errors.Is(err, application.ErrUserNotFound) {
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}
		h.logger.Error("presence connect failed", zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("websocket upgrade failed", zap.Error(err))
		h.presenceSvc.Disconnect(claims.UserID)
		return
	}

	client := NewClient(h.hub, conn, claims.UserID, h.logger)
	// Queue snapshots before registering so later broadcasts arrive after them.
	client.SendJSON(presenceSnapshotFor(client.userID, h.presenceSvc.Snapshot(client.userID)))
	client.SendJSON(voiceStateSnapshot(h.voiceSvc.Snapshot()))
	h.hub.register <- client

	go client.writePump()
	go func() {
		client.readPump(h.dispatch)
		h.voiceSvc.Leave(client.userID, client.id)
		h.presenceSvc.Disconnect(client.userID)
	}()
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth != "" && strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return r.URL.Query().Get("token")
}

func (h *Handler) dispatch(client *Client, raw []byte) {
	// Any frame from the client counts as user activity (keeps them out of idle).
	h.presenceSvc.Touch(client.userID)

	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		h.sendError(client, "", "invalid message format", "PARSE_ERROR")
		return
	}

	switch env.Type {
	case "sendMessage":
		h.handleSendMessage(client, env)
	case "editMessage":
		h.handleEditMessage(client, env)
	case "deleteMessage":
		h.handleDeleteMessage(client, env)
	case "presenceUpdate":
		h.handlePresenceUpdate(client, env)
	case "joinVoice":
		h.handleJoinVoice(client, env)
	case "leaveVoice":
		h.voiceSvc.Leave(client.userID, client.id)
	case "updateVoiceState":
		h.handleUpdateVoiceState(client, env)
	case "voiceAnswer":
		h.handleVoiceAnswer(client, env)
	case "voiceCandidate":
		h.handleVoiceCandidate(client, env)
	case "activity":
		// Activity heartbeat: the Touch above is all it needs.
	default:
		h.sendError(client, env.RequestID, "unknown event type", "UNKNOWN_EVENT")
	}
}

func (h *Handler) handleSendMessage(client *Client, env Envelope) {
	var data sendMessageData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		h.sendError(client, env.RequestID, "invalid data", "VALIDATION_ERROR")
		return
	}
	if data.ChannelID == "" || data.Content == "" {
		h.sendError(client, env.RequestID, "channelId and content are required", "VALIDATION_ERROR")
		return
	}

	message, err := h.messageSvc.Create(context.Background(), client.userID, data.ChannelID, data.Content)
	if err != nil {
		if errors.Is(err, application.ErrChannelNotFound) {
			h.sendError(client, env.RequestID, "channel not found", "NOT_FOUND")
			return
		}
		h.logger.Error("failed to create message via ws", zap.Error(err))
		h.sendError(client, env.RequestID, "failed to send message", "INTERNAL_ERROR")
		return
	}

	h.broadcastEvent("messageCreated", messageToEvent(message))
}

func (h *Handler) handleEditMessage(client *Client, env Envelope) {
	var data editMessageData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		h.sendError(client, env.RequestID, "invalid data", "VALIDATION_ERROR")
		return
	}
	if data.MessageID == "" || data.Content == "" {
		h.sendError(client, env.RequestID, "messageId and content are required", "VALIDATION_ERROR")
		return
	}

	message, err := h.messageSvc.Update(context.Background(), client.userID, data.MessageID, data.Content)
	if err != nil {
		if errors.Is(err, application.ErrMessageNotFound) {
			h.sendError(client, env.RequestID, "message not found", "NOT_FOUND")
			return
		}
		if errors.Is(err, application.ErrMessageForbidden) {
			h.sendError(client, env.RequestID, "forbidden", "FORBIDDEN")
			return
		}
		h.logger.Error("failed to update message via ws", zap.Error(err))
		h.sendError(client, env.RequestID, "failed to edit message", "INTERNAL_ERROR")
		return
	}

	h.broadcastEvent("messageUpdated", messageToEvent(message))
}

func (h *Handler) handleDeleteMessage(client *Client, env Envelope) {
	var data deleteMessageData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		h.sendError(client, env.RequestID, "invalid data", "VALIDATION_ERROR")
		return
	}
	if data.MessageID == "" {
		h.sendError(client, env.RequestID, "messageId is required", "VALIDATION_ERROR")
		return
	}

	if err := h.messageSvc.Delete(context.Background(), client.userID, data.MessageID); err != nil {
		if errors.Is(err, application.ErrMessageNotFound) {
			h.sendError(client, env.RequestID, "message not found", "NOT_FOUND")
			return
		}
		if errors.Is(err, application.ErrMessageForbidden) {
			h.sendError(client, env.RequestID, "forbidden", "FORBIDDEN")
			return
		}
		h.logger.Error("failed to delete message via ws", zap.Error(err))
		h.sendError(client, env.RequestID, "failed to delete message", "INTERNAL_ERROR")
		return
	}

	h.broadcastEvent("messageDeleted", messageDeletedEvent{MessageID: data.MessageID})
}

func (h *Handler) handlePresenceUpdate(client *Client, env Envelope) {
	var data presenceUpdateData
	if err := json.Unmarshal(env.Data, &data); err != nil || data.Invisible == nil {
		h.sendError(client, env.RequestID, "invisible is required", "VALIDATION_ERROR")
		return
	}

	if err := h.presenceSvc.SetInvisible(context.Background(), client.userID, *data.Invisible); err != nil {
		h.logger.Error("failed to update presence via ws", zap.Error(err))
		h.sendError(client, env.RequestID, "failed to update presence", "INTERNAL_ERROR")
	}
}

func (h *Handler) handleJoinVoice(client *Client, env Envelope) {
	var data joinVoiceData
	if err := json.Unmarshal(env.Data, &data); err != nil || data.ChannelID == "" {
		h.sendError(client, env.RequestID, "channelId is required", "VALIDATION_ERROR")
		return
	}

	err := h.voiceSvc.Join(context.Background(), client.userID, client.id, data.ChannelID)
	switch {
	case err == nil:
	case errors.Is(err, application.ErrChannelNotFound):
		h.sendError(client, env.RequestID, "channel not found", "NOT_FOUND")
	case errors.Is(err, application.ErrNotVoiceChannel):
		h.sendError(client, env.RequestID, "not a voice channel", "NOT_VOICE_CHANNEL")
	case errors.Is(err, application.ErrVoiceChannelFull):
		h.sendError(client, env.RequestID, "voice channel is full", "VOICE_CHANNEL_FULL")
	default:
		h.logger.Error("failed to join voice", zap.Error(err))
		h.sendError(client, env.RequestID, "failed to join voice", "INTERNAL_ERROR")
	}
}

func (h *Handler) handleUpdateVoiceState(client *Client, env Envelope) {
	var data updateVoiceStateData
	if err := json.Unmarshal(env.Data, &data); err != nil || (data.Muted == nil && data.Deafened == nil) {
		h.sendError(client, env.RequestID, "muted or deafened is required", "VALIDATION_ERROR")
		return
	}

	if err := h.voiceSvc.SetState(client.userID, client.id, data.Muted, data.Deafened); err != nil {
		if errors.Is(err, application.ErrNotInVoice) {
			h.sendError(client, env.RequestID, "not in a voice channel", "NOT_IN_VOICE")
			return
		}
		h.logger.Error("failed to update voice state", zap.Error(err))
		h.sendError(client, env.RequestID, "failed to update voice state", "INTERNAL_ERROR")
	}
}

func (h *Handler) handleVoiceAnswer(client *Client, env Envelope) {
	var data voiceSDPData
	if err := json.Unmarshal(env.Data, &data); err != nil || data.SDP == "" {
		h.sendError(client, env.RequestID, "sdp is required", "VALIDATION_ERROR")
		return
	}
	h.voiceSignalingResult(client, env, h.voiceSvc.HandleAnswer(client.userID, client.id, data.SDP))
}

func (h *Handler) handleVoiceCandidate(client *Client, env Envelope) {
	var data voiceCandidateData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		h.sendError(client, env.RequestID, "invalid candidate", "VALIDATION_ERROR")
		return
	}
	err := h.voiceSvc.AddICECandidate(client.userID, client.id, domain.ICECandidate{
		Candidate:        data.Candidate,
		SDPMid:           data.SDPMid,
		SDPMLineIndex:    data.SDPMLineIndex,
		UsernameFragment: data.UsernameFragment,
	})
	h.voiceSignalingResult(client, env, err)
}

func (h *Handler) voiceSignalingResult(client *Client, env Envelope, err error) {
	switch {
	case err == nil:
	case errors.Is(err, application.ErrNotInVoice):
		h.sendError(client, env.RequestID, "not in a voice channel", "NOT_IN_VOICE")
	case errors.Is(err, domain.ErrInvalidSignaling):
		h.logger.Debug("invalid voice signaling", zap.String("userID", client.userID), zap.Error(err))
		h.sendError(client, env.RequestID, "invalid signaling", "INVALID_SIGNALING")
	default:
		h.logger.Error("voice signaling failed", zap.Error(err))
		h.sendError(client, env.RequestID, "voice signaling failed", "INTERNAL_ERROR")
	}
}

func (h *Handler) broadcastEvent(eventType string, data any) {
	env := Envelope{
		Type: eventType,
		Data: mustMarshal(data),
	}
	h.hub.BroadcastJSON(mustMarshal(env))
}

func (h *Handler) sendError(client *Client, requestID, message, code string) {
	env := Envelope{
		Type:      "error",
		RequestID: requestID,
		Data:      mustMarshal(errorEvent{Error: message, Code: code}),
	}
	client.SendJSON(mustMarshal(env))
}

func messageToEvent(m *domain.Message) messageEvent {
	return messageEvent{
		ID:        m.ID,
		UserID:    authorID(m.UserID),
		ChannelID: m.ChannelID,
		Content:   m.Content,
		CreatedAt: m.CreatedAt.Format(time.RFC3339),
		UpdatedAt: m.UpdatedAt.Format(time.RFC3339),
	}
}

// authorID maps a deleted author (empty ID) to JSON null.
func authorID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

func mustMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic("json marshal: " + err.Error())
	}
	return data
}
