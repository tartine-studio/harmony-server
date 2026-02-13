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
}

func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
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
