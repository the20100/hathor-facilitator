package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port                 int
	HathorNodeURL        string
	HathorMiningServiceURL string
	MinConfirmations     int
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:                  getEnvAsInt("PORT", 3000),
		HathorNodeURL:         getEnv("HATHOR_NODE_URL", "http://localhost:8080"),
		HathorMiningServiceURL: getEnv("HATHOR_MINING_SERVICE_URL", "https://txmining.testnet.hathor.network"),
		MinConfirmations:      getEnvAsInt("MIN_CONFIRMATIONS", 0),
	}

	// Validate required fields
	if cfg.HathorNodeURL == "" {
		return nil, fmt.Errorf("HATHOR_NODE_URL is required")
	}
	if cfg.HathorMiningServiceURL == "" {
		return nil, fmt.Errorf("HATHOR_MINING_SERVICE_URL is required")
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
