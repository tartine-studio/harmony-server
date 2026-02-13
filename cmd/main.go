package main

import (
	"fmt"
	"net/http"
	"path/filepath"

	"go.uber.org/zap"

	httphandler "github.com/tartine-studio/harmony-server/internal/adapter/http"
	"github.com/tartine-studio/harmony-server/internal/adapter/repository"
	"github.com/tartine-studio/harmony-server/internal/adapter/token"
	"github.com/tartine-studio/harmony-server/internal/application"
	"github.com/tartine-studio/harmony-server/internal/config"
)

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}

	db, err := repository.Open(filepath.Join(cfg.DataDir, "harmony.db"))
	if err != nil {
		logger.Fatal("failed to open database", zap.Error(err))
	}
	defer db.Close()

	jwtSvc := token.NewJwtService(cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	userRepo := repository.NewUserRepository(db)
	authSvc := application.NewAuthService(userRepo, jwtSvc)
	authHandler := httphandler.NewAuthHandler(authSvc, logger)
	userSvc := application.NewUserService(userRepo)
	userHandler := httphandler.NewHandler(userSvc, logger)

	channelRepo := repository.NewChannelRepository(db)
	channelSvc := application.NewChannelService(channelRepo)
	channelHandler := httphandler.NewChannelHandler(channelSvc, logger)

	router := httphandler.NewRouter(httphandler.Dependencies{
		AuthHandler:    authHandler,
		UserHandler:    userHandler,
		ChannelHandler: channelHandler,
		JWTService:     jwtSvc,
		Logger:         logger,
	})

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	logger.Info("starting server", zap.String("addr", addr))

	if err := http.ListenAndServe(addr, router); err != nil {
		logger.Fatal("server failed", zap.Error(err))
	}
}
