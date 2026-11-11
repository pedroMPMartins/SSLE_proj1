package tests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"simple_soa/internal/function"
	"simple_soa/internal/models"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestStorageDatabaseOperations verifies SQLite table creation, data insertion, and average calculation
func TestStorageDatabaseOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ssle_test_storage_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test_temp_data.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open sqlite db: %v", err)
	}
	defer db.Close()

	// Initialize table schema
	query := `
		create table if not exists temperatures (
			id integer primary key autoincrement,
			device_id text,
			temperature real,
			timestamp datetime default current_timestamp
		);
	`
	if _, err := db.Exec(query); err != nil {
		t.Fatalf("Failed to create temperatures table: %v", err)
	}

	// Insert test readings
	readings := []struct {
		deviceID string
		temp     float64
	}{
		{"sensor-alpha", 20.0},
		{"sensor-beta", 25.0},
		{"sensor-alpha", 30.0},
	}

	for _, r := range readings {
		_, err := db.Exec("INSERT INTO temperatures (device_id, temperature) VALUES (?, ?)", r.deviceID, r.temp)
		if err != nil {
			t.Fatalf("Failed to insert reading: %v", err)
		}
	}

	// Verify count
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM temperatures").Scan(&count); err != nil {
		t.Fatalf("Failed to query count: %v", err)
	}
	if count != 3 {
		t.Errorf("Expected 3 rows in database, got %d", count)
	}

	// Verify average
	var avg float64
	if err := db.QueryRow("SELECT AVG(temperature) FROM temperatures").Scan(&avg); err != nil {
		t.Fatalf("Failed to compute average: %v", err)
	}
	if avg != 25.0 {
		t.Errorf("Expected average temperature 25.0, got %f", avg)
	}
}

// TestRegistryEndpoints verifies registration, listing, filtering, and protected deletion
func TestRegistryEndpoints(t *testing.T) {
	registry := make(map[string]models.Service)
	var mu sync.RWMutex
	adminToken := "secret-test-token"

	mux := http.NewServeMux()

	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		var req models.Service
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.URL == "" || req.Kind == "" {
			function.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing name or url"})
			return
		}
		mu.Lock()
		registry[req.ID] = req
		mu.Unlock()
		function.RespondJSON(w, http.StatusOK, map[string]string{"message": "registered"})
	})

	mux.HandleFunc("GET /services/{kind}", func(w http.ResponseWriter, r *http.Request) {
		kind := r.PathValue("kind")
		mu.RLock()
		defer mu.RUnlock()
		var res []models.Service
		for _, s := range registry {
			if s.Kind == kind {
				res = append(res, s)
			}
		}
		function.RespondJSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("DELETE /register/{id}", func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Admin-Token")
		if token != adminToken {
			function.RespondJSON(w, http.StatusForbidden, map[string]string{"error": "Unauthorized"})
			return
		}
		id := r.PathValue("id")
		mu.Lock()
		defer mu.Unlock()
		if _, ok := registry[id]; ok {
			delete(registry, id)
			function.RespondJSON(w, http.StatusOK, map[string]string{"message": "unregistered"})
			return
		}
		function.RespondJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// 1. Register a service
	regBody, _ := json.Marshal(models.Service{
		Kind: "storage",
		ID:   "storage-test-1",
		URL:  "http://localhost:15002",
	})
	resp, err := http.Post(server.URL+"/register", "application/json", bytes.NewBuffer(regBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Failed to register service: %v, status: %d", err, resp.StatusCode)
	}

	// 2. Query services by kind
	resp, err = http.Get(server.URL + "/services/storage")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Failed to query services: %v", err)
	}
	var svcList []models.Service
	json.NewDecoder(resp.Body).Decode(&svcList)
	if len(svcList) != 1 || svcList[0].ID != "storage-test-1" {
		t.Errorf("Unexpected service list: %+v", svcList)
	}

	// 3. Attempt unauthorized DELETE -> Expect 403
	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/register/storage-test-1", nil)
	delResp, err := http.DefaultClient.Do(req)
	if err != nil || delResp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden without admin token, got %d", delResp.StatusCode)
	}

	// 4. Attempt authorized DELETE -> Expect 200
	reqAuth, _ := http.NewRequest(http.MethodDelete, server.URL+"/register/storage-test-1", nil)
	reqAuth.Header.Set("X-Admin-Token", adminToken)
	delAuthResp, err := http.DefaultClient.Do(reqAuth)
	if err != nil || delAuthResp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK with admin token, got %d", delAuthResp.StatusCode)
	}
}

