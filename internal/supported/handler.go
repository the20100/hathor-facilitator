package supported

import (
	"encoding/json"
	"net/http"
)

type Handler struct{}

func NewHandler() *Handler {
	return &Handler{}
}

type SupportedResponse struct {
	Success bool     `json:"success"`
	Schemes []Scheme `json:"schemes"`
}

type Scheme struct {
	Scheme      string    `json:"scheme"`
	Networks    []string  `json:"networks"`
	Assets      []string  `json:"assets"`
	Description string    `json:"description"`
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		respondJSON(w, http.StatusMethodNotAllowed, SupportedResponse{
			Success: false,
		})
		return
	}

	// Handle CORS preflight
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Return supported schemes and networks
	response := SupportedResponse{
		Success: true,
		Schemes: []Scheme{
			{
				Scheme:      "exact",
				Networks:    []string{"hathor-mainnet", "hathor-testnet"},
				Assets:      []string{"HTR", "00"}, // "00" is the token ID for native HTR
				Description: "Exact payment scheme - pay a fixed amount for a single request",
			},
		},
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	respondJSON(w, http.StatusOK, response)
}

func respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

