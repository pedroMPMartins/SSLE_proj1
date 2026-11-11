package function_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"simple_soa/internal/function"
	"simple_soa/internal/models"
	"testing"
	"time"
)

func TestGenerateRandomID(t *testing.T) {
	id1 := function.GenerateRandomID()
	id2 := function.GenerateRandomID()

	if len(id1) != 32 {
		t.Errorf("Expected 32-character hex string, got %d chars (%s)", len(id1), id1)
	}

	if id1 == id2 {
		t.Errorf("GenerateRandomID produced identical IDs: %s == %s", id1, id2)
	}
}

func TestRespondJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	payload := map[string]string{"message": "test_ok", "status": "active"}

	function.RespondJSON(rec, http.StatusCreated, payload)

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected HTTP status 201 Created, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", ct)
	}

	var decoded map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&decoded); err != nil {
		t.Fatalf("Failed to decode response JSON: %v", err)
	}

	if decoded["message"] != "test_ok" || decoded["status"] != "active" {
		t.Errorf("Decoded payload does not match expected: %+v", decoded)
	}
}

func TestDiscoverServices(t *testing.T) {
	mockServices := []models.Service{
		{
			Kind:      "storage",
			ID:        "storage-01",
			URL:       "http://localhost:15002",
			Timestamp: time.Now().UTC(),
		},
		{
			Kind:      "storage",
			ID:        "storage-02",
			URL:       "http://localhost:15004",
			Timestamp: time.Now().UTC(),
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/services/storage" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(mockServices)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	discovered, err := function.DiscoverServices(server.URL, "storage")
	if err != nil {
		t.Fatalf("DiscoverServices failed: %v", err)
	}

	if len(discovered) != 2 {
		t.Fatalf("Expected 2 services, got %d", len(discovered))
	}

	if discovered[0].ID != "storage-01" || discovered[1].ID != "storage-02" {
		t.Errorf("Discovered services do not match mock data: %+v", discovered)
	}
}

func TestDiscoverOneService(t *testing.T) {
	mockServices := []models.Service{
		{
			Kind:      "ingestion",
			ID:        "ingest-01",
			URL:       "http://localhost:15001",
			Timestamp: time.Now().UTC(),
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/services/ingestion" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(mockServices)
			return
		}
		if r.URL.Path == "/services/nonexistent" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]models.Service{})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	one, err := function.DiscoverOneService(server.URL, "ingestion")
	if err != nil {
		t.Fatalf("DiscoverOneService failed: %v", err)
	}

	if one.ID != "ingest-01" {
		t.Errorf("Expected service ID 'ingest-01', got %q", one.ID)
	}

	// Test non-existent kind returns error
	_, errEmpty := function.DiscoverOneService(server.URL, "nonexistent")
	if errEmpty == nil {
		t.Errorf("Expected error when no services of kind exist, got nil")
	}
}

func TestHierarchicalFQSN(t *testing.T) {
	svc := models.Service{
		ID:   "srv-01",
		Kind: "ingestion",
		Location: models.ServiceLocation{
			Region:       "pt-aveiro",
			Zone:         "zone-alpha",
			PhysicalHost: "host-node1",
			LogicalEnv:   "incus:ssle-ingest",
		},
	}

	fqsn := svc.BuildFQSN()
	expected := "pt-aveiro.zone-alpha.host-node1.incus:ssle-ingest.srv-01.ingestion"
	if fqsn != expected {
		t.Errorf("Expected FQSN %q, got %q", expected, fqsn)
	}

	// Test parsing back
	var parsed models.Service
	if err := parsed.ParseFQSN(fqsn); err != nil {
		t.Fatalf("Failed to parse FQSN: %v", err)
	}

	if parsed.Location.Region != "pt-aveiro" ||
		parsed.Location.Zone != "zone-alpha" ||
		parsed.Location.PhysicalHost != "host-node1" ||
		parsed.Location.LogicalEnv != "incus:ssle-ingest" ||
		parsed.ID != "srv-01" ||
		parsed.Kind != "ingestion" {
		t.Errorf("Parsed fields do not match original: %+v", parsed)
	}
}

func TestHierarchicalFilter(t *testing.T) {
	svc := models.Service{
		ID:   "storage-01",
		Kind: "storage",
		Location: models.ServiceLocation{
			Region:       "pt-aveiro",
			Zone:         "lab-ssle",
			PhysicalHost: "kali-vm",
			LogicalEnv:   "incus:ssle-storage",
		},
	}

	// Positive matches
	if !svc.MatchesFilter("pt-aveiro", "lab-ssle", "kali-vm", "incus:ssle-storage", "storage") {
		t.Errorf("Expected exact match to succeed")
	}
	if !svc.MatchesFilter("pt-aveiro", "", "", "", "storage") {
		t.Errorf("Expected wildcard match to succeed")
	}

	// Negative matches
	if svc.MatchesFilter("eu-west", "", "", "", "") {
		t.Errorf("Expected region mismatch to fail")
	}
	if svc.MatchesFilter("", "", "", "", "ingestion") {
		t.Errorf("Expected kind mismatch to fail")
	}
}

func TestIncidentLocalizationDetails(t *testing.T) {
	svc := models.Service{
		ID:   "ingest-01",
		Kind: "ingestion",
		Name: "pt-aveiro.lab-ssle.kali-vm.incus:ssle-ingestion.ingest-01.ingestion",
		URL:  "http://10.0.4.10:5001",
		Location: models.ServiceLocation{
			Region:       "pt-aveiro",
			Zone:         "lab-ssle",
			PhysicalHost: "kali-vm",
			LogicalEnv:   "incus:ssle-ingestion",
		},
		Timestamp: time.Now().UTC(),
	}

	incident := svc.LocateIncidentDetails()
	if incident["service_id"] != "ingest-01" {
		t.Errorf("Expected service_id 'ingest-01', got %v", incident["service_id"])
	}
	if incident["physical_host"] != "kali-vm" {
		t.Errorf("Expected physical_host 'kali-vm', got %v", incident["physical_host"])
	}
	if incident["administrative_domain"] != "pt-aveiro/lab-ssle" {
		t.Errorf("Expected administrative_domain 'pt-aveiro/lab-ssle', got %v", incident["administrative_domain"])
	}
}

func TestDiscoverLocally(t *testing.T) {
	localSvc := models.Service{
		ID:   "ingest-local",
		Kind: "ingestion",
		URL:  "http://10.0.1.1:5001",
		Location: models.ServiceLocation{
			Region: "pt-aveiro",
			Zone:   "zone-a",
		},
	}
	remoteSvc := models.Service{
		ID:   "ingest-remote",
		Kind: "ingestion",
		URL:  "http://10.0.2.1:5001",
		Location: models.ServiceLocation{
			Region: "us-east",
			Zone:   "zone-b",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		region := q.Get("region")
		zone := q.Get("zone")

		w.Header().Set("Content-Type", "application/json")
		if region == "pt-aveiro" && zone == "zone-a" {
			json.NewEncoder(w).Encode([]models.Service{localSvc})
			return
		}
		if region == "pt-aveiro" {
			json.NewEncoder(w).Encode([]models.Service{localSvc})
			return
		}
		// Global
		json.NewEncoder(w).Encode([]models.Service{localSvc, remoteSvc})
	}))
	defer server.Close()

	// 1. Should pick local zone instance
	chosen, err := function.DiscoverLocally(server.URL, "ingestion", models.ServiceLocation{
		Region: "pt-aveiro",
		Zone:   "zone-a",
	})
	if err != nil {
		t.Fatalf("DiscoverLocally failed: %v", err)
	}
	if chosen.ID != "ingest-local" {
		t.Errorf("Expected local service 'ingest-local', got %s", chosen.ID)
	}

	// 2. Fallback when requested zone is absent but region matches
	chosenRegional, err := function.DiscoverLocally(server.URL, "ingestion", models.ServiceLocation{
		Region: "pt-aveiro",
		Zone:   "zone-nonexistent",
	})
	if err != nil {
		t.Fatalf("DiscoverLocally regional fallback failed: %v", err)
	}
	if chosenRegional.ID != "ingest-local" {
		t.Errorf("Expected regional fallback to find 'ingest-local', got %s", chosenRegional.ID)
	}
}
