package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/tartine-studio/harmony-server/internal/application"
	"github.com/tartine-studio/harmony-server/internal/domain"
)

type ChannelHandler struct {
	svc    *application.ChannelService
	logger *zap.Logger
}

func NewChannelHandler(svc *application.ChannelService, logger *zap.Logger) *ChannelHandler {
	return &ChannelHandler{svc: svc, logger: logger}
}

type createChannelRequest struct {
	Name string `json:"name" validate:"required,min=1,max=100"`
	Type string `json:"type" validate:"required,oneof=text voice"`
}

type updateChannelRequest struct {
	Name string `json:"name" validate:"required,min=1,max=100"`
}

func (h *ChannelHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body", "VALIDATION_ERROR"})
		return
	}
	if err := validate.Struct(req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{formatValidationError(err), "VALIDATION_ERROR"})
		return
	}

	channel, err := h.svc.Create(r.Context(), req.Name, domain.ChannelType(req.Type))
	if err != nil {
		h.logger.Error("failed to create channel", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	h.logger.Info("channel created", zap.String("id", channel.ID), zap.String("name", channel.Name))
	writeJSON(w, http.StatusCreated, ChannelToResponse(channel))
}

func (h *ChannelHandler) GetAll(w http.ResponseWriter, r *http.Request) {
	channels, err := h.svc.GetAll(r.Context())
	if err != nil {
		h.logger.Error("failed to get all channels", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	writeJSON(w, http.StatusOK, ChannelsToResponse(channels))
}

func (h *ChannelHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	channel, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, application.ErrChannelNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{"channel not found", "NOT_FOUND"})
			return
		}
		h.logger.Error("failed to get channel", zap.String("id", id), zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	writeJSON(w, http.StatusOK, ChannelToResponse(channel))
}

func (h *ChannelHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req updateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body", "VALIDATION_ERROR"})
		return
	}
	if err := validate.Struct(req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{formatValidationError(err), "VALIDATION_ERROR"})
		return
	}

	channel, err := h.svc.Update(r.Context(), id, req.Name)
	if err != nil {
		if errors.Is(err, application.ErrChannelNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{"channel not found", "NOT_FOUND"})
			return
		}
		h.logger.Error("failed to update channel", zap.String("id", id), zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	h.logger.Info("channel updated", zap.String("id", id))
	writeJSON(w, http.StatusOK, ChannelToResponse(channel))
}

func (h *ChannelHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.Delete(r.Context(), id); err != nil {
		if errors.Is(err, application.ErrChannelNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{"channel not found", "NOT_FOUND"})
			return
		}
		h.logger.Error("failed to delete channel", zap.String("id", id), zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	h.logger.Info("channel deleted", zap.String("id", id))
	w.WriteHeader(http.StatusNoContent)
}
