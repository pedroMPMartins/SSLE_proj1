package main

import (
	"bytes"
	"encoding/json"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"simple_soa/internal/function"
	"strings"
	"sync"
	"time"
)

var (
	deviceID    = function.GenerateRandomID()
	deviceKind  = "temperature"
	registryURL = os.Getenv("REGISTRY_URL")
	ingestURL   = os.Getenv("INGEST_URL")
	port        = os.Getenv("PORT")
	serviceURL  = os.Getenv("SERVICE_URL")

	stateMu     sync.RWMutex
	lastTemp    float64
	readingsSent uint64
)

func sendTemp() {
	for {
		// Discover ingestion service dynamically if not explicitly specified via environment
		targetIngestURL := ingestURL
		if targetIngestURL == "" {
			service, err := function.DiscoverOneService(registryURL, "ingestion")
			if err != nil {
				log.Printf("Discovery error: %v. Retrying in 2 seconds...", err)
				time.Sleep(2 * time.Second)
				continue
			}
			targetIngestURL = service.URL
		}

		endpoint := strings.TrimSuffix(targetIngestURL, "/") + "/ingest"

		// Generate random float between 20.0 and 30.0, rounded to 2 decimal places
		rawTemp := 20.0 + rand.Float64()*10.0
		temp := math.Round(rawTemp*100) / 100

		payload := map[string]any{
			"device_id":   deviceID,
			"temperature": temp,
		}

		jsonBody, err := json.Marshal(payload)
		if err != nil {
			log.Println("Error marshalling JSON:", err)
			time.Sleep(5 * time.Second)
			continue
		}

		resp, err := http.Post(endpoint, "application/json", bytes.NewBuffer(jsonBody))
		if err != nil {
			log.Printf("Error sending data to %s: %v\n", endpoint, err)
		} else {
			resp.Body.Close()
			stateMu.Lock()
			lastTemp = temp
			readingsSent++
			stateMu.Unlock()
			log.Printf("Successfully sent reading to %s: %+v (status %d)\n", endpoint, payload, resp.StatusCode)
		}

		time.Sleep(5 * time.Second)
	}
}

func main() {
	if port == "" {
		port = "5000"
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

	// Register sensor with the registry in background retry loop
	go function.RegisterWithRetry(deviceID, deviceKind, serviceURL, regEndpoint)

	mux := http.NewServeMux()

	// GET /health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		function.RespondJSON(w, http.StatusOK, map[string]any{
			"status":    "UP",
			"service":   deviceKind,
			"device_id": deviceID,
		})
	})

	// GET /status
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		stateMu.RLock()
		defer stateMu.RUnlock()
		function.RespondJSON(w, http.StatusOK, map[string]any{
			"status":        "UP",
			"service":       deviceKind,
			"device_id":     deviceID,
			"last_temp":     lastTemp,
			"readings_sent": readingsSent,
		})
	})

	// Start background sensor loop
	go sendTemp()

	log.Printf("Temperature sensor started (ID: %s) on :%s. Target registry: %s\n", deviceID, port, registryURL)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Temperature server error: %v", err)
	}
}
