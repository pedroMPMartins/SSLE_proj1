package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"simple_soa/internal/function"
	"strings"
)

var (
	serviceKind = "ingestion"
	serviceID   = function.GenerateRandomID()
	registryURL = os.Getenv("REGISTRY_URL")
	storageURL  = os.Getenv("STORAGE_URL")
	port        = os.Getenv("PORT")
	serviceURL  = os.Getenv("SERVICE_URL")
)

func main() {
	if port == "" {
		port = "5001"
	}
	if serviceURL == "" {
		serviceURL = "http://localhost:" + port
	}
	if registryURL == "" {
		registryURL = "http://localhost:5050"
	}

	regEndpoint := registryURL
	if !strings.HasSuffix(regEndpoint, "/register") {
		regEndpoint = strings.TrimSuffix(regEndpoint, "/") + "/register"
	}

	// Register with service registry in background retry loop
	go function.RegisterWithRetry(serviceID, serviceKind, serviceURL, regEndpoint)

	mux := http.NewServeMux()

	// POST /ingest
	mux.HandleFunc("POST /ingest", func(w http.ResponseWriter, r *http.Request) {
		// Read incoming request body
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil || len(bodyBytes) == 0 {
			function.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid or empty request body"})
			return
		}

		fmt.Println("Received:", string(bodyBytes))

		// Resolve storage URL: either from STORAGE_URL env or via dynamic discovery
		targetStorageURL := storageURL
		if targetStorageURL == "" {
			storageService, err := function.DiscoverOneService(registryURL, "storage")
			if err != nil {
				log.Printf("Failed to discover storage service: %v\n", err)
				function.RespondJSON(w, http.StatusServiceUnavailable, map[string]string{
					"error": fmt.Sprintf("Storage service discovery failed: %v", err),
				})
				return
			}
			targetStorageURL = storageService.URL
		}

		storeEndpoint := strings.TrimSuffix(targetStorageURL, "/") + "/store"

		// Forward payload to storage service
		resp, err := http.Post(storeEndpoint, "application/json", bytes.NewBuffer(bodyBytes))
		if err != nil {
			log.Printf("Failed to forward data to storage (%s): %v\n", storeEndpoint, err)
			function.RespondJSON(w, http.StatusBadGateway, map[string]string{
				"error": fmt.Sprintf("Failed to contact storage service: %v", err),
			})
			return
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("Storage response (%d): %s\n", resp.StatusCode, string(respBody))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
	})

	log.Printf("Ingestion service listening on :%s (ID: %s)\n", port, serviceID)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
