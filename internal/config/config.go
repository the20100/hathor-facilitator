package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port              int
	HathorNodeURL     string
	HathorWalletURL   string
	HathorWalletID    string
	MinConfirmations  int
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:              getEnvAsInt("PORT", 8443),
		HathorNodeURL:     getEnv("HATHOR_NODE_URL", "http://localhost:8080"),
		HathorWalletURL:   getEnv("HATHOR_WALLET_URL", "http://localhost:8000"),
		HathorWalletID:    getEnv("HATHOR_WALLET_ID", "main-wallet"),
		MinConfirmations:  getEnvAsInt("MIN_CONFIRMATIONS", 1),
	}

	// Validate required fields
	if cfg.HathorNodeURL == "" {
		return nil, fmt.Errorf("HATHOR_NODE_URL is required")
	}
	if cfg.HathorWalletURL == "" {
		return nil, fmt.Errorf("HATHOR_WALLET_URL is required")
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
