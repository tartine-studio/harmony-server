package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"

	"github.com/tartine-studio/harmony-server/internal/application"
)

type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

var (
	ErrEmailTaken         = application.ErrEmailTaken
	ErrInvalidCredentials = application.ErrInvalidCredentials
	ErrInvalidToken       = application.ErrInvalidToken
)

var validate = validator.New()

type AuthHandler struct {
	svc    *application.AuthService
	logger *zap.Logger
}

func NewAuthHandler(svc *application.AuthService, logger *zap.Logger) *AuthHandler {
	return &AuthHandler{svc: svc, logger: logger}
}

type registerRequest struct {
	Username string `json:"username" validate:"required,min=2,max=32"`
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8,max=128"`
}

type loginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken" validate:"required"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body", "VALIDATION_ERROR"})
		return
	}

	if err := validate.Struct(req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{formatValidationError(err), "VALIDATION_ERROR"})
		return
	}

	user, err := h.svc.Register(r.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		h.logger.Error("failed to register new user", zap.String("email", req.Email), zap.Error(err))
		if errors.Is(err, ErrEmailTaken) {
			writeJSON(w, http.StatusBadRequest, errorResponse{"registration failed", "REGISTRATION_FAILED"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	h.logger.Info("new user registered", zap.String("username", user.Username), zap.String("id", user.ID))
	writeJSON(w, http.StatusCreated, user)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body", "VALIDATION_ERROR"})
		return
	}

	if err := validate.Struct(req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{formatValidationError(err), "VALIDATION_ERROR"})
		return
	}

	pair, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		h.logger.Warn("failed to login", zap.String("email", req.Email), zap.Error(err))
		if errors.Is(err, ErrInvalidCredentials) {
			writeJSON(w, http.StatusUnauthorized, errorResponse{"invalid email or password", "INVALID_CREDENTIALS"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	h.logger.Info("user logged in", zap.String("email", req.Email))
	writeJSON(w, http.StatusOK, pair)
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body", "VALIDATION_ERROR"})
		return
	}

	if err := validate.Struct(req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{formatValidationError(err), "VALIDATION_ERROR"})
		return
	}

	pair, err := h.svc.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		h.logger.Warn("failed to refresh token", zap.Error(err))
		if errors.Is(err, ErrInvalidToken) {
			writeJSON(w, http.StatusUnauthorized, errorResponse{"invalid or expired refresh token", "INVALID_TOKEN"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error", "INTERNAL_ERROR"})
		return
	}

	h.logger.Info("token refreshed")
	writeJSON(w, http.StatusOK, pair)
}

func formatValidationError(err error) string {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) && len(ve) > 0 {
		fe := ve[0]
		switch fe.Tag() {
		case "required":
			return fe.Field() + " is required"
		case "email":
			return "invalid email format"
		case "min":
			return fe.Field() + " must be at least " + fe.Param() + " characters"
		case "max":
			return fe.Field() + " must be at most " + fe.Param() + " characters"
		}
	}
	return "validation error"
}
