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
	"path/filepath"
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
	enableRateLimit          = parseBoolEnv("ENABLE_RATE_LIMIT", false)
	enableInputValidation    = parseBoolEnv("ENABLE_INPUT_VALIDATION", false)
	enableShellshockDefense  = parseBoolEnv("ENABLE_SHELLSHOCK_DEFENSE", true) // Default defense-in-depth enabled
	enableHMACVerification   = parseBoolEnv("ENABLE_HMAC_VERIFICATION", false) // Guide 5 Threat 3: MitM & Tamper Defense
	hmacSecretKey            = getEnv("HMAC_SECRET_KEY", "ssle-tamper-defense-secret-key-2026")
	maxTimestampDriftSecs    = parseFloatEnv("MAX_TIMESTAMP_DRIFT_SECS", 60.0)
	blockedDevicesList       = os.Getenv("BLOCKED_DEVICES")
	minTempAllowed           = parseFloatEnv("MIN_TEMP", -50.0)
	maxTempAllowed           = parseFloatEnv("MAX_TEMP", 100.0)
	rateLimitRPS             = parseFloatEnv("RATE_LIMIT_RPS", 20.0)

	// Rate limiter state: token bucket per IP
	rateLimiterMu sync.Mutex
	clientsBucket = make(map[string]*tokenBucket)

	// Access logging for Wazuh SIEM integration (Guide 4)
	accessLogPath = getEnv("ACCESS_LOG_PATH", "/var/log/ssle/ingestion_access.log")
	accessLogFile *os.File
	accessLogMu   sync.Mutex
)

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func parseBoolEnv(key string, fallback bool) bool {
	val := os.Getenv(key)
	if val == "" {
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

// initAccessLogger initializes the access log file with automatic directory creation and fallback
func initAccessLogger() {
	dir := filepath.Dir(accessLogPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		// Fallback to local access.log if system directory (/var/log/ssle) is not writable
		accessLogPath = "./ingestion_access.log"
	}

	f, err := os.OpenFile(accessLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		accessLogPath = "./ingestion_access.log"
		f, err = os.OpenFile(accessLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Printf("[Wazuh SIEM] Warning: Unable to open access log file: %v. Access logging to file disabled.\n", err)
			return
		}
	}
	accessLogFile = f
	log.Printf("[Wazuh SIEM] Structured access logging initialized at: %s\n", accessLogPath)
}

// logAccess writes standard Apache Combined Log Format entries for Wazuh localfile ingestion
func logAccess(clientIP string, method, uri, proto string, status, bytesSent int, userAgent string) {
	if accessLogFile == nil {
		return
	}

	// Apache Combined format: %h %l %u %t "%r" %>s %b "%{Referer}i" "%{User-Agent}i"
	// Example: 192.168.1.100 - - [11/Nov/2026:14:30:00 +0000] "POST /ingest HTTP/1.1" 200 45 "-" "curl/7.68.0"
	timeStr := time.Now().Format("02/Jan/2006:15:04:05 -0700")
	line := fmt.Sprintf("%s - - [%s] \"%s %s %s\" %d %d \"-\" \"%s\"\n",
		clientIP, timeStr, method, uri, proto, status, bytesSent, userAgent)

	accessLogMu.Lock()
	defer accessLogMu.Unlock()
	accessLogFile.WriteString(line)
}

type statusRecorder struct {
	http.ResponseWriter
	status    int
	bytesSent int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytesSent += n
	return n, err
}

func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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

	initAccessLogger()
	if accessLogFile != nil {
		defer accessLogFile.Close()
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
			"shellshock_defense":       enableShellshockDefense,
			"hmac_verification":        enableHMACVerification,
			"access_log_path":          accessLogPath,
			"blocked_devices_count":    len(strings.Split(blockedDevicesList, ",")),
		})
	})

	// POST /ingest
	mux.HandleFunc("POST /ingest", func(w http.ResponseWriter, r *http.Request) {
		clientIP := extractClientIP(r)
		userAgent := r.UserAgent()

		// SIEM Threat 1: Shellshock (CVE-2014-6271) Exploit Inspection
		// Attack pattern typically found in User-Agent, URI, or headers: () { :; };
		if strings.Contains(userAgent, "() {") || strings.Contains(r.URL.Path, "() {") {
			log.Printf("[SECURITY ALERT - Wazuh Threat 1] Shellshock (CVE-2014-6271) attack pattern detected from IP %s! User-Agent: %s\n",
				clientIP, userAgent)

			if enableShellshockDefense {
				function.RespondJSON(w, http.StatusForbidden, map[string]string{
					"error": "Malicious payload detected: Shellshock attack blocked by application security filter",
				})
				return
			}
		}

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

		// Defense 4: Cryptographic Message Integrity & Replay Protection (Threat 3 - MitM Prevention)
		if enableHMACVerification {
			tsHeader := r.Header.Get("X-Signature-Timestamp")
			sigHeader := r.Header.Get("X-Signature")

			valid, reason := function.VerifyPayload([]byte(hmacSecretKey), tsHeader, sigHeader, bodyBytes, time.Duration(maxTimestampDriftSecs)*time.Second)
			if !valid {
				log.Printf("[SECURITY ALERT - Threat 3] MitM tampering or invalid signature detected from IP %s! Reason: %s\n",
					clientIP, reason)
				function.RespondJSON(w, http.StatusUnauthorized, map[string]string{
					"error": fmt.Sprintf("Cryptographic integrity verification failed: %s", reason),
				})
				return
			}
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

	// Wrap mux with access logging middleware for Wazuh SIEM
	loggingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		mux.ServeHTTP(rec, r)

		clientIP := extractClientIP(r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		logAccess(clientIP, r.Method, r.RequestURI, r.Proto, status, rec.bytesSent, r.UserAgent())
	})

	log.Printf("Ingestion service listening on :%s (ID: %s)\n", port, serviceID)
	log.Printf("  Defenses: RateLimit=%v (RPS=%.1f) | InputValidation=%v (range=[%.1f, %.1f]) | ShellshockDefense=%v | Blocklist=%s\n",
		enableRateLimit, rateLimitRPS, enableInputValidation, minTempAllowed, maxTempAllowed, enableShellshockDefense, blockedDevicesList)

	// Production HTTP Server hardening against Slowloris and connection-flooding DDoS attacks
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           loggingHandler,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       15 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB header limit
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}
