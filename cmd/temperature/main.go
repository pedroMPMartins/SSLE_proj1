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
	"time"
)

var (
	deviceID    = function.GenerateRandomID()
	deviceKind  = "temperature"
	registryURL = os.Getenv("REGISTRY_URL")
	ingestURL   = os.Getenv("INGEST_URL")
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
			log.Printf("Successfully sent reading to %s: %+v (status %d)\n", endpoint, payload, resp.StatusCode)
		}

		time.Sleep(5 * time.Second)
	}
}

func main() {
	if registryURL == "" {
		registryURL = "http://localhost:5050"
	}

	regEndpoint := registryURL
	if !strings.HasSuffix(regEndpoint, "/register") {
		regEndpoint = strings.TrimSuffix(regEndpoint, "/") + "/register"
	}

	// Register sensor with the registry in background retry loop
	go function.RegisterWithRetry(deviceID, deviceKind, "", regEndpoint)

	log.Printf("Temperature sensor started (ID: %s). Target registry: %s\n", deviceID, registryURL)
	sendTemp()
}
