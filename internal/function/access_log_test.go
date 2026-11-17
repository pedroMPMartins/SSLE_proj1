package function

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccessLoggerAndMiddleware(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_access.log")

	logger := NewAccessLogger("test_service", logPath)
	if logger == nil || logger.file == nil {
		t.Fatalf("Failed to initialize test AccessLogger")
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		RespondJSON(w, http.StatusOK, map[string]string{"result": "success"})
	})

	loggedHandler := AccessLoggingMiddleware(logger, handler)

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test/endpoint", nil)
	req.RemoteAddr = "10.0.0.42:12345"
	req.Header.Set("User-Agent", "TestAgent/1.0")

	loggedHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected HTTP 200, got %d", rec.Code)
	}

	logger.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read created access log: %v", err)
	}

	logStr := string(data)
	if !strings.Contains(logStr, "10.0.0.42") {
		t.Errorf("Access log missing client IP 10.0.0.42: %s", logStr)
	}
	if !strings.Contains(logStr, "GET /test/endpoint HTTP/1.1") {
		t.Errorf("Access log missing request method/URI: %s", logStr)
	}
	if !strings.Contains(logStr, "200") {
		t.Errorf("Access log missing status 200: %s", logStr)
	}
	if !strings.Contains(logStr, "TestAgent/1.0") {
		t.Errorf("Access log missing User-Agent: %s", logStr)
	}
}
