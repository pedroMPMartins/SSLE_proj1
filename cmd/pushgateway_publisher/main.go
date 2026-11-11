package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	pushgatewayURL = getEnv("PUSHGATEWAY_URL", "http://localhost:9091")
	registryURL    = getEnv("REGISTRY_URL", "http://localhost:5050")
	jobName        = getEnv("JOB_NAME", "ssle_push_monitor")
	pushInterval   = 5 * time.Second

	httpClient = &http.Client{Timeout: 2000 * time.Millisecond}
)

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

type ServiceProbe struct {
	Service  string
	Endpoint string
}

func main() {
	log.Printf("==============================================================")
	log.Printf("  SSLE Practical Guide 3: Pushgateway Metric Publisher (Go)")
	log.Printf("  Pushgateway Target: %s", pushgatewayURL)
	log.Printf("  Scraping Registry:  %s", registryURL)
	log.Printf("==============================================================")

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "host-vm"
	}

	pushEndpoint := fmt.Sprintf("%s/metrics/job/%s/instance/%s",
		strings.TrimSuffix(pushgatewayURL, "/"), jobName, hostname)

	endpoints := []ServiceProbe{
		{"registry", strings.TrimSuffix(registryURL, "/") + "/health"},
		{"ingestion", "http://localhost:5001/health"},
		{"storage", "http://localhost:5002/health"},
		{"analytics", "http://localhost:5003/health"},
		{"temperature", "http://localhost:5000/health"},
	}

	for {
		var sb strings.Builder
		sb.WriteString("# HELP pushed_service_health Service health status pushed via pushgateway (1 = UP, 0 = DOWN)\n")
		sb.WriteString("# TYPE pushed_service_health gauge\n")

		var latencySb strings.Builder
		latencySb.WriteString("# HELP pushed_service_latency_seconds Service response latency in seconds\n")
		latencySb.WriteString("# TYPE pushed_service_latency_seconds gauge\n")

		for _, ep := range endpoints {
			start := time.Now()
			alive := 0.0

			resp, err := httpClient.Get(ep.Endpoint)
			duration := time.Since(start).Seconds()

			if err == nil && resp.StatusCode == http.StatusOK {
				alive = 1.0
			}
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}

			sb.WriteString(fmt.Sprintf("pushed_service_health{endpoint=%q,service=%q} %.1f\n", ep.Endpoint, ep.Service, alive))
			latencySb.WriteString(fmt.Sprintf("pushed_service_latency_seconds{endpoint=%q,service=%q} %.6f\n", ep.Endpoint, ep.Service, duration))
		}

		payload := sb.String() + "\n" + latencySb.String()

		req, err := http.NewRequest(http.MethodPost, pushEndpoint, bytes.NewBufferString(payload))
		if err == nil {
			req.Header.Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			resp, err := httpClient.Do(req)
			if err != nil {
				log.Printf("[Pushgateway] Failed to push metrics to %s: %v\n", pushEndpoint, err)
			} else {
				resp.Body.Close()
				log.Printf("[Pushgateway] Successfully pushed metrics for %d services (status %d)\n",
					len(endpoints), resp.StatusCode)
			}
		}

		time.Sleep(pushInterval)
	}
}
