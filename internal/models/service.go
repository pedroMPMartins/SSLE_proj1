package models

import (
	"fmt"
	"strings"
	"time"
)

// ServiceLocation describes the physical, logical, and geographic placement of a service.
// This structure satisfies the SSLE requirements for scalable hierarchical naming:
// - Physical location: Real machine / bare-metal host (e.g., kali-vm, srv-rack-01)
// - Logical location: Container / VM / isolation namespace (e.g., incus:ssle-ingestion)
// - Geographic location: Region and Zone for administrative delegation and latency/bandwidth optimization
type ServiceLocation struct {
	Region       string `json:"region"`        // Geographic region (e.g., "pt-aveiro", "eu-west")
	Zone         string `json:"zone"`          // Availability zone / datacenter (e.g., "lab-ssle", "zone-a")
	PhysicalHost string `json:"physical_host"` // Real physical machine (e.g., "kali-vm", "server-01")
	LogicalEnv   string `json:"logical_env"`   // Logical container / VM (e.g., "incus:ssle-ingestion")
}

// Service represents a registered service instance in the SOA ecosystem.
// Canonical Hierarchical FQSN (Fully Qualified Service Name):
// <region>.<zone>.<physical_host>.<logical_env>.<instance_id>.<kind>
type Service struct {
	Name      string          `json:"name"`      // Canonical hierarchical FQSN
	Kind      string          `json:"kind"`      // Service type: ingestion, storage, analytics, temperature, registry
	ID        string          `json:"id"`        // Unique instance identifier
	URL       string          `json:"url"`       // Network endpoint URL
	Location  ServiceLocation `json:"location"`  // Structured location metadata
	Timestamp time.Time       `json:"timestamp"` // Registration timestamp
}

// SanitizeIdentifier cleans segment strings to ensure valid dot-separated FQSN format.
func SanitizeIdentifier(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "/", "-")
	return s
}

// BuildFQSN constructs a canonical hierarchical Fully Qualified Service Name.
// Format: <region>.<zone>.<physical_host>.<logical_env>.<id>.<kind>
func (s *Service) BuildFQSN() string {
	region := s.Location.Region
	if region == "" {
		region = "pt-aveiro"
	}
	zone := s.Location.Zone
	if zone == "" {
		zone = "lab-ssle"
	}
	phys := s.Location.PhysicalHost
	if phys == "" {
		phys = "kali-vm"
	}
	logic := s.Location.LogicalEnv
	if logic == "" {
		if s.Kind != "" {
			logic = "incus:ssle-" + s.Kind
		} else {
			logic = "incus:ssle-node"
		}
	}
	id := s.ID
	if id == "" {
		id = "instance"
	}
	kind := s.Kind
	if kind == "" {
		kind = "service"
	}

	return fmt.Sprintf("%s.%s.%s.%s.%s.%s",
		SanitizeIdentifier(region),
		SanitizeIdentifier(zone),
		SanitizeIdentifier(phys),
		SanitizeIdentifier(logic),
		SanitizeIdentifier(id),
		SanitizeIdentifier(kind),
	)
}

// ParseFQSN parses a canonical 6-part dot-separated FQSN into service fields.
func (s *Service) ParseFQSN(fqsn string) error {
	parts := strings.Split(strings.TrimSpace(fqsn), ".")
	if len(parts) < 6 {
		return fmt.Errorf("invalid FQSN %q: expected at least 6 dot-separated segments (<region>.<zone>.<phys>.<logic>.<id>.<kind>)", fqsn)
	}

	s.Location.Region = parts[0]
	s.Location.Zone = parts[1]
	s.Location.PhysicalHost = parts[2]
	s.Location.LogicalEnv = parts[3]
	s.ID = parts[4]
	s.Kind = parts[5]
	s.Name = fqsn
	return nil
}

// MatchesFilter checks if the service matches the provided location and kind filters.
// Empty string filters are treated as wildcards (match anything).
func (s *Service) MatchesFilter(region, zone, physicalHost, logicalEnv, kind string) bool {
	if region != "" && !strings.EqualFold(s.Location.Region, region) {
		return false
	}
	if zone != "" && !strings.EqualFold(s.Location.Zone, zone) {
		return false
	}
	if physicalHost != "" && !strings.EqualFold(s.Location.PhysicalHost, physicalHost) {
		return false
	}
	if logicalEnv != "" && !strings.EqualFold(s.Location.LogicalEnv, logicalEnv) {
		return false
	}
	if kind != "" && !strings.EqualFold(s.Kind, kind) {
		return false
	}
	return true
}

// LocateIncidentDetails generates a detailed diagnostic summary for rapid incident response.
func (s *Service) LocateIncidentDetails() map[string]any {
	return map[string]any{
		"service_id":            s.ID,
		"fqsn":                  s.Name,
		"kind":                  s.Kind,
		"url":                   s.URL,
		"registered_at":         s.Timestamp.Format(time.RFC3339),
		"physical_host":         s.Location.PhysicalHost,
		"logical_env":           s.Location.LogicalEnv,
		"zone":                  s.Location.Zone,
		"region":                s.Location.Region,
		"administrative_domain": fmt.Sprintf("%s/%s", s.Location.Region, s.Location.Zone),
		"remediation_hint":      fmt.Sprintf("To inspect container: incus exec %s -- journalctl -u ssle-%s -n 50", strings.TrimPrefix(s.Location.LogicalEnv, "incus:"), s.Kind),
	}
}
