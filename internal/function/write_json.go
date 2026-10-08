package function

import (
	"encoding/json"
	"net/http"
)

// Helper function to send JSON requests
func RespondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}
