// Package server wires every adapter and service into a ready-to-serve
// http.Handler. It is the composition root shared by cmd/main.go and the
// end-to-end tests.
package server

import (
	"context"
	"database/sql"
	"net/http"
	"path/filepath"

	"go.uber.org/zap"

	httphandler "github.com/tartine-studio/harmony-server/internal/adapter/http"
	"github.com/tartine-studio/harmony-server/internal/adapter/repository"
	"github.com/tartine-studio/harmony-server/internal/adapter/sfu"
	"github.com/tartine-studio/harmony-server/internal/adapter/token"
	wshandler "github.com/tartine-studio/harmony-server/internal/adapter/ws"
	"github.com/tartine-studio/harmony-server/internal/application"
	"github.com/tartine-studio/harmony-server/internal/config"
)

// Server is a fully wired Harmony instance.
type Server struct {
	// Handler serves the REST API and the WebSocket endpoint.
	Handler http.Handler

	db     *sql.DB
	voice  *sfu.SFU
	cancel context.CancelFunc
}

// New opens the database in cfg.DataDir (running migrations), starts the
// voice SFU on cfg.VoiceUDPPort and the background loops. Call Close to
// release everything.
func New(cfg config.Config, logger *zap.Logger) (*Server, error) {
	db, err := repository.Open(filepath.Join(cfg.DataDir, "harmony.db"))
	if err != nil {
		return nil, err
	}

	jwtSvc := token.NewJwtService(cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	userRepo := repository.NewUserRepository(db)
	channelRepo := repository.NewChannelRepository(db)
	messageRepo := repository.NewMessageRepository(db)

	hub := wshandler.NewHub(logger)
	go hub.Run()

	voiceMedia, err := sfu.New(sfu.Config{UDPPort: cfg.VoiceUDPPort, PublicIP: cfg.PublicIP}, hub, logger)
	if err != nil {
		db.Close()
		return nil, err
	}
	logger.Info("voice server listening", zap.Int("udpPort", voiceMedia.UDPPort()), zap.String("publicIP", cfg.PublicIP))

	authSvc := application.NewAuthService(userRepo, jwtSvc)
	userSvc := application.NewUserService(userRepo)
	voiceSvc := application.NewVoiceService(channelRepo, hub, voiceMedia, cfg.VoiceMaxParticipants)
	voiceMedia.SetEvents(voiceSvc)
	channelSvc := application.NewChannelService(channelRepo, voiceSvc)
	messageSvc := application.NewMessageService(messageRepo, channelRepo)

	ctx, cancel := context.WithCancel(context.Background())
	presenceSvc := application.NewPresenceService(userRepo, hub, cfg.PresenceIdleTimeout, cfg.PresenceOfflineGrace)
	go presenceSvc.Run(ctx)

	router := httphandler.NewRouter(httphandler.Dependencies{
		AuthHandler:    httphandler.NewAuthHandler(authSvc, logger),
		UserHandler:    httphandler.NewHandler(userSvc, logger),
		ChannelHandler: httphandler.NewChannelHandler(channelSvc, logger),
		MessageHandler: httphandler.NewMessageHandler(messageSvc, logger),
		WsHandler:      wshandler.NewHandler(hub, messageSvc, presenceSvc, voiceSvc, jwtSvc, logger),
		JWTService:     jwtSvc,
		Logger:         logger,
	})

	return &Server{Handler: router, db: db, voice: voiceMedia, cancel: cancel}, nil
}

// Close stops the background loops, the SFU and closes the database.
func (s *Server) Close() {
	s.cancel()
	s.voice.Close()
	s.db.Close()
}
