package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"simple_soa/internal/function"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	serviceKind = "ingestion"
	serviceID   = function.GenerateRandomID()
	registryURL = os.Getenv("REGISTRY_URL")
	storageURL  = os.Getenv("STORAGE_URL")
	port        = os.Getenv("PORT")
	serviceURL  = os.Getenv("SERVICE_URL")

	// --- Defensive Controls (Guide 3 Remediation) ---
	enableRateLimit       = parseBoolEnv("ENABLE_RATE_LIMIT", false)
	enableInputValidation = parseBoolEnv("ENABLE_INPUT_VALIDATION", false)
	blockedDevicesList    = os.Getenv("BLOCKED_DEVICES")
	minTempAllowed        = parseFloatEnv("MIN_TEMP", -50.0)
	maxTempAllowed        = parseFloatEnv("MAX_TEMP", 100.0)
	rateLimitRPS          = parseFloatEnv("RATE_LIMIT_RPS", 20.0)

	// Rate limiter state: token bucket per IP
	rateLimiterMu sync.Mutex
	clientsBucket = make(map[string]*tokenBucket)
)

func parseBoolEnv(key string, fallback bool) bool {
	val := os.Getenv(key)
	if val == "" {
		// Also check global ENABLE_DEFENSES
		if os.Getenv("ENABLE_DEFENSES") == "true" {
			return true
		}
		return fallback
	}
	return val == "true" || val == "1"
}

func parseFloatEnv(key string, fallback float64) float64 {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return fallback
	}
	return f
}

type tokenBucket struct {
	tokens         float64
	capacity       float64
	refillRate     float64 // tokens per second
	lastRefillTime time.Time
}

func (tb *tokenBucket) allow() bool {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefillTime).Seconds()
	tb.lastRefillTime = now

	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}
	return false
}

func isRateLimited(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	rateLimiterMu.Lock()
	defer rateLimiterMu.Unlock()

	bucket, exists := clientsBucket[host]
	if !exists {
		bucket = &tokenBucket{
			tokens:         rateLimitRPS,
			capacity:       rateLimitRPS * 2,
			refillRate:     rateLimitRPS,
			lastRefillTime: time.Now(),
		}
		clientsBucket[host] = bucket
	}

	return !bucket.allow()
}

func isDeviceBlocked(deviceID string) bool {
	if blockedDevicesList == "" || deviceID == "" {
		return false
	}
	blocked := strings.Split(blockedDevicesList, ",")
	for _, b := range blocked {
		if strings.TrimSpace(b) == deviceID {
			return true
		}
	}
	return false
}

type IngestReading struct {
	DeviceID    string   `json:"device_id"`
	ID          string   `json:"id"`
	Temperature *float64 `json:"temperature"`
}

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

	// GET /health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		function.RespondJSON(w, http.StatusOK, map[string]any{
			"status":                   "UP",
			"service":                  serviceKind,
			"rate_limit_enabled":       enableRateLimit,
			"input_validation_enabled": enableInputValidation,
			"blocked_devices_count":    len(strings.Split(blockedDevicesList, ",")),
		})
	})

	// POST /ingest
	mux.HandleFunc("POST /ingest", func(w http.ResponseWriter, r *http.Request) {
		// Defense 1: Rate Limiting
		if enableRateLimit && isRateLimited(r.RemoteAddr) {
			w.Header().Set("Retry-After", "1")
			function.RespondJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "Rate limit exceeded. Please throttle requests.",
			})
			return
		}

		// Cap max body size to 1MB to avoid memory exhaustion
		r.Body = http.MaxBytesReader(w, r.Body, 1048576)
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil || len(bodyBytes) == 0 {
			function.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid or empty request body"})
			return
		}

		// Parse reading for inspection
		var reading IngestReading
		if err := json.Unmarshal(bodyBytes, &reading); err != nil {
			function.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": "Malformed JSON payload"})
			return
		}

		devID := reading.DeviceID
		if devID == "" {
			devID = reading.ID
		}

		// Defense 2: Blocklisting
		if isDeviceBlocked(devID) {
			log.Printf("[Defense] Blocked request from unauthorized device ID: %s\n", devID)
			function.RespondJSON(w, http.StatusForbidden, map[string]string{
				"error": fmt.Sprintf("Device '%s' is blocked by security policy", devID),
			})
			return
		}

		// Defense 3: Input Validation / Bounds Checking
		if enableInputValidation && reading.Temperature != nil {
			temp := *reading.Temperature
			if temp < minTempAllowed || temp > maxTempAllowed {
				log.Printf("[Defense] Rejected invalid temperature value %.2f°C from %s (allowed range: %.1f to %.1f)\n",
					temp, devID, minTempAllowed, maxTempAllowed)
				function.RespondJSON(w, http.StatusUnprocessableEntity, map[string]string{
					"error": fmt.Sprintf("Temperature value %.2f is out of physical plausible bounds [%.1f, %.1f]",
						temp, minTempAllowed, maxTempAllowed),
				})
				return
			}
		}

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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
	})

	log.Printf("Ingestion service listening on :%s (ID: %s)\n", port, serviceID)
	log.Printf("  Defenses: RateLimit=%v (RPS=%.1f) | InputValidation=%v (range=[%.1f, %.1f]) | Blocklist=%s\n",
		enableRateLimit, rateLimitRPS, enableInputValidation, minTempAllowed, maxTempAllowed, blockedDevicesList)

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