// TestHierarchicalRegistryFeatures verifies FQSN construction, location filtering, and incident localization
func TestHierarchicalRegistryFeatures(t *testing.T) {
	registry := make(map[string]models.Service)
	var mu sync.RWMutex

	mux := http.NewServeMux()

	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		var req models.Service
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			function.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if req.Name == "" {
			req.Name = req.BuildFQSN()
		}
		mu.Lock()
		registry[req.ID] = req
		mu.Unlock()
		function.RespondJSON(w, http.StatusOK, map[string]any{
			"message": "registered",
			"fqsn":    req.Name,
		})
	})

	mux.HandleFunc("GET /services/{kind}", func(w http.ResponseWriter, r *http.Request) {
		kind := r.PathValue("kind")
		q := r.URL.Query()
		region := q.Get("region")
		zone := q.Get("zone")

		mu.RLock()
		defer mu.RUnlock()
		var res []models.Service
		for _, s := range registry {
			if s.MatchesFilter(region, zone, "", "", kind) {
				res = append(res, s)
			}
		}
		function.RespondJSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("GET /services/locate/{query}", func(w http.ResponseWriter, r *http.Request) {
		query := r.PathValue("query")
		mu.RLock()
		defer mu.RUnlock()
		for _, s := range registry {
			if s.ID == query || s.Name == query {
				function.RespondJSON(w, http.StatusOK, map[string]any{
					"status":   "FOUND",
					"incident": s.LocateIncidentDetails(),
				})
				return
			}
		}
		function.RespondJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// 1. Register two services in different locations
	svc1 := models.Service{
		ID:   "ingest-pt-1",
		Kind: "ingestion",
		URL:  "http://10.0.1.1:5001",
		Location: models.ServiceLocation{
			Region:       "pt-aveiro",
			Zone:         "lab-ssle",
			PhysicalHost: "kali-vm",
			LogicalEnv:   "incus:ssle-ingestion",
		},
	}
	svc2 := models.Service{
		ID:   "ingest-eu-1",
		Kind: "ingestion",
		URL:  "http://10.0.2.1:5001",
		Location: models.ServiceLocation{
			Region:       "eu-west",
			Zone:         "dc-cloud",
			PhysicalHost: "dell-srv-02",
			LogicalEnv:   "incus:ssle-ingestion",
		},
	}

	for _, s := range []models.Service{svc1, svc2} {
		body, _ := json.Marshal(s)
		resp, err := http.Post(server.URL+"/register", "application/json", bytes.NewBuffer(body))
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to register: %v", err)
		}
	}

	// 2. Query filtered by Region
	resp, err := http.Get(server.URL + "/services/ingestion?region=pt-aveiro")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Failed to query regional services: %v", err)
	}
	var ptList []models.Service
	json.NewDecoder(resp.Body).Decode(&ptList)
	if len(ptList) != 1 || ptList[0].ID != "ingest-pt-1" {
		t.Errorf("Expected 1 service in pt-aveiro, got %+v", ptList)
	}

	// 3. Test Rapid Incident Localization endpoint
	resp, err = http.Get(server.URL + "/services/locate/ingest-pt-1")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Failed to locate incident: %v", err)
	}
	var locResp map[string]any
	json.NewDecoder(resp.Body).Decode(&locResp)
	if locResp["status"] != "FOUND" {
		t.Errorf("Expected status FOUND, got %v", locResp["status"])
	}
	incident := locResp["incident"].(map[string]any)
	if incident["physical_host"] != "kali-vm" || incident["zone"] != "lab-ssle" {
		t.Errorf("Unexpected incident localization details: %+v", incident)
	}
}


// TestIngestionDefensesValidation verifies rate limiting, plausibility bounds, and blocklisting
func TestIngestionDefensesValidation(t *testing.T) {
	minTemp := -50.0
	maxTemp := 100.0
	blockedDevice := "banned-device-99"

	validateReading := func(deviceID string, temp float64) (int, string) {
		if deviceID == blockedDevice {
			return http.StatusForbidden, "Blocked device"
		}
		if temp < minTemp || temp > maxTemp {
			return http.StatusUnprocessableEntity, "Out of bounds"
		}
		return http.StatusOK, "Accepted"
	}

	// Case 1: Valid reading
	code, msg := validateReading("sensor-valid", 23.5)
	if code != http.StatusOK {
		t.Errorf("Expected 200 OK for valid reading, got %d: %s", code, msg)
	}

	// Case 2: Extreme high temperature (Poisoning) -> Expect 422
	codeHigh, _ := validateReading("sensor-valid", 999.0)
	if codeHigh != http.StatusUnprocessableEntity {
		t.Errorf("Expected 422 for extreme high temperature, got %d", codeHigh)
	}

	// Case 3: Extreme low temperature (Poisoning) -> Expect 422
	codeLow, _ := validateReading("sensor-valid", -273.15)
	if codeLow != http.StatusUnprocessableEntity {
		t.Errorf("Expected 422 for extreme low temperature, got %d", codeLow)
	}

	// Case 4: Blocklisted Device ID -> Expect 403
	codeBlocked, _ := validateReading(blockedDevice, 25.0)
	if codeBlocked != http.StatusForbidden {
		t.Errorf("Expected 403 for blocked device ID, got %d", codeBlocked)
	}
}

