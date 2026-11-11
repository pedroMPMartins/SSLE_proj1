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
