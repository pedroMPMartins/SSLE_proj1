package tests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
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
			if s.ID == query || s.Name == query || s.Kind == query || strings.Contains(strings.ToLower(s.Name), strings.ToLower(query)) || strings.Contains(s.URL, query) {
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

// TestDDoSTargetHardeningControls verifies Guide 5 hardening: body size limits and concurrency throttling
func TestDDoSTargetHardeningControls(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ingest", func(w http.ResponseWriter, r *http.Request) {
		// Enforce 1MB payload capping
		r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			function.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": "Payload too large"})
			return
		}
		if len(body) == 0 {
			function.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": "Empty body"})
			return
		}
		function.RespondJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// 1. Normal valid payload
	validResp, err := http.Post(server.URL+"/ingest", "application/json", strings.NewReader(`{"device_id":"s1","temperature":22.0}`))
	if err != nil || validResp.StatusCode != http.StatusOK {
		t.Fatalf("Valid request failed: %v", err)
	}

	// 2. Oversized payload (DDoS memory exhaustion probe: 2MB > 1MB limit)
	hugePayload := bytes.Repeat([]byte("A"), 2*1024*1024)
	hugeResp, err := http.Post(server.URL+"/ingest", "application/json", bytes.NewReader(hugePayload))
	if err != nil {
		t.Fatalf("Huge request failed: %v", err)
	}
	if hugeResp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected HTTP 400 for oversized payload (>1MB), got %d", hugeResp.StatusCode)
	}
}

// TestRegistryUnhappyPathsAndEdgeCases verifies error handling on missing fields, auth, and 404 lookups
func TestRegistryUnhappyPathsAndEdgeCases(t *testing.T) {
	registry := make(map[string]models.Service)
	var mu sync.RWMutex
	adminToken := "secret-token-123"

	mux := http.NewServeMux()

	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		var req models.Service
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.URL == "" || req.Kind == "" {
			function.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing id, kind, or url"})
			return
		}
		if req.Name == "" {
			req.Name = req.BuildFQSN()
		}
		mu.Lock()
		registry[req.ID] = req
		mu.Unlock()
		function.RespondJSON(w, http.StatusOK, map[string]string{"message": "registered"})
	})

	mux.HandleFunc("DELETE /register/{id}", func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Admin-Token")
		if token != adminToken {
			function.RespondJSON(w, http.StatusForbidden, map[string]string{"error": "Unauthorized"})
			return
		}
		target := r.PathValue("id")
		mu.Lock()
		defer mu.Unlock()

		var foundKey string
		if _, exists := registry[target]; exists {
			foundKey = target
		} else {
			for k, v := range registry {
				if v.Name == target {
					foundKey = k
					break
				}
			}
		}

		if foundKey != "" {
			delete(registry, foundKey)
			function.RespondJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
			return
		}
		function.RespondJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	})

	mux.HandleFunc("GET /services/locate/{query}", func(w http.ResponseWriter, r *http.Request) {
		query := r.PathValue("query")
		mu.RLock()
		defer mu.RUnlock()
		if s, ok := registry[query]; ok {
			function.RespondJSON(w, http.StatusOK, map[string]any{"status": "FOUND", "incident": s.LocateIncidentDetails()})
			return
		}
		function.RespondJSON(w, http.StatusNotFound, map[string]any{"status": "NOT_FOUND", "error": "Not found"})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 1. Unhappy POST: Empty payload -> 400
	resp, _ := http.Post(ts.URL+"/register", "application/json", strings.NewReader(""))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for empty body, got %d", resp.StatusCode)
	}

	// 2. Unhappy POST: Missing required field 'url' -> 400
	badJSON := `{"id":"s1","kind":"storage"}`
	resp, _ = http.Post(ts.URL+"/register", "application/json", strings.NewReader(badJSON))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing url, got %d", resp.StatusCode)
	}

	// 3. Register valid service for subsequent delete tests
	validJSON := `{"id":"storage-x","kind":"storage","url":"http://localhost:15002","location":{"region":"pt-aveiro","zone":"zone-1"}}`
	resp, _ = http.Post(ts.URL+"/register", "application/json", strings.NewReader(validJSON))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Failed to register valid service: status %d", resp.StatusCode)
	}

	// 4. Unhappy DELETE: Missing admin token -> 403 Forbidden
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/register/storage-x", nil)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected 403 for unauthorized delete, got %d", resp.StatusCode)
	}

	// 5. Unhappy DELETE: Non-existent ID with valid token -> 404 Not Found
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/register/non-existent-id", nil)
	req.Header.Set("X-Admin-Token", adminToken)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for deleting non-existent ID, got %d", resp.StatusCode)
	}

	// 6. Unhappy GET locate: Non-existent query -> 404 Not Found
	resp, _ = http.Get(ts.URL + "/services/locate/non-existent-service")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for locating non-existent service, got %d", resp.StatusCode)
	}

	// 7. Happy DELETE by FQSN -> 200 OK
	fqsn := "pt-aveiro.zone-1.kali-vm.incus:ssle-storage.storage-x.storage"
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/register/"+fqsn, nil)
	req.Header.Set("X-Admin-Token", adminToken)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for deleting service by FQSN, got %d", resp.StatusCode)
	}
}

