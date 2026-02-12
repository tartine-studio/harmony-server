package main

import (
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/tartine-studio/harmony-server/internal/config"
	"github.com/tartine-studio/harmony-server/internal/server"
)

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}

	router := server.NewRouter()

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	logger.Info("starting server", zap.String("addr", addr))

	if err := http.ListenAndServe(addr, router); err != nil {
		logger.Fatal("server failed", zap.Error(err))
	}
}
