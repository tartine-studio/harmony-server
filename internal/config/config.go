package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Host          string        `env:"HARMONY_HOST"            envDefault:"0.0.0.0"`
	Port          int           `env:"HARMONY_PORT"            envDefault:"8080"`
	DataDir       string        `env:"HARMONY_DATA_DIR"        envDefault:"./data"`
	JWTSecret     string        `env:"HARMONY_JWT_SECRET"`
	JWTAccessTTL  time.Duration `env:"HARMONY_JWT_ACCESS_TTL"  envDefault:"15m"`
	JWTRefreshTTL time.Duration `env:"HARMONY_JWT_REFRESH_TTL" envDefault:"168h"`

	PresenceIdleTimeout  time.Duration `env:"HARMONY_PRESENCE_IDLE_TIMEOUT"   envDefault:"5m"`
	PresenceOfflineGrace time.Duration `env:"HARMONY_PRESENCE_OFFLINE_GRACE"  envDefault:"10s"`

	VoiceUDPPort         int    `env:"HARMONY_VOICE_UDP_PORT"         envDefault:"50000"`
	PublicIP             string `env:"HARMONY_PUBLIC_IP"`
	VoiceMaxParticipants int    `env:"HARMONY_VOICE_MAX_PARTICIPANTS" envDefault:"25"`
}

func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}

	if cfg.VoiceMaxParticipants < 1 {
		return Config{}, fmt.Errorf("HARMONY_VOICE_MAX_PARTICIPANTS must be >= 1, got %d", cfg.VoiceMaxParticipants)
	}
	if cfg.VoiceUDPPort < 1 || cfg.VoiceUDPPort > 65535 {
		return Config{}, fmt.Errorf("HARMONY_VOICE_UDP_PORT must be a valid port, got %d", cfg.VoiceUDPPort)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return Config{}, fmt.Errorf("create data dir: %w", err)
	}

	if cfg.JWTSecret == "" {
		secret, err := resolveJWTSecret(cfg.DataDir)
		if err != nil {
			return Config{}, fmt.Errorf("resolve jwt secret: %w", err)
		}
		cfg.JWTSecret = secret
	}

	return cfg, nil
}

func resolveJWTSecret(dataDir string) (string, error) {
	path := filepath.Join(dataDir, ".jwt_secret")

	data, err := os.ReadFile(path)
	if err == nil {
		return string(data), nil
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}

	secret := hex.EncodeToString(b)
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		return "", fmt.Errorf("persist secret: %w", err)
	}

	return secret, nil
}