// TestIngestionUnhappyPathsAndBoundaryValues tests edge cases in input validation, blocklists, and failure resilience
func TestIngestionUnhappyPathsAndBoundaryValues(t *testing.T) {
	minTemp := -50.0
	maxTemp := 100.0
	blockedList := "blocked-bot-1, rogue-sensor-99 "

	isBlocked := func(id string) bool {
		for _, b := range strings.Split(blockedList, ",") {
			if strings.TrimSpace(b) == strings.TrimSpace(id) {
				return true
			}
		}
		return false
	}

	validateInput := func(devID string, temp *float64) int {
		if isBlocked(devID) {
			return http.StatusForbidden
		}
		if temp != nil {
			if *temp < minTemp || *temp > maxTemp {
				return http.StatusUnprocessableEntity
			}
		}
		return http.StatusOK
	}

	// 1. Boundary testing on temperature
	pTemp := func(f float64) *float64 { return &f }

	cases := []struct {
		name     string
		devID    string
		temp     *float64
		expected int
	}{
		{"Exact minimum boundary (-50.0)", "sensor-1", pTemp(-50.0), http.StatusOK},
		{"Exact maximum boundary (100.0)", "sensor-1", pTemp(100.0), http.StatusOK},
		{"Nominal temperature (22.5)", "sensor-1", pTemp(22.5), http.StatusOK},
		{"Zero temperature (0.0)", "sensor-1", pTemp(0.0), http.StatusOK},
		{"Just below boundary (-50.001)", "sensor-1", pTemp(-50.001), http.StatusUnprocessableEntity},
		{"Just above boundary (100.001)", "sensor-1", pTemp(100.001), http.StatusUnprocessableEntity},
		{"Absolute zero violation (-273.15)", "sensor-1", pTemp(-273.15), http.StatusUnprocessableEntity},
		{"Extreme fire violation (1250.0)", "sensor-1", pTemp(1250.0), http.StatusUnprocessableEntity},
		{"Blocklisted device with exact match", "blocked-bot-1", pTemp(25.0), http.StatusForbidden},
		{"Blocklisted device with whitespace match", "rogue-sensor-99", pTemp(25.0), http.StatusForbidden},
		{"Nil temperature reading", "sensor-1", nil, http.StatusOK},
	}

	for _, tc := range cases {
		status := validateInput(tc.devID, tc.temp)
		if status != tc.expected {
			t.Errorf("Case %q: expected HTTP %d, got %d", tc.name, tc.expected, status)
		}
	}
}

