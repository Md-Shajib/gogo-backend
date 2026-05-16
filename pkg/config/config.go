package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port                string
	DatabaseURL         string
	JWTSecret           string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:                os.Getenv("PORT"),
		DatabaseURL:         os.Getenv("DB_URL"),
		JWTSecret:           os.Getenv("JWT_SECRET"),
	}

	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	required := map[string]string{
		"DB_URL":                cfg.DatabaseURL,
		"JWT_SECRET":            cfg.JWTSecret,
	}

	for key, val := range required {
		if val == "" {
			return nil, fmt.Errorf("%s is required", key)
		}
	}

	return cfg, nil
}
