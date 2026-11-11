package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type TargetService struct {
	Kind     string `json:"kind"`
	URL      string `json:"url"`
	ID       string `json:"id"`
	Endpoint string
}

type HealthSample struct {
	Service      string
	Endpoint     string
	IsAlive      float64
	ResponseTime float64
}

var (
	registryURL   = getEnv("REGISTRY_URL", "http://localhost:5050")
	port          = getEnv("PORT", "8000")
	checkInterval = 3 * time.Second

	httpClient = &http.Client{
		Timeout: 2500 * time.Millisecond,
	}

	mu      sync.RWMutex
	samples []HealthSample
)

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

var fallbackServices = []TargetService{
	{Kind: "registry", URL: "http://localhost:5050", Endpoint: "http://localhost:5050/health"},
	{Kind: "ingestion", URL: "http://localhost:5001", Endpoint: "http://localhost:5001/health"},
	{Kind: "storage", URL: "http://localhost:5002", Endpoint: "http://localhost:5002/health"},
	{Kind: "analytics", URL: "http://localhost:5003", Endpoint: "http://localhost:5003/health"},
	{Kind: "temperature", URL: "http://localhost:5000", Endpoint: "http://localhost:5000/health"},
}

func discoverTargets() []TargetService {
	servicesEndpoint := strings.TrimSuffix(registryURL, "/") + "/services"
	resp, err := httpClient.Get(servicesEndpoint)
	if err != nil || resp.StatusCode != http.StatusOK {
		return fallbackServices
	}
	defer resp.Body.Close()

	var regMap map[string]TargetService
	if err := json.NewDecoder(resp.Body).Decode(&regMap); err != nil || len(regMap) == 0 {
		return fallbackServices
	}

	targets := make([]TargetService, 0, len(regMap)+1)
	// Always include registry itself
	targets = append(targets, TargetService{
		Kind:     "registry",
		URL:      registryURL,
		Endpoint: strings.TrimSuffix(registryURL, "/") + "/health",
	})

	for id, svc := range regMap {
		if svc.URL != "" {
			svc.ID = id
			svc.Endpoint = strings.TrimSuffix(svc.URL, "/") + "/health"
			targets = append(targets, svc)
		}
	}
	return targets
}

func probeServices() {
	targets := discoverTargets()
	results := make([]HealthSample, 0, len(targets))

	for _, t := range targets {
		start := time.Now()
		alive := 0.0

		resp, err := httpClient.Get(t.Endpoint)
		duration := time.Since(start).Seconds()

		if err == nil {
			if resp.StatusCode == http.StatusOK {
				alive = 1.0
			} else {
				// Fallback probe base url if /health is not 200
				resp.Body.Close()
				respBase, errBase := httpClient.Get(t.URL)
				if errBase == nil {
					if respBase.StatusCode < 500 {
						alive = 1.0
					}
					respBase.Body.Close()
				}
			}
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
		}

		results = append(results, HealthSample{
			Service:      t.Kind,
			Endpoint:     t.Endpoint,
			IsAlive:      alive,
			ResponseTime: duration,
		})

		statusStr := "UP"
		if alive == 0.0 {
			statusStr = "DOWN"
		}
		log.Printf("[Health Probe] Service: %-12s Endpoint: %-32s Status: %-4s (%.4fs)\n",
			t.Kind, t.Endpoint, statusStr, duration)
	}

	mu.Lock()
	samples = results
	mu.Unlock()
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	current := make([]HealthSample, len(samples))
	copy(current, samples)
	mu.RUnlock()

	// Sort for stable output
	sort.Slice(current, func(i, j int) bool {
		if current[i].Service == current[j].Service {
			return current[i].Endpoint < current[j].Endpoint
		}
		return current[i].Service < current[j].Service
	})

	var sb strings.Builder

	// Metric 1: service_health
	sb.WriteString("# HELP service_health Health status of the service (1 = UP, 0 = DOWN)\n")
	sb.WriteString("# TYPE service_health gauge\n")
	for _, s := range current {
		sb.WriteString(fmt.Sprintf("service_health{endpoint=%q,service=%q} %.1f\n", s.Endpoint, s.Service, s.IsAlive))
	}

	sb.WriteString("\n")

	// Metric 2: service_response_time_seconds
	sb.WriteString("# HELP service_response_time_seconds HTTP health check response time in seconds\n")
	sb.WriteString("# TYPE service_response_time_seconds gauge\n")
	for _, s := range current {
		sb.WriteString(fmt.Sprintf("service_response_time_seconds{endpoint=%q,service=%q} %.6f\n", s.Endpoint, s.Service, s.ResponseTime))
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(sb.String()))
}

func main() {
	log.Printf("==============================================================")
	log.Printf("  SSLE Practical Guide 3: Service Health Exporter (Go)")
	log.Printf("  Listening on:     :%s", port)
	log.Printf("  Registry URL:     %s", registryURL)
	log.Printf("  Metrics endpoint: http://localhost:%s/metrics", port)
	log.Printf("==============================================================")

	// Background health checking loop
	go func() {
		probeServices()
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()
		for range ticker.C {
			probeServices()
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", metricsHandler)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"UP","service":"health_exporter"}`))
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><h1>SSLE Health Exporter</h1><p><a href="/metrics">Metrics</a></p></body></html>`))
	})

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Health exporter server failed: %v", err)
	}
}
