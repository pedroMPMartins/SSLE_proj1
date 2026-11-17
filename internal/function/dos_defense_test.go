package function

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExtractClientIP(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://localhost/test", nil)
	req.RemoteAddr = "192.168.1.50:12345"
	if ip := ExtractClientIP(req); ip != "192.168.1.50" {
		t.Errorf("Expected 192.168.1.50, got %s", ip)
	}

	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	if ip := ExtractClientIP(req); ip != "10.0.0.1" {
		t.Errorf("Expected 10.0.0.1 from X-Forwarded-For, got %s", ip)
	}

	req.Header.Del("X-Forwarded-For")
	req.Header.Set("X-Real-IP", "172.16.0.99")
	if ip := ExtractClientIP(req); ip != "172.16.0.99" {
		t.Errorf("Expected 172.16.0.99 from X-Real-IP, got %s", ip)
	}
}

func TestRateLimiter(t *testing.T) {
	// 5 RPS, capacity 5
	rl := NewRateLimiter(5.0, 5.0)

	// Fire 5 requests immediately: all should pass
	for i := 0; i < 5; i++ {
		if !rl.Allow("1.2.3.4") {
			t.Fatalf("Request %d should be allowed", i+1)
		}
	}

	// 6th request immediately should be rejected
	if rl.Allow("1.2.3.4") {
		t.Fatalf("6th immediate request should be rejected by rate limiter")
	}

	// Different IP should still have its full bucket
	if !rl.Allow("5.6.7.8") {
		t.Fatalf("Request from different IP should be allowed")
	}
}

func TestDoSProtectionMiddleware_RateLimitAndPayloadClamping(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			RespondJSON(w, http.StatusBadRequest, map[string]string{"error": "Body read error"})
			return
		}
		RespondJSON(w, http.StatusOK, map[string]string{"status": "ok", "body_len": string(rune(len(body)))})
	})

	cfg := DoSProtectionConfig{
		EnableRateLimit:       true,
		RateLimitRPS:          2.0,
		EnablePayloadClamping: true,
		MaxBodyBytes:          1024, // 1KB limit for testing
		RateLimiter:           NewRateLimiter(2.0, 2.0),
	}

	protected := DoSProtectionMiddleware(handler, cfg)

	// 1. Normal request passes
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/data", bytes.NewReader([]byte("hello world")))
	req.RemoteAddr = "10.10.10.1:54321"
	protected.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("Expected HTTP 200, got %d", rec.Code)
	}

	// 2. Consume remaining token
	rec = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/data", bytes.NewReader([]byte("second request")))
	req.RemoteAddr = "10.10.10.1:54321"
	protected.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("Expected HTTP 200 for 2nd request, got %d", rec.Code)
	}

	// 3. Third request exceeds rate limit -> 429 Too Many Requests
	rec = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/data", bytes.NewReader([]byte("third request")))
	req.RemoteAddr = "10.10.10.1:54321"
	protected.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("Expected HTTP 429 for rate limit exceeded, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "1" {
		t.Errorf("Expected Retry-After: 1 header")
	}

	// 4. Oversized payload from fresh IP -> 400 Bad Request
	rec = httptest.NewRecorder()
	huge := bytes.Repeat([]byte("X"), 2048) // 2KB exceeds 1KB limit
	req, _ = http.NewRequest("POST", "/data", bytes.NewReader(huge))
	req.RemoteAddr = "10.10.10.2:54321" // fresh IP
	protected.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected HTTP 400 for oversized payload, got %d", rec.Code)
	}
}

func TestNewHardenedServer(t *testing.T) {
	mux := http.NewServeMux()
	server := NewHardenedServer(":8080", mux)

	if server.ReadHeaderTimeout != 3*time.Second {
		t.Errorf("Expected ReadHeaderTimeout 3s, got %v", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 5*time.Second {
		t.Errorf("Expected ReadTimeout 5s, got %v", server.ReadTimeout)
	}
	if server.WriteTimeout != 10*time.Second {
		t.Errorf("Expected WriteTimeout 10s, got %v", server.WriteTimeout)
	}
	if server.IdleTimeout != 15*time.Second {
		t.Errorf("Expected IdleTimeout 15s, got %v", server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 1<<20 {
		t.Errorf("Expected MaxHeaderBytes 1MB, got %v", server.MaxHeaderBytes)
	}
}
