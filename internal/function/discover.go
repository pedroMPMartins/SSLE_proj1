package function

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"simple_soa/internal/models"
	"strings"
	"time"
)

// DiscoverServices queries the registry for all services of a given kind.
func DiscoverServices(registryURL, kind string) ([]models.Service, error) {
	return DiscoverServicesWithFilter(registryURL, kind, models.ServiceLocation{})
}

// DiscoverServicesWithFilter queries the registry with optional location criteria.
func DiscoverServicesWithFilter(registryURL, kind string, filter models.ServiceLocation) ([]models.Service, error) {
	baseURL := strings.TrimSuffix(registryURL, "/register")
	baseURL = strings.TrimSuffix(baseURL, "/")

	params := url.Values{}
	if filter.Region != "" {
		params.Set("region", filter.Region)
	}
	if filter.Zone != "" {
		params.Set("zone", filter.Zone)
	}
	if filter.PhysicalHost != "" {
		params.Set("physical_host", filter.PhysicalHost)
	}
	if filter.LogicalEnv != "" {
		params.Set("logical_env", filter.LogicalEnv)
	}

	targetURL := baseURL + "/services/" + kind
	if len(params) > 0 {
		targetURL += "?" + params.Encode()
	}

	resp, err := http.Get(targetURL)
	if err != nil {
		return nil, fmt.Errorf("failed to query registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	var services []models.Service
	if err := json.NewDecoder(resp.Body).Decode(&services); err != nil {
		return nil, fmt.Errorf("failed to decode services list: %w", err)
	}

	return services, nil
}

// DiscoverOneService queries the registry and selects one instance (random load balancing).
func DiscoverOneService(registryURL, kind string) (*models.Service, error) {
	services, err := DiscoverServices(registryURL, kind)
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("no active service of kind %q found", kind)
	}

	return &services[rand.Intn(len(services))], nil
}

// DiscoverLocally implements locality-aware discovery for bandwidth optimization:
// 1. Tries matching the exact local Zone and Region first (zero cross-zone latency/cost).
// 2. If unavailable, widens search to the same Region (regional delegation).
// 3. If unavailable, falls back to any available global service.
func DiscoverLocally(registryURL, kind string, preferred models.ServiceLocation) (*models.Service, error) {
	// Step 1: Try Zone + Region match (Local DC / cluster)
	if preferred.Zone != "" && preferred.Region != "" {
		zoneFilter := models.ServiceLocation{
			Region: preferred.Region,
			Zone:   preferred.Zone,
		}
		if services, err := DiscoverServicesWithFilter(registryURL, kind, zoneFilter); err == nil && len(services) > 0 {
			return &services[rand.Intn(len(services))], nil
		}
	}

	// Step 2: Try Region match (Local geographic region)
	if preferred.Region != "" {
		regionFilter := models.ServiceLocation{
			Region: preferred.Region,
		}
		if services, err := DiscoverServicesWithFilter(registryURL, kind, regionFilter); err == nil && len(services) > 0 {
			return &services[rand.Intn(len(services))], nil
		}
	}

	// Step 3: Global fallback
	return DiscoverOneService(registryURL, kind)
}

// DiscoverWithRetry periodically queries the registry until a service is found.
func DiscoverWithRetry(registryURL, kind string, retryInterval time.Duration) *models.Service {
	for {
		service, err := DiscoverOneService(registryURL, kind)
		if err == nil {
			log.Printf("Discovered %s service at %s (FQSN: %s)\n", kind, service.URL, service.Name)
			return service
		}
		log.Printf("Waiting for %s service to be registered (%v), retrying in %v...\n", kind, err, retryInterval)
		time.Sleep(retryInterval)
	}
}

// DiscoverLocallyWithRetry periodically queries the registry preferring local locality.
func DiscoverLocallyWithRetry(registryURL, kind string, preferred models.ServiceLocation, retryInterval time.Duration) *models.Service {
	for {
		service, err := DiscoverLocally(registryURL, kind, preferred)
		if err == nil {
			log.Printf("Discovered %s service locally at %s (FQSN: %s, Region: %s, Zone: %s)\n",
				kind, service.URL, service.Name, service.Location.Region, service.Location.Zone)
			return service
		}
		log.Printf("Waiting for %s service in [%s/%s] (%v), retrying in %v...\n",
			kind, preferred.Region, preferred.Zone, err, retryInterval)
		time.Sleep(retryInterval)
	}
}
