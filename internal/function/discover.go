package function

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"simple_soa/internal/models"
	"strings"
	"time"
)

// DiscoverServices queries the registry for all services of a given kind.
func DiscoverServices(registryURL, kind string) ([]models.Service, error) {
	baseURL := strings.TrimSuffix(registryURL, "/register")
	baseURL = strings.TrimSuffix(baseURL, "/")

	resp, err := http.Get(baseURL + "/services/" + kind)
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

// DiscoverWithRetry periodically queries the registry until a service of the specified kind is found.
func DiscoverWithRetry(registryURL, kind string, retryInterval time.Duration) *models.Service {
	for {
		service, err := DiscoverOneService(registryURL, kind)
		if err == nil {
			log.Printf("Discovered %s service at %s\n", kind, service.URL)
			return service
		}
		log.Printf("Waiting for %s service to be registered (%v), retrying in %v...\n", kind, err, retryInterval)
		time.Sleep(retryInterval)
	}
}