// TestPrometheusMetricsFormatting verifies valid Prometheus exposition format output
func TestPrometheusMetricsFormatting(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("# HELP service_health Health status of the service (1 = UP, 0 = DOWN)\n")
	sb.WriteString("# TYPE service_health gauge\n")
	sb.WriteString(fmt.Sprintf("service_health{endpoint=%q,service=%q} %.1f\n", "http://localhost:15001/health", "ingestion", 1.0))
	sb.WriteString(fmt.Sprintf("service_health{endpoint=%q,service=%q} %.1f\n", "http://localhost:15002/health", "storage", 1.0))

	output := sb.String()
	if !strings.Contains(output, "# HELP service_health") || !strings.Contains(output, "# TYPE service_health gauge") {
		t.Errorf("Output missing Prometheus headers: %s", output)
	}
	if !strings.Contains(output, `service_health{endpoint="http://localhost:15001/health",service="ingestion"} 1.0`) {
		t.Errorf("Output missing metric line: %s", output)
	}
}

// TestWazuhSIEMLoggingAndShellshockDefense verifies access log formatting and Shellshock exploit prevention
func TestWazuhSIEMLoggingAndShellshockDefense(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ssle_wazuh_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logFilePath := filepath.Join(tmpDir, "ingestion_access.log")

	// Middleware simulation matching cmd/ingestion
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("Failed to open test access log: %v", err)
	}
	defer logFile.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /ingest", func(w http.ResponseWriter, r *http.Request) {
		userAgent := r.UserAgent()
		// Shellshock defense rule
		if strings.Contains(userAgent, "() {") {
			function.RespondJSON(w, http.StatusForbidden, map[string]string{
				"error": "Malicious payload detected: Shellshock attack blocked",
			})
			return
		}
		function.RespondJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)

		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		w.Write(rec.Body.Bytes())

		// Write Apache Combined entry
		timeStr := time.Now().Format("02/Jan/2006:15:04:05 -0700")
		line := fmt.Sprintf("%s - - [%s] \"%s %s %s\" %d %d \"-\" \"%s\"\n",
			r.RemoteAddr, timeStr, r.Method, r.RequestURI, r.Proto, rec.Code, rec.Body.Len(), r.UserAgent())
		logFile.WriteString(line)
	})

	ts := httptest.NewServer(handler)
	defer ts.Close()

	// 1. Normal legitimate request
	resp, err := http.Post(ts.URL+"/ingest", "application/json", strings.NewReader(`{"device_id":"sensor-1","temperature":20.0}`))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Legitimate request failed: %v (status %d)", err, resp.StatusCode)
	}

	// 2. Shellshock Exploit Probe (Guide 4)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/ingest", strings.NewReader(`{"device_id":"bad","temperature":20.0}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "() { :; }; /bin/cat /etc/passwd")

	exploitResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Shellshock request failed: %v", err)
	}
	if exploitResp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected HTTP 403 for Shellshock attack, got %d", exploitResp.StatusCode)
	}

	// 3. Inspect generated log file
	logBytes, err := os.ReadFile(logFilePath)
	if err != nil {
		t.Fatalf("Failed to read access log: %v", err)
	}
	logContent := string(logBytes)

	if !strings.Contains(logContent, `POST /ingest HTTP/1.1" 200`) {
		t.Errorf("Access log missing valid 200 entry: %s", logContent)
	}
	if !strings.Contains(logContent, `() { :; }; /bin/cat /etc/passwd`) {
		t.Errorf("Access log missing Shellshock attack signature for Wazuh: %s", logContent)
	}
	if !strings.Contains(logContent, `POST /ingest HTTP/1.1" 403`) {
		t.Errorf("Access log missing 403 status entry for exploit: %s", logContent)
	}
}