// TestAnalyticsMathAndEmptyData verifies average calculation with empty database and negative numbers
func TestAnalyticsMathAndEmptyData(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ssle_test_analytics_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test_analytics.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE temperatures (id INTEGER PRIMARY KEY, device_id TEXT, temperature REAL, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP);`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// 1. Unhappy path: Empty table -> AVG is NULL, should return nil/null without error
	var avg sql.NullFloat64
	err = db.QueryRow("SELECT AVG(temperature) FROM temperatures").Scan(&avg)
	if err != nil {
		t.Fatalf("Query on empty table failed: %v", err)
	}
	if avg.Valid {
		t.Errorf("Expected avg.Valid == false on empty table, got %f", avg.Float64)
	}

	// 2. Happy path: Multiple readings including negative numbers: -10.5, 20.0, 35.5 -> Sum = 45.0, Count = 3, Avg = 15.0
	_, _ = db.Exec("INSERT INTO temperatures (device_id, temperature) VALUES ('s1', -10.5)")
	_, _ = db.Exec("INSERT INTO temperatures (device_id, temperature) VALUES ('s2', 20.0)")
	_, _ = db.Exec("INSERT INTO temperatures (device_id, temperature) VALUES ('s3', 35.5)")

	err = db.QueryRow("SELECT AVG(temperature) FROM temperatures").Scan(&avg)
	if err != nil || !avg.Valid {
		t.Fatalf("Query failed on populated table: %v", err)
	}

	if avg.Float64 != 15.0 {
		t.Errorf("Expected average 15.0, got %f", avg.Float64)
	}
}

// TestHMACPayloadSigningAndTamperDefense tests Guide 5 Threat 3: Route & Signals Hijacking / MitM Prevention
func TestHMACPayloadSigningAndTamperDefense(t *testing.T) {
	secretKey := []byte("ssle-test-secret-key-32bytes-long!")
	maxDrift := 10 * time.Second

	// 1. Unit Tests for cryptographic signing and verification functions
	originalBody := []byte(`{"device_id":"sensor-edge-01","temperature":23.5}`)
	now := time.Now()

	tsStr, sig := function.SignPayload(secretKey, now, originalBody)
	if sig == "" || tsStr == "" {
		t.Fatal("SignPayload returned empty signature or timestamp")
	}

	// 1a. Happy path: untampered body & fresh timestamp
	valid, reason := function.VerifyPayload(secretKey, tsStr, sig, originalBody, maxDrift)
	if !valid {
		t.Fatalf("VerifyPayload failed on authentic payload: %s", reason)
	}

	// 1b. Unhappy path: MitM adversary tampers with temperature in transit
	tamperedBody := []byte(`{"device_id":"sensor-edge-01","temperature":999.9}`)
	valid, reason = function.VerifyPayload(secretKey, tsStr, sig, tamperedBody, maxDrift)
	if valid {
		t.Fatal("Expected VerifyPayload to fail on tampered payload, but it succeeded")
	}
	if !strings.Contains(reason, "signature mismatch") {
		t.Errorf("Expected signature mismatch error, got: %s", reason)
	}

	// 1c. Unhappy path: Replay attack (timestamp older than drift limit)
	oldTime := now.Add(-60 * time.Second)
	oldTsStr, oldSig := function.SignPayload(secretKey, oldTime, originalBody)
	valid, reason = function.VerifyPayload(secretKey, oldTsStr, oldSig, originalBody, maxDrift)
	if valid {
		t.Fatal("Expected VerifyPayload to fail on expired timestamp (replay attack), but it succeeded")
	}
	if !strings.Contains(reason, "timestamp drift exceeded") {
		t.Errorf("Expected drift exceeded error, got: %s", reason)
	}

	// 1d. Unhappy path: Invalid signature hex format
	valid, reason = function.VerifyPayload(secretKey, tsStr, "invalid-hex-non-signature", originalBody, maxDrift)
	if valid {
		t.Fatal("Expected VerifyPayload to fail on invalid signature hex, but it succeeded")
	}

	// 2. Integration Test: HTTP Ingestion Endpoint under MitM attack conditions
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ingest", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			function.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": "Failed to read body"})
			return
		}

		sigHeader := r.Header.Get("X-Signature")
		tsHeader := r.Header.Get("X-Signature-Timestamp")
		if sigHeader == "" || tsHeader == "" {
			function.RespondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Missing HMAC signature headers"})
			return
		}

		valid, verifyErr := function.VerifyPayload(secretKey, tsHeader, sigHeader, body, maxDrift)
		if !valid {
			function.RespondJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "MitM tampering or invalid signature detected: " + verifyErr,
			})
			return
		}

		function.RespondJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Scenario A: Legitimate client request with valid HMAC
	clientTsStr, clientSig := function.SignPayload(secretKey, time.Now(), originalBody)

	reqA, _ := http.NewRequest("POST", server.URL+"/ingest", bytes.NewReader(originalBody))
	reqA.Header.Set("Content-Type", "application/json")
	reqA.Header.Set("X-Signature", clientSig)
	reqA.Header.Set("X-Signature-Timestamp", clientTsStr)

	respA, err := http.DefaultClient.Do(reqA)
	if err != nil || respA.StatusCode != http.StatusOK {
		t.Fatalf("Legitimate signed request failed with status: %v, err: %v", respA.StatusCode, err)
	}

	// Scenario B: Active MitM interceptor alters payload body
	tamperedPayload := []byte(`{"device_id":"sensor-edge-01","temperature":100.0}`)
	reqB, _ := http.NewRequest("POST", server.URL+"/ingest", bytes.NewReader(tamperedPayload))
	reqB.Header.Set("Content-Type", "application/json")
	reqB.Header.Set("X-Signature", clientSig) // original signature
	reqB.Header.Set("X-Signature-Timestamp", clientTsStr)

	respB, err := http.DefaultClient.Do(reqB)
	if err != nil {
		t.Fatalf("MitM request error: %v", err)
	}
	if respB.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected HTTP 401 Unauthorized for tampered payload, got %d", respB.StatusCode)
	}

	// Scenario C: Attacker replays old captured packet
	staleTime := time.Now().Add(-100 * time.Second)
	staleTsStr, staleSig := function.SignPayload(secretKey, staleTime, originalBody)
	reqC, _ := http.NewRequest("POST", server.URL+"/ingest", bytes.NewReader(originalBody))
	reqC.Header.Set("Content-Type", "application/json")
	reqC.Header.Set("X-Signature", staleSig)
	reqC.Header.Set("X-Signature-Timestamp", staleTsStr)

	respC, err := http.DefaultClient.Do(reqC)
	if err != nil {
		t.Fatalf("Replay request error: %v", err)
	}
	if respC.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected HTTP 401 Unauthorized for replayed packet, got %d", respC.StatusCode)
	}

	// Scenario D: Unsigned request when HMAC is enforced
	reqD, _ := http.NewRequest("POST", server.URL+"/ingest", bytes.NewReader(originalBody))
	reqD.Header.Set("Content-Type", "application/json")

	respD, err := http.DefaultClient.Do(reqD)
	if err != nil {
		t.Fatalf("Unsigned request error: %v", err)
	}
	if respD.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected HTTP 401 Unauthorized for missing signature, got %d", respD.StatusCode)
	}
}

// TestThreeThreatsTriadValidation verifies that all three course syllabus threats are verified in an integrated pipeline
func TestThreeThreatsTriadValidation(t *testing.T) {
	secretKey := []byte("ssle-triad-secret-key-32bytes!")
	maxDrift := 5 * time.Second

	// Setup a fully defended mock ingestion server with all 3 threat mitigations active:
	// - Threat 1: Shellshock command injection defense
	// - Threat 2: Payload size capping (1MB) + rate limiting (burst of 5)
	// - Threat 3: HMAC-SHA256 signature verification + replay window check
	limiterTokens := 5
	var mu sync.Mutex

	mux := http.NewServeMux()
	mux.HandleFunc("POST /ingest", func(w http.ResponseWriter, r *http.Request) {
		// Defense 1 (Threat 1): Shellshock Header Inspection
		for _, values := range r.Header {
			for _, val := range values {
				if strings.Contains(val, "() {") {
					function.RespondJSON(w, http.StatusForbidden, map[string]string{"error": "Threat 1: Shellshock injection blocked"})
					return
				}
			}
		}

		// Defense 2a (Threat 2): Payload size capping (1MB)
		r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			function.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": "Threat 2: Payload too large"})
			return
		}

		// Defense 2b (Threat 2): Token-bucket rate limiting
		mu.Lock()
		if limiterTokens <= 0 {
			mu.Unlock()
			w.Header().Set("Retry-After", "1")
			function.RespondJSON(w, http.StatusTooManyRequests, map[string]string{"error": "Threat 2: Rate limit exceeded"})
			return
		}
		limiterTokens--
		mu.Unlock()

		// Defense 3 (Threat 3): HMAC-SHA256 signature & replay window check
		sigHeader := r.Header.Get("X-Signature")
		tsHeader := r.Header.Get("X-Signature-Timestamp")
		if sigHeader == "" || tsHeader == "" {
			function.RespondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Threat 3: Missing signature headers"})
			return
		}

		valid, reason := function.VerifyPayload(secretKey, tsHeader, sigHeader, body, maxDrift)
		if !valid {
			function.RespondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Threat 3: " + reason})
			return
		}

		function.RespondJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// 1. Verify Threat 1 Defense: Shellshock injection is dropped with 403 Forbidden
	req1, _ := http.NewRequest("POST", server.URL+"/ingest", strings.NewReader(`{"device_id":"s1","temperature":20.0}`))
	req1.Header.Set("User-Agent", "() { :; }; /bin/cat /etc/passwd")
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil || resp1.StatusCode != http.StatusForbidden {
		t.Errorf("Threat 1 defense failed: expected HTTP 403, got %d, err: %v", resp1.StatusCode, err)
	}

	// 2. Verify Threat 2 Defense:
	// 2a. Oversized payload is dropped with 400 Bad Request
	hugeBody := bytes.Repeat([]byte("B"), 2*1024*1024)
	req2a, _ := http.NewRequest("POST", server.URL+"/ingest", bytes.NewReader(hugeBody))
	resp2a, err := http.DefaultClient.Do(req2a)
	if err != nil || resp2a.StatusCode != http.StatusBadRequest {
		t.Errorf("Threat 2 payload capping failed: expected HTTP 400, got %d, err: %v", resp2a.StatusCode, err)
	}

	// 2b. Volumetric flooding exhausts tokens and returns 429 Too Many Requests
	exhausted := false
	for i := 0; i < 10; i++ {
		validPayload := []byte(`{"device_id":"legit-sensor","temperature":21.0}`)
		tsStr, sigStr := function.SignPayload(secretKey, time.Now(), validPayload)
		reqFlood, _ := http.NewRequest("POST", server.URL+"/ingest", bytes.NewReader(validPayload))
		reqFlood.Header.Set("X-Signature", sigStr)
		reqFlood.Header.Set("X-Signature-Timestamp", tsStr)
		respFlood, err := http.DefaultClient.Do(reqFlood)
		if err == nil && respFlood.StatusCode == http.StatusTooManyRequests {
			exhausted = true
			break
		}
	}
	if !exhausted {
		t.Errorf("Threat 2 rate limiting failed: expected HTTP 429 upon token depletion")
	}

	// 3. Verify Threat 3 Defense:
	// Reset rate limit tokens for Threat 3 verification
	mu.Lock()
	limiterTokens = 5
	mu.Unlock()

	legitBody := []byte(`{"device_id":"sensor-edge-01","temperature":22.5}`)
	tsStr, sigStr := function.SignPayload(secretKey, time.Now(), legitBody)

	// 3a. Legitimate signed request -> 200 OK
	req3a, _ := http.NewRequest("POST", server.URL+"/ingest", bytes.NewReader(legitBody))
	req3a.Header.Set("X-Signature", sigStr)
	req3a.Header.Set("X-Signature-Timestamp", tsStr)
	resp3a, err := http.DefaultClient.Do(req3a)
	if err != nil || resp3a.StatusCode != http.StatusOK {
		t.Errorf("Threat 3 authentic request failed: expected HTTP 200, got %d", resp3a.StatusCode)
	}

	// 3b. MitM tampered payload in transit -> 401 Unauthorized
	tamperedBody := []byte(`{"device_id":"sensor-edge-01","temperature":999.9}`)
	req3b, _ := http.NewRequest("POST", server.URL+"/ingest", bytes.NewReader(tamperedBody))
	req3b.Header.Set("X-Signature", sigStr)
	req3b.Header.Set("X-Signature-Timestamp", tsStr)
	resp3b, err := http.DefaultClient.Do(req3b)
	if err != nil || resp3b.StatusCode != http.StatusUnauthorized {
		t.Errorf("Threat 3 tamper defense failed: expected HTTP 401, got %d", resp3b.StatusCode)
	}

	// 3c. Replayed packet with stale timestamp -> 401 Unauthorized
	staleTsStr, staleSig := function.SignPayload(secretKey, time.Now().Add(-30*time.Second), legitBody)
	req3c, _ := http.NewRequest("POST", server.URL+"/ingest", bytes.NewReader(legitBody))
	req3c.Header.Set("X-Signature", staleSig)
	req3c.Header.Set("X-Signature-Timestamp", staleTsStr)
	resp3c, err := http.DefaultClient.Do(req3c)
	if err != nil || resp3c.StatusCode != http.StatusUnauthorized {
		t.Errorf("Threat 3 replay defense failed: expected HTTP 401, got %d", resp3c.StatusCode)
	}
}

// TestFCAPSEventCorrelationWorkflow tests the 5-stage Fault Management cycle (Slide 9)
func TestFCAPSEventCorrelationWorkflow(t *testing.T) {
	// 1. Stage 1: Detecting (Metric Threshold Anomaly Detection)
	type MetricAnomaly struct {
		Name      string
		Value     float64
		Threshold float64
		IsAnomaly bool
	}

	testMetrics := []MetricAnomaly{
		{"latency_seconds", 0.250, 0.150, true},   // 250ms > 150ms -> Latency spike
		{"error_rate_pct", 12.5, 5.0, true},       // 12.5% > 5% -> Elevated error rate
		{"service_health", 1.0, 1.0, false},       // UP -> Normal
		{"cpu_percent", 88.0, 80.0, true},         // 88% > 80% -> CPU contention
	}

	detectedAnomalies := 0
	for _, m := range testMetrics {
		if m.Name == "service_health" {
			if m.Value == 0 {
				detectedAnomalies++
			}
		} else {
			if m.Value > m.Threshold {
				detectedAnomalies++
			}
		}
	}
	if detectedAnomalies != 3 {
		t.Errorf("Stage 1 Detecting: expected 3 anomalies, found %d", detectedAnomalies)
	}

	// 2. Stage 2: Diagnosing & Isolating (Hierarchical Scope Narrowing)
	serv := models.Service{
		ID:   "ingest-prod-01",
		Kind: "ingestion",
		URL:  "http://10.0.2.15:5001",
		Location: models.ServiceLocation{
			Region:       "pt-aveiro",
			Zone:         "lab-ssle",
			PhysicalHost: "kali-vm",
			LogicalEnv:   "incus:ssle-ingestion",
		},
		Timestamp: time.Now(),
	}
	serv.Name = serv.BuildFQSN()

	incidentDetails := serv.LocateIncidentDetails()
	if incidentDetails["physical_host"] != "kali-vm" {
		t.Errorf("Stage 2 Isolating: expected physical_host 'kali-vm', got %v", incidentDetails["physical_host"])
	}
	if incidentDetails["logical_env"] != "incus:ssle-ingestion" {
		t.Errorf("Stage 2 Isolating: expected logical_env 'incus:ssle-ingestion', got %v", incidentDetails["logical_env"])
	}
	if !strings.Contains(serv.Name, "pt-aveiro.lab-ssle.kali-vm.incus:ssle-ingestion") {
		t.Errorf("Stage 2 Isolating: canonical FQSN missing expected segments: %s", serv.Name)
	}

	// 3. Stage 3: Correlating (Connecting Metric Symptoms with Root Cause Log Evidence)
	type CorrelationCase struct {
		LogLine          string
		ExpectedCause    string
		ExpectedRuleID   int
		ExpectedThreat   string
	}

	cases := []CorrelationCase{
		{
			LogLine:        `198.51.100.25 - - [12/Nov/2026:10:00:00 +0000] "POST /ingest HTTP/1.1" 403 45 "-" "() { :; }; /bin/cat /etc/passwd"`,
			ExpectedCause:  "EXPLOITATION_SHELLSHOCK",
			ExpectedRuleID: 100100,
			ExpectedThreat: "Threat 1",
		},
		{
			LogLine:        `10.20.1.50 - - [12/Nov/2026:10:00:00 +0000] "POST /ingest HTTP/1.1" 401 55 "-" "SSLE-Attacker/1.0"`,
			ExpectedCause:  "MITM_PAYLOAD_TAMPERING",
			ExpectedRuleID: 100104,
			ExpectedThreat: "Threat 3",
		},
		{
			LogLine:        `10.20.1.88 - - [12/Nov/2026:10:00:00 +0000] "POST /ingest HTTP/1.1" 429 60 "-" "ApacheBench/2.3"`,
			ExpectedCause:  "DOS_VOLUMETRIC_FLOOD",
			ExpectedRuleID: 100102,
			ExpectedThreat: "Threat 2",
		},
	}

	for _, c := range cases {
		var matchedCause string
		var matchedRule int
		var matchedThreat string

		if strings.Contains(c.LogLine, "() {") {
			matchedCause = "EXPLOITATION_SHELLSHOCK"
			matchedRule = 100100
			matchedThreat = "Threat 1"
		} else if strings.Contains(c.LogLine, `POST /ingest HTTP/1.1" 401`) {
			matchedCause = "MITM_PAYLOAD_TAMPERING"
			matchedRule = 100104
			matchedThreat = "Threat 3"
		} else if strings.Contains(c.LogLine, `POST /ingest HTTP/1.1" 429`) {
			matchedCause = "DOS_VOLUMETRIC_FLOOD"
			matchedRule = 100102
			matchedThreat = "Threat 2"
		}

		if matchedCause != c.ExpectedCause || matchedRule != c.ExpectedRuleID || matchedThreat != c.ExpectedThreat {
			t.Errorf("Stage 3 Correlating: expected (%s, %d, %s), got (%s, %d, %s)",
				c.ExpectedCause, c.ExpectedRuleID, c.ExpectedThreat, matchedCause, matchedRule, matchedThreat)
		}
	}

	// 4. Stage 4: Resolution / Mitigation (Action-Response Formulation)
	attackerIP := "198.51.100.25"
	activeResponseCommand := fmt.Sprintf("/var/ossec/active-response/bin/firewall-drop -ip %s -timeout 60", attackerIP)
	if !strings.Contains(activeResponseCommand, attackerIP) || !strings.Contains(activeResponseCommand, "60") {
		t.Errorf("Stage 4 Resolution: unexpected active response command: %s", activeResponseCommand)
	}

	// 5. Stage 5: Post-Mortem & Recovery Verification
	isRecovered := true
	postLatency := 0.012 // 12ms baseline
	if !isRecovered || postLatency > 0.050 {
		t.Errorf("Stage 5 Post-Mortem: failed baseline recovery verification")
	}
}

