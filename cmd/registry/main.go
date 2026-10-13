package main

import (
	"encoding/json"
	"net/http"
	"simple_soa/internal/function"
	"simple_soa/internal/models"
	"sync"
	"time"
)

var (
	registry = make(map[string]models.Service)
	mu       sync.RWMutex
)

func main() {
	mux := http.NewServeMux()

	// Post /register
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		var req models.Service

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.URL == "" || req.Kind == "" {
			function.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing name or url"})
			return
		}

		if req.Timestamp.IsZero() {
			req.Timestamp = time.Now().UTC()
		}

		mu.Lock()
		registry[req.ID] = req
		mu.Unlock()

		function.RespondJSON(w, http.StatusOK, map[string]string{"message": req.ID + " registered"})
	})

	// Delete /register/{id}
	mux.HandleFunc("DELETE /register/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		mu.Lock()
		defer mu.Unlock()

		if _, exists := registry[id]; exists {
			delete(registry, id)
			function.RespondJSON(w, http.StatusOK, map[string]string{"message": id + " unregistered"})
			return
		}

		function.RespondJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	})

	// Get /services/{kind}
	mux.HandleFunc("GET /services/{kind}", func(w http.ResponseWriter, r *http.Request) {
		kind := r.PathValue("kind")

		mu.RLock()
		defer mu.RUnlock()
		var servicesToReturn []models.Service
		for _, serv := range registry {
			if serv.Kind == kind {
				servicesToReturn = append(servicesToReturn, serv)
			}
		}

		function.RespondJSON(w, http.StatusOK, servicesToReturn)
	})

	// Get /services
	mux.HandleFunc("GET /services", func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		defer mu.RUnlock()

		function.RespondJSON(w, http.StatusOK, registry)
	})

	// Start server on port 5050
	http.ListenAndServe(":5050", mux)
}
