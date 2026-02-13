package http

import (
	"go.uber.org/zap"

	"github.com/tartine-studio/harmony-server/internal/application"
)

type UserHandler struct {
	svc    *application.UserService
	logger *zap.Logger
}

func NewHandler(svc *application.UserService, logger *zap.Logger) *UserHandler {
	return &UserHandler{svc: svc, logger: logger}
}
