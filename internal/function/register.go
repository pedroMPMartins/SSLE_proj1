package function

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"simple_soa/internal/models"
	"strings"
	"time"
)

// Helper to get environment variable with fallback.
func getEnvWithFallback(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// BuildDefaultLocation extracts structured location from system environment variables or fallbacks.
func BuildDefaultLocation(kind string) models.ServiceLocation {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "kali-vm"
	}

	return models.ServiceLocation{
		Region:       getEnvWithFallback("SERVICE_REGION", "pt-aveiro"),
		Zone:         getEnvWithFallback("SERVICE_ZONE", "lab-ssle"),
		PhysicalHost: getEnvWithFallback("PHYSICAL_HOST", hostname),
		LogicalEnv:   getEnvWithFallback("LOGICAL_ENV", "incus:ssle-"+kind),
	}
}

// RegisterServiceWithRetry registers a fully populated models.Service with retry logic.
func RegisterServiceWithRetry(svc models.Service, registryUrl string) {
	if !strings.HasSuffix(registryUrl, "/register") {
		registryUrl = strings.TrimSuffix(registryUrl, "/") + "/register"
	}

	// Auto-fill timestamps and location if omitted
	if svc.Timestamp.IsZero() {
		svc.Timestamp = time.Now().UTC()
	}
	if svc.Location.Region == "" || svc.Location.Zone == "" {
		defaultLoc := BuildDefaultLocation(svc.Kind)
		if svc.Location.Region == "" {
			svc.Location.Region = defaultLoc.Region
		}
		if svc.Location.Zone == "" {
			svc.Location.Zone = defaultLoc.Zone
		}
		if svc.Location.PhysicalHost == "" {
			svc.Location.PhysicalHost = defaultLoc.PhysicalHost
		}
		if svc.Location.LogicalEnv == "" {
			svc.Location.LogicalEnv = defaultLoc.LogicalEnv
		}
	}
	if svc.Name == "" {
		svc.Name = svc.BuildFQSN()
	}

	jsonBody, err := json.Marshal(svc)
	if err != nil {
		log.Printf("[Register] Failed to serialize service model: %v\n", err)
		return
	}

	// Retry loop in case the registry isn't up yet
	for {
		resp, err := http.Post(registryUrl, "application/json", bytes.NewBuffer(jsonBody))
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				log.Printf("Successfully registered service '%s' (FQSN: %s) with registry.\n", svc.ID, svc.Name)
				return
			}
			resp.Body.Close()
		}
		log.Println("Registry not available yet, retrying in 500ms...")
		time.Sleep(500 * time.Millisecond)
	}
}

// RegisterWithRetry maintains backward compatibility for callers passing individual strings,
// automatically enriching with hierarchical location metadata from environment.
func RegisterWithRetry(id string, kind string, url string, registryUrl string) {
	svc := models.Service{
		ID:        id,
		Kind:      kind,
		URL:       url,
		Location:  BuildDefaultLocation(kind),
		Timestamp: time.Now().UTC(),
	}
	if customName := os.Getenv("SERVICE_NAME"); customName != "" {
		svc.Name = customName
	} else {
		svc.Name = svc.BuildFQSN()
	}

	RegisterServiceWithRetry(svc, registryUrl)
}
