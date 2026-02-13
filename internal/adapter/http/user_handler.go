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

type UserHandler struct {
	svc    *application.UserService
	logger *zap.Logger
}

func NewHandler(svc *application.UserService, logger *zap.Logger) *UserHandler {
	return &UserHandler{svc: svc, logger: logger}
}

type updateUserRequest struct {
	Username string `json:"username" validate:"omitempty,min=2,max=32"`
	Email    string `json:"email" validate:"omitempty,email"`
}

func (h *UserHandler) Me(w http.ResponseWriter, r *http.Request) {
	uc, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, errorResponse{"unauthorized", "UNAUTHORIZED"})
		return
	}

	user, err := h.svc.GetByID(r.Context(), uc.UserID)
	if err != nil {
		h.logger.Error("failed to get current user", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	writeJSON(w, http.StatusOK, UserToResponse(user))
}

func (h *UserHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	uc, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, errorResponse{"unauthorized", "UNAUTHORIZED"})
		return
	}

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body", "VALIDATION_ERROR"})
		return
	}
	if err := validate.Struct(req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{formatValidationError(err), "VALIDATION_ERROR"})
		return
	}

	user, err := h.svc.Update(r.Context(), uc.UserID, req.Username, req.Email)
	if err != nil {
		h.logger.Error("failed to update current user", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	h.logger.Info("user updated their profile", zap.String("id", uc.UserID))
	writeJSON(w, http.StatusOK, UserToResponse(user))
}

func (h *UserHandler) GetAll(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.GetAll(r.Context())
	if err != nil {
		h.logger.Error("failed to get all users", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	writeJSON(w, http.StatusOK, UsersToResponse(users))
}

func (h *UserHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	user, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, application.ErrUserNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{"user not found", "NOT_FOUND"})
			return
		}
		h.logger.Error("failed to get user", zap.String("id", id), zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	writeJSON(w, http.StatusOK, UserToResponse(user))
}

func (h *UserHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body", "VALIDATION_ERROR"})
		return
	}
	if err := validate.Struct(req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{formatValidationError(err), "VALIDATION_ERROR"})
		return
	}

	user, err := h.svc.Update(r.Context(), id, req.Username, req.Email)
	if err != nil {
		if errors.Is(err, application.ErrUserNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{"user not found", "NOT_FOUND"})
			return
		}
		h.logger.Error("failed to update user", zap.String("id", id), zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	h.logger.Info("user updated", zap.String("id", id))
	writeJSON(w, http.StatusOK, UserToResponse(user))
}

func (h *UserHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.Delete(r.Context(), id); err != nil {
		if errors.Is(err, application.ErrUserNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{"user not found", "NOT_FOUND"})
			return
		}
		h.logger.Error("failed to delete user", zap.String("id", id), zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	h.logger.Info("user deleted", zap.String("id", id))
	w.WriteHeader(http.StatusNoContent)
}
