package config

import (
	"github.com/caarlos0/env/v11"
)

type Config struct {
	Host string `env:"HARMONY_HOST" envDefault:"0.0.0.0"`
	Port int    `env:"HARMONY_PORT" envDefault:"8080"`
}

func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
