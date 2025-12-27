package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/hathor-network/hathor-facilitator/internal/config"
	"github.com/hathor-network/hathor-facilitator/internal/hathor"
	"github.com/hathor-network/hathor-facilitator/internal/settle"
	"github.com/hathor-network/hathor-facilitator/internal/supported"
	"github.com/hathor-network/hathor-facilitator/internal/verify"
)

// loggingMiddleware wraps an http.HandlerFunc with request logging
func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		log.Printf(">>> Incoming Request: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		log.Printf("    Request URI: %s", r.RequestURI)
		log.Printf("    User-Agent: %s", r.Header.Get("User-Agent"))
		log.Printf("    Content-Type: %s", r.Header.Get("Content-Type"))
		log.Printf("    Content-Length: %s", r.Header.Get("Content-Length"))
		
		// Call the actual handler
		next(w, r)
		
		// Log completion
		duration := time.Since(start)
		log.Printf("<<< Request Completed: %s %s (duration: %v)", r.Method, r.URL.Path, duration)
	}
}

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize Hathor client
	hathorClient := hathor.NewClient(cfg.HathorNodeURL, cfg.HathorMiningServiceURL)

	// Initialize handlers
	verifyHandler := verify.NewHandler(hathorClient)
	settleHandler := settle.NewHandler(hathorClient, cfg.MinConfirmations)
	supportedHandler := supported.NewHandler()

	// Setup routes with logging middleware
	http.HandleFunc("/verify", loggingMiddleware(verifyHandler.Handle))
	http.HandleFunc("/settle", loggingMiddleware(settleHandler.Handle))
	http.HandleFunc("/supported", loggingMiddleware(supportedHandler.Handle))
	http.HandleFunc("/health", loggingMiddleware(healthHandler))

	// Start server
	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Hathor x402 Facilitator starting on %s", addr)
	log.Printf("Hathor Node: %s", cfg.HathorNodeURL)
	log.Printf("Hathor Mining Service: %s", cfg.HathorMiningServiceURL)
	
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("=== Health Check Request Received ===")
	log.Printf("Method: %s", r.Method)
	log.Printf("Path: %s", r.URL.Path)
	log.Printf("RemoteAddr: %s", r.RemoteAddr)
	log.Printf("Content-Type: %s", r.Header.Get("Content-Type"))
	log.Printf("Content-Length: %s", r.Header.Get("Content-Length"))
	
	// Log all headers for debugging
	log.Printf("All request headers:")
	for name, values := range r.Header {
		for _, value := range values {
			// Truncate long headers for readability
			displayValue := value
			if len(displayValue) > 200 {
				displayValue = displayValue[:200] + "... (truncated)"
			}
			log.Printf("  %s: %s", name, displayValue)
		}
	}
	
	log.Printf("Health check: returning healthy status")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
		"service": "hathor-facilitator",
	})
	log.Printf("Health check: response sent successfully")
}
