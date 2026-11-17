package main

import (
	"database/sql"
	"log"
	"math"
	"net/http"
	"os"
	"simple_soa/internal/function"
	"strings"

	_ "modernc.org/sqlite"
)

var (
	db          *sql.DB
	serviceKind = "analytics"
	serviceID   = function.GenerateRandomID()
	registryURL = os.Getenv("REGISTRY_URL")
	serviceURL  = os.Getenv("SERVICE_URL")
	port        = os.Getenv("PORT")
	dbPath      = os.Getenv("DB_PATH")
)

func initDB() {
	if dbPath == "" {
		dbPath = "temp_data.db"
	}

	var err error
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	// Ensure table exists so queries succeed even if no records yet
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
		log.Fatalf("Failed to initialize database table: %v", err)
	}
}

func main() {
	if port == "" {
		port = "5003"
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

	initDB()
	defer db.Close()

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

	// GET /average
	mux.HandleFunc("GET /average", func(w http.ResponseWriter, r *http.Request) {
		var avg sql.NullFloat64
		err := db.QueryRow("SELECT AVG(temperature) FROM temperatures").Scan(&avg)
		if err != nil {
			log.Printf("Query error on /average: %v\n", err)
			function.RespondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to compute average temperature"})
			return
		}

		if !avg.Valid {
			function.RespondJSON(w, http.StatusOK, map[string]any{"average_temperature": nil})
			return
		}

		rounded := math.Round(avg.Float64*100) / 100
		function.RespondJSON(w, http.StatusOK, map[string]any{"average_temperature": rounded})
	})

	// Wrap mux with generic DoS protection (token-bucket rate limiting and payload clamping)
	dosCfg := function.LoadDoSConfigFromEnv()
	protectedHandler := function.DoSProtectionMiddleware(mux, dosCfg)

	// Wrap with structured access logging for Wazuh SIEM
	accessLogger := function.NewAccessLogger("analytics", "")
	defer accessLogger.Close()
	loggingHandler := function.AccessLoggingMiddleware(accessLogger, protectedHandler)

	log.Printf("Analytics service listening on :%s (ID: %s, DoS Defense: RateLimit=%v [RPS=%.1f], MaxBodyBytes=%d)\n",
		port, serviceID, dosCfg.EnableRateLimit, dosCfg.RateLimitRPS, dosCfg.MaxBodyBytes)

	server := function.NewHardenedServer(":"+port, loggingHandler)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}
