package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"simple_soa/internal/function"

	_ "modernc.org/sqlite"
)

var (
	db          *sql.DB
	serviceKind = "storage"
	serviceID   = function.GenerateRandomID()
	serviceURL  = os.Getenv("SERVICE_URL")
	registryURL = os.Getenv("REGISTRY_URL")
	port        = os.Getenv("PORT")
)

func initDB() {
	var err error
	// Open connection to sqlite database file
	db, err = sql.Open("sqlite", "temp_data.db")
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
		registryURL = "http://localhost:5050/register"
	}

	initDB()
	defer db.Close()

	// Register with service registry in background retry loop
	go function.RegisterWithRetry(serviceID, serviceKind, serviceURL, registryURL)

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

	log.Printf("Storage service listening on :%s (ID: %s)\n", port, serviceID)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
