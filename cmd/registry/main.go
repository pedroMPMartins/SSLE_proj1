package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"simple_soa/internal/function"
	"simple_soa/internal/models"
	"strings"
	"sync"
	"time"
)

var (
	registry   = make(map[string]models.Service)
	mu         sync.RWMutex
	adminToken = os.Getenv("ADMIN_TOKEN") // Optional token to protect DELETE /register/{id}
	port       = os.Getenv("PORT")
)

func main() {
	if port == "" {
		port = "5050"
	}

	mux := http.NewServeMux()

	// GET /health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		count := len(registry)
		mu.RUnlock()

		function.RespondJSON(w, http.StatusOK, map[string]any{
			"status":             "UP",
			"service":            "registry",
			"registered_services": count,
		})
	})

	// POST /register
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

		log.Printf("[Registry] Registered service '%s' (%s) at %s\n", req.ID, req.Kind, req.URL)
		function.RespondJSON(w, http.StatusOK, map[string]string{"message": req.ID + " registered"})
	})

	// DELETE /register/{id}
	mux.HandleFunc("DELETE /register/{id}", func(w http.ResponseWriter, r *http.Request) {
		// Defense mechanism: if ADMIN_TOKEN is set, enforce token authentication
		if adminToken != "" {
			reqToken := r.Header.Get("X-Admin-Token")
			if reqToken == "" {
				authHeader := r.Header.Get("Authorization")
				reqToken = strings.TrimPrefix(authHeader, "Bearer ")
			}
			if reqToken != adminToken {
				log.Printf("[Security Alert] Unauthorized unregister attempt on '%s' from %s\n", r.PathValue("id"), r.RemoteAddr)
				function.RespondJSON(w, http.StatusForbidden, map[string]string{"error": "Unauthorized: admin token required to unregister"})
				return
			}
		}

		id := r.PathValue("id")

		mu.Lock()
		defer mu.Unlock()

		if _, exists := registry[id]; exists {
			delete(registry, id)
			log.Printf("[Registry] Unregistered service '%s'\n", id)
			function.RespondJSON(w, http.StatusOK, map[string]string{"message": id + " unregistered"})
			return
		}

		function.RespondJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	})

	// GET /services/{kind}
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

	// GET /services
	mux.HandleFunc("GET /services", func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		defer mu.RUnlock()

		function.RespondJSON(w, http.StatusOK, registry)
	})

	log.Printf("Service Registry listening on :%s\n", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Registry server failed: %v", err)
	}
}
