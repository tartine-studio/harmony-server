package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/tartine-studio/harmony-server/internal/adapter/http/middleware"
	"github.com/tartine-studio/harmony-server/internal/application"
)

type MessageHandler struct {
	svc    *application.MessageService
	logger *zap.Logger
}

func NewMessageHandler(svc *application.MessageService, logger *zap.Logger) *MessageHandler {
	return &MessageHandler{svc: svc, logger: logger}
}

type createMessageRequest struct {
	Content string `json:"content" validate:"required,min=1,max=2000"`
}

type updateMessageRequest struct {
	Content string `json:"content" validate:"required,min=1,max=2000"`
}

func (h *MessageHandler) GetByChannelID(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")

	messages, err := h.svc.GetByChannelID(r.Context(), channelID)
	if err != nil {
		if errors.Is(err, application.ErrChannelNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{"channel not found", "NOT_FOUND"})
			return
		}
		h.logger.Error("failed to get messages", zap.String("channelID", channelID), zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	writeJSON(w, http.StatusOK, MessagesToResponse(messages))
}

func (h *MessageHandler) Create(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	uc, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, errorResponse{"unauthorized", "UNAUTHORIZED"})
		return
	}

	var req createMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body", "VALIDATION_ERROR"})
		return
	}
	if err := validate.Struct(req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{formatValidationError(err), "VALIDATION_ERROR"})
		return
	}

	message, err := h.svc.Create(r.Context(), uc.UserID, channelID, req.Content)
	if err != nil {
		if errors.Is(err, application.ErrChannelNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{"channel not found", "NOT_FOUND"})
			return
		}
		h.logger.Error("failed to create message", zap.String("channelID", channelID), zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	h.logger.Info("message created", zap.String("id", message.ID), zap.String("channelID", channelID))
	writeJSON(w, http.StatusCreated, MessageToResponse(message))
}

func (h *MessageHandler) Update(w http.ResponseWriter, r *http.Request) {
	messageID := chi.URLParam(r, "messageID")
	uc, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, errorResponse{"unauthorized", "UNAUTHORIZED"})
		return
	}

	var req updateMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body", "VALIDATION_ERROR"})
		return
	}
	if err := validate.Struct(req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{formatValidationError(err), "VALIDATION_ERROR"})
		return
	}

	message, err := h.svc.Update(r.Context(), uc.UserID, messageID, req.Content)
	if err != nil {
		if errors.Is(err, application.ErrMessageNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{"message not found", "NOT_FOUND"})
			return
		}
		if errors.Is(err, application.ErrMessageForbidden) {
			writeJSON(w, http.StatusForbidden, errorResponse{"forbidden", "FORBIDDEN"})
			return
		}
		h.logger.Error("failed to update message", zap.String("id", messageID), zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	h.logger.Info("message updated", zap.String("id", messageID))
	writeJSON(w, http.StatusOK, MessageToResponse(message))
}

func (h *MessageHandler) Delete(w http.ResponseWriter, r *http.Request) {
	messageID := chi.URLParam(r, "messageID")
	uc, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, errorResponse{"unauthorized", "UNAUTHORIZED"})
		return
	}

	if err := h.svc.Delete(r.Context(), uc.UserID, messageID); err != nil {
		if errors.Is(err, application.ErrMessageNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{"message not found", "NOT_FOUND"})
			return
		}
		if errors.Is(err, application.ErrMessageForbidden) {
			writeJSON(w, http.StatusForbidden, errorResponse{"forbidden", "FORBIDDEN"})
			return
		}
		h.logger.Error("failed to delete message", zap.String("id", messageID), zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	h.logger.Info("message deleted", zap.String("id", messageID))
	w.WriteHeader(http.StatusNoContent)
}