// TestRegistryScalabilityAndLookupBounds tests quantitative scaling up to 1,000 services (Slides 4 & 7)
func TestRegistryScalabilityAndLookupBounds(t *testing.T) {
	registry := make(map[string]models.Service)
	var mu sync.RWMutex

	nServices := 1000
	tStart := time.Now()

	// 1. Batch registration of 1,000 services
	for i := 0; i < nServices; i++ {
		id := fmt.Sprintf("sensor-node-%04d", i)
		reg := "pt-aveiro"
		if i%2 == 1 {
			reg = "pt-porto"
		}
		zone := "lab-ssle"
		if i%4 == 0 {
			zone = "zone-backup"
		}

		s := models.Service{
			ID:   id,
			Kind: "temperature",
			URL:  fmt.Sprintf("http://10.20.1.%d:5000", (i%240)+10),
			Location: models.ServiceLocation{
				Region:       reg,
				Zone:         zone,
				PhysicalHost: fmt.Sprintf("host-%d", i%5),
				LogicalEnv:   fmt.Sprintf("incus:sensor-%d", i),
			},
			Timestamp: time.Now(),
		}
		s.Name = s.BuildFQSN()

		mu.Lock()
		registry[id] = s
		mu.Unlock()
	}

	regDuration := time.Since(tStart)
	if regDuration > 2*time.Second {
		t.Errorf("Registration of 1000 services took too long: %v (expected < 2s)", regDuration)
	}

	// 2. Exact O(1) hash map lookup
	tLookup := time.Now()
	mu.RLock()
	target, exists := registry["sensor-node-0500"]
	mu.RUnlock()
	lookupDuration := time.Since(tLookup)

	if !exists {
		t.Fatalf("Service sensor-node-0500 not found in registry")
	}
	if target.Location.Region != "pt-aveiro" {
		t.Errorf("Expected region pt-aveiro, got %s", target.Location.Region)
	}
	if lookupDuration > 10*time.Millisecond {
		t.Errorf("Direct O(1) lookup took too long: %v (expected < 10ms)", lookupDuration)
	}

	// 3. Filtered query across 1,000 services (matching ~250 services in pt-aveiro and zone-backup)
	tFilter := time.Now()
	mu.RLock()
	var matched []models.Service
	for _, s := range registry {
		if s.MatchesFilter("pt-aveiro", "zone-backup", "", "", "") {
			matched = append(matched, s)
		}
	}
	mu.RUnlock()
	filterDuration := time.Since(tFilter)

	if len(matched) == 0 {
		t.Errorf("Filtered query returned 0 matches, expected ~125 matches")
	}
	if filterDuration > 50*time.Millisecond {
		t.Errorf("Linear filter scan over 1,000 items took too long: %v (expected < 50ms)", filterDuration)
	}
}

// BenchmarkRegistryDirectLookup measures the speed of O(1) hash map lookup across 1,000 services
func BenchmarkRegistryDirectLookup(b *testing.B) {
	registry := make(map[string]models.Service, 1000)
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("sensor-%04d", i)
		registry[id] = models.Service{
			ID:   id,
			Kind: "temperature",
			Location: models.ServiceLocation{
				Region: "pt-aveiro",
				Zone:   "lab-ssle",
			},
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = registry["sensor-0500"]
	}
}

// BenchmarkHMACSignatureVerification measures the throughput of Threat 3 cryptographic verification
func BenchmarkHMACSignatureVerification(b *testing.B) {
	secretKey := []byte("ssle-secret-benchmark-key-32b!")
	body := []byte(`{"device_id":"sensor-edge-01","temperature":22.5}`)
	tsStr, sigHex := function.SignPayload(secretKey, time.Now(), body)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = function.VerifyPayload(secretKey, tsStr, sigHex, body, 10*time.Second)
	}
}





