package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DiagnosticMetrics struct {
	CPUUsagePercent   map[string]float64
	MemoryUsageBytes  map[string]float64
	RequestsPerSecond map[string]float64
	ErrorRatePercent  map[string]float64
}

var (
	port          = getEnv("PORT", "8001")
	checkInterval = 5 * time.Second
	httpClient    = &http.Client{Timeout: 1500 * time.Millisecond}

	mu      sync.RWMutex
	metrics = DiagnosticMetrics{
		CPUUsagePercent:   make(map[string]float64),
		MemoryUsageBytes:  make(map[string]float64),
		RequestsPerSecond: make(map[string]float64),
		ErrorRatePercent:  make(map[string]float64),
	}
)

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

var incusContainers = []struct {
	service   string
	container string
}{
	{"registry", "ssle-registry"},
	{"ingestion", "ssle-ingestion"},
	{"storage", "ssle-storage"},
	{"analytics", "ssle-analytics"},
	{"temperature", "ssle-temperature"},
}

var processPatterns = []struct {
	service string
	pattern string
}{
	{"registry", "registry"},
	{"ingestion", "ingestion"},
	{"storage", "storage"},
	{"analytics", "analytics"},
	{"temperature", "temperature"},
	{"attacker", "attacker"},
}

func collectIncusStats() bool {
	foundAny := false
	for _, ic := range incusContainers {
		cmd := exec.Command("incus", "info", ic.container, "--format", "json")
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		foundAny = true

		var info struct {
			State struct {
				Memory struct {
					Usage int64 `json:"usage"`
				} `json:"memory"`
				CPU struct {
					Usage int64 `json:"usage"`
				} `json:"cpu"`
			} `json:"state"`
		}

		if err := json.Unmarshal(out, &info); err == nil {
			mu.Lock()
			metrics.MemoryUsageBytes[ic.service] = float64(info.State.Memory.Usage)
			metrics.CPUUsagePercent[ic.service] = float64(info.State.CPU.Usage) / 1e9 // seconds scale
			mu.Unlock()
		}
	}
	return foundAny
}

func collectProcessStats() {
	// Fallback when Incus is not present or running outside containers
	for _, pp := range processPatterns {
		// Run ps command to get %cpu and rss (in KB)
		cmdStr := fmt.Sprintf("ps -eo %%cpu,rss,args | grep '%s' | grep -v grep | grep -v diagnostics | head -n 1", pp.pattern)
		out, err := exec.Command("sh", "-c", cmdStr).Output()
		if err != nil {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(string(out)))
		if len(fields) >= 2 {
			cpu, _ := strconv.ParseFloat(fields[0], 64)
			rssKB, _ := strconv.ParseFloat(fields[1], 64)

			mu.Lock()
			metrics.CPUUsagePercent[pp.service] = cpu
			metrics.MemoryUsageBytes[pp.service] = rssKB * 1024
			mu.Unlock()
		}
	}
}

func probeThroughputAndErrors() {
	endpoints := []struct {
		service string
		url     string
	}{
		{"ingestion", "http://localhost:5001/health"},
		{"analytics", "http://localhost:5003/average"},
		{"registry", "http://localhost:5050/services"},
	}

	for _, ep := range endpoints {
		totalCalls := 5
		errors := 0
		start := time.Now()

		for i := 0; i < totalCalls; i++ {
			resp, err := httpClient.Get(ep.url)
			if err != nil || resp.StatusCode >= 400 {
				errors++
			}
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
		}

		duration := time.Since(start).Seconds()
		if duration <= 0 {
			duration = 0.001
		}
		rps := float64(totalCalls) / duration
		errPct := (float64(errors) / float64(totalCalls)) * 100.0

		mu.Lock()
		metrics.RequestsPerSecond[ep.service] = rps
		metrics.ErrorRatePercent[ep.service] = errPct
		mu.Unlock()
	}
}

func collectDiagnostics() {
	if !collectIncusStats() {
		collectProcessStats()
	}
	probeThroughputAndErrors()
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	defer mu.RUnlock()

	var sb strings.Builder

	// Metric 1: CPU usage
	sb.WriteString("# HELP service_container_cpu_usage_percent Container or process CPU utilization percentage\n")
	sb.WriteString("# TYPE service_container_cpu_usage_percent gauge\n")
	for svc, val := range metrics.CPUUsagePercent {
		sb.WriteString(fmt.Sprintf("service_container_cpu_usage_percent{container=%q,service=%q} %.2f\n", "proc-"+svc, svc, val))
	}
	sb.WriteString("\n")

	// Metric 2: Memory usage bytes
	sb.WriteString("# HELP service_container_memory_usage_bytes Container or process resident memory usage in bytes\n")
	sb.WriteString("# TYPE service_container_memory_usage_bytes gauge\n")
	for svc, val := range metrics.MemoryUsageBytes {
		sb.WriteString(fmt.Sprintf("service_container_memory_usage_bytes{container=%q,service=%q} %.0f\n", "proc-"+svc, svc, val))
	}
	sb.WriteString("\n")

	// Metric 3: Requests per second
	sb.WriteString("# HELP service_requests_per_second Observed request throughput per second\n")
	sb.WriteString("# TYPE service_requests_per_second gauge\n")
	for svc, val := range metrics.RequestsPerSecond {
		sb.WriteString(fmt.Sprintf("service_requests_per_second{service=%q} %.2f\n", svc, val))
	}
	sb.WriteString("\n")

	// Metric 4: Error rate percentage
	sb.WriteString("# HELP service_error_rate_percent Percentage of failed or errored requests\n")
	sb.WriteString("# TYPE service_error_rate_percent gauge\n")
	for svc, val := range metrics.ErrorRatePercent {
		sb.WriteString(fmt.Sprintf("service_error_rate_percent{service=%q} %.1f\n", svc, val))
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(sb.String()))
}

func main() {
	log.Printf("==============================================================")
	log.Printf("  SSLE Practical Guide 3: Diagnostics Exporter (Go)")
	log.Printf("  Listening on:     :%s", port)
	log.Printf("  Metrics endpoint: http://localhost:%s/metrics", port)
	log.Printf("==============================================================")

	go func() {
		collectDiagnostics()
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()
		for range ticker.C {
			collectDiagnostics()
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", metricsHandler)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"UP","service":"diagnostics_exporter"}`))
	})

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Diagnostics exporter server failed: %v", err)
	}
}
