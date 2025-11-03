package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/hathor-network/hathor-facilitator/internal/config"
	"github.com/hathor-network/hathor-facilitator/internal/hathor"
	"github.com/hathor-network/hathor-facilitator/internal/settle"
	"github.com/hathor-network/hathor-facilitator/internal/supported"
	"github.com/hathor-network/hathor-facilitator/internal/verify"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize Hathor client
	hathorClient := hathor.NewClient(cfg.HathorNodeURL, cfg.HathorWalletURL, cfg.HathorWalletID)

	// Initialize handlers
	verifyHandler := verify.NewHandler(hathorClient)
	settleHandler := settle.NewHandler(hathorClient, cfg.MinConfirmations)
	supportedHandler := supported.NewHandler()

	// Setup routes
	http.HandleFunc("/verify", verifyHandler.Handle)
	http.HandleFunc("/settle", settleHandler.Handle)
	http.HandleFunc("/supported", supportedHandler.Handle)
	http.HandleFunc("/health", healthHandler)

	// Start server
	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Hathor x402 Facilitator starting on %s", addr)
	log.Printf("Hathor Node: %s", cfg.HathorNodeURL)
	log.Printf("Hathor Wallet: %s", cfg.HathorWalletURL)
	
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
		"service": "hathor-facilitator",
	})
}
