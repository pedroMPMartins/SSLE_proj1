package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"simple_soa/internal/function"
	"simple_soa/internal/models"
	"sort"
	"strings"
	"sync"
	"time"
)

type TargetService struct {
	Kind     string                 `json:"kind"`
	URL      string                 `json:"url"`
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Location models.ServiceLocation `json:"location"`
	Endpoint string
}

type HealthSample struct {
	Service      string
	Endpoint     string
	Region       string
	Zone         string
	PhysicalHost string
	LogicalEnv   string
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
	{Kind: "registry", URL: "http://localhost:5050", Endpoint: "http://localhost:5050/health", Location: models.ServiceLocation{Region: "pt-aveiro", Zone: "lab-ssle", PhysicalHost: "kali-vm", LogicalEnv: "incus:ssle-registry"}},
	{Kind: "ingestion", URL: "http://localhost:5001", Endpoint: "http://localhost:5001/health", Location: models.ServiceLocation{Region: "pt-aveiro", Zone: "lab-ssle", PhysicalHost: "kali-vm", LogicalEnv: "incus:ssle-ingestion"}},
	{Kind: "storage", URL: "http://localhost:5002", Endpoint: "http://localhost:5002/health", Location: models.ServiceLocation{Region: "pt-aveiro", Zone: "lab-ssle", PhysicalHost: "kali-vm", LogicalEnv: "incus:ssle-storage"}},
	{Kind: "analytics", URL: "http://localhost:5003", Endpoint: "http://localhost:5003/health", Location: models.ServiceLocation{Region: "pt-aveiro", Zone: "lab-ssle", PhysicalHost: "kali-vm", LogicalEnv: "incus:ssle-analytics"}},
	{Kind: "temperature", URL: "http://localhost:5000", Endpoint: "http://localhost:5000/health", Location: models.ServiceLocation{Region: "pt-aveiro", Zone: "lab-ssle", PhysicalHost: "kali-vm", LogicalEnv: "incus:ssle-temperature"}},
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
		Location: models.ServiceLocation{
			Region:       getEnv("SERVICE_REGION", "pt-aveiro"),
			Zone:         getEnv("SERVICE_ZONE", "lab-ssle"),
			PhysicalHost: getEnv("PHYSICAL_HOST", "kali-vm"),
			LogicalEnv:   "incus:ssle-registry",
		},
	})

	for id, svc := range regMap {
		if svc.URL != "" {
			svc.ID = id
			svc.Endpoint = strings.TrimSuffix(svc.URL, "/") + "/health"
			if svc.Location.Region == "" {
				svc.Location.Region = "pt-aveiro"
			}
			if svc.Location.Zone == "" {
				svc.Location.Zone = "lab-ssle"
			}
			if svc.Location.PhysicalHost == "" {
				svc.Location.PhysicalHost = "kali-vm"
			}
			if svc.Location.LogicalEnv == "" {
				svc.Location.LogicalEnv = "incus:ssle-" + svc.Kind
			}
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
		resp, err := httpClient.Get(t.Endpoint)
		duration := time.Since(start).Seconds()

		isAlive := 0.0
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				isAlive = 1.0
			}
			resp.Body.Close()
		}

		results = append(results, HealthSample{
			Service:      t.Kind,
			Endpoint:     t.Endpoint,
			Region:       t.Location.Region,
			Zone:         t.Location.Zone,
			PhysicalHost: t.Location.PhysicalHost,
			LogicalEnv:   t.Location.LogicalEnv,
			IsAlive:      isAlive,
			ResponseTime: duration,
		})

		statusStr := "DOWN"
		if isAlive == 1.0 {
			statusStr = "UP"
		}
		log.Printf("[Health Probe] Service: %-12s | Endpoint: %-30s | Status: %-4s | Latency: %.4fs (Region: %s, Host: %s)\n",
			t.Kind, t.Endpoint, statusStr, duration, t.Location.Region, t.Location.PhysicalHost)
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
		sb.WriteString(fmt.Sprintf("service_health{endpoint=%q,logical_env=%q,physical_host=%q,region=%q,service=%q,zone=%q} %.1f\n",
			s.Endpoint, s.LogicalEnv, s.PhysicalHost, s.Region, s.Service, s.Zone, s.IsAlive))
	}

	sb.WriteString("\n")

	// Metric 2: service_response_time_seconds
	sb.WriteString("# HELP service_response_time_seconds HTTP health check response time in seconds\n")
	sb.WriteString("# TYPE service_response_time_seconds gauge\n")
	for _, s := range current {
		sb.WriteString(fmt.Sprintf("service_response_time_seconds{endpoint=%q,logical_env=%q,physical_host=%q,region=%q,service=%q,zone=%q} %.6f\n",
			s.Endpoint, s.LogicalEnv, s.PhysicalHost, s.Region, s.Service, s.Zone, s.ResponseTime))
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

	// Run initial probe synchronously so metrics are ready immediately
	probeServices()

	// Periodic probe loop
	go func() {
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

	// Wrap mux with generic DoS protection (token-bucket rate limiting and payload clamping)
	dosCfg := function.LoadDoSConfigFromEnv()
	protectedHandler := function.DoSProtectionMiddleware(mux, dosCfg)

	log.Printf("Health Exporter listening on :%s (DoS Defense: RateLimit=%v [RPS=%.1f], MaxBodyBytes=%d)\n",
		port, dosCfg.EnableRateLimit, dosCfg.RateLimitRPS, dosCfg.MaxBodyBytes)

	server := function.NewHardenedServer(":"+port, protectedHandler)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Health Exporter server failed: %v", err)
	}
}
