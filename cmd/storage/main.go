package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"simple_soa/internal/function"
	"strings"

	_ "modernc.org/sqlite"
)

var (
	db          *sql.DB
	serviceKind = "storage"
	serviceID   = function.GenerateRandomID()
	serviceURL  = os.Getenv("SERVICE_URL")
	registryURL = os.Getenv("REGISTRY_URL")
	port        = os.Getenv("PORT")
	dbPath      = os.Getenv("DB_PATH")
)

func initDB() {
	if dbPath == "" {
		dbPath = "temp_data.db"
	}
	var err error
	// Open connection to sqlite database file
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	// Create table if it doesn't exist
	query := `
		create table if not exists temperatures (
			id integer primary key autoincrement,
			device_id text,
			temperature real,
			timestamp datetime default current_timestamp
		);
	`

	_, err = db.Exec(query)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}
}

type TemperaturePayload struct {
	DeviceID    string   `json:"device_id"`
	ID          string   `json:"id"`
	Temperature *float64 `json:"temperature"`
}

func main() {
	if port == "" {
		port = "5002"
	}
	if serviceURL == "" {
		serviceURL = "http://localhost:" + port
	}
	if registryURL == "" {
		registryURL = "http://localhost:5050"
	}

	initDB()
	defer db.Close()

	regEndpoint := registryURL
	if !strings.HasSuffix(regEndpoint, "/register") {
		regEndpoint = strings.TrimSuffix(regEndpoint, "/") + "/register"
	}

	// Register with service registry in background retry loop
	go function.RegisterWithRetry(serviceID, serviceKind, serviceURL, regEndpoint)

	mux := http.NewServeMux()

	// GET /health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(); err != nil {
			function.RespondJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status":  "DOWN",
				"service": serviceKind,
				"error":   err.Error(),
			})
			return
		}
		function.RespondJSON(w, http.StatusOK, map[string]string{
			"status":  "UP",
			"service": serviceKind,
		})
	})

	// POST /store
	mux.HandleFunc("POST /store", func(w http.ResponseWriter, r *http.Request) {
		var data TemperaturePayload
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			function.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
			return
		}

		deviceID := data.DeviceID
		if deviceID == "" {
			deviceID = data.ID
		}

		if deviceID == "" || data.Temperature == nil {
			function.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing device_id or temperature"})
			return
		}

		_, err := db.Exec("INSERT INTO temperatures (device_id, temperature) VALUES (?, ?)", deviceID, *data.Temperature)
		if err != nil {
			log.Printf("Failed to insert temperature: %v\n", err)
			function.RespondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database insert failed"})
			return
		}

		function.RespondJSON(w, http.StatusOK, map[string]string{"status": "stored"})
	})

	// Wrap mux with generic DoS protection (token-bucket rate limiting and payload clamping)
	dosCfg := function.LoadDoSConfigFromEnv()
	protectedHandler := function.DoSProtectionMiddleware(mux, dosCfg)

	// Wrap with structured access logging for Wazuh SIEM
	accessLogger := function.NewAccessLogger("storage", "")
	defer accessLogger.Close()
	loggingHandler := function.AccessLoggingMiddleware(accessLogger, protectedHandler)

	log.Printf("Storage service listening on :%s (ID: %s, DoS Defense: RateLimit=%v [RPS=%.1f], MaxBodyBytes=%d)\n",
		port, serviceID, dosCfg.EnableRateLimit, dosCfg.RateLimitRPS, dosCfg.MaxBodyBytes)

	server := function.NewHardenedServer(":"+port, loggingHandler)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}
