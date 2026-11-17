package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"simple_soa/internal/function"
	"simple_soa/internal/models"
	"strings"
	"sync"
	"time"
)

var (
	registry   = make(map[string]models.Service)
	mu         sync.RWMutex
	adminToken = os.Getenv("ADMIN_TOKEN") // Optional token to protect DELETE /register/{id}
	port       = os.Getenv("PORT")
)

func main() {
	if port == "" {
		port = "5050"
	}

	mux := http.NewServeMux()

	// GET /health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		count := len(registry)
		regions := make(map[string]int)
		zones := make(map[string]int)
		for _, s := range registry {
			if s.Location.Region != "" {
				regions[s.Location.Region]++
			}
			if s.Location.Zone != "" {
				zones[s.Location.Zone]++
			}
		}
		mu.RUnlock()

		function.RespondJSON(w, http.StatusOK, map[string]any{
			"status":              "UP",
			"service":             "registry",
			"registered_services": count,
			"regions":             regions,
			"zones":               zones,
		})
	})

	// POST /register
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		var req models.Service

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.URL == "" || req.Kind == "" {
			function.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing id, kind, or url"})
			return
		}

		if req.Timestamp.IsZero() {
			req.Timestamp = time.Now().UTC()
		}

		// Ensure location defaults if missing
		if req.Location.Region == "" || req.Location.Zone == "" {
			defaultLoc := function.BuildDefaultLocation(req.Kind)
			if req.Location.Region == "" {
				req.Location.Region = defaultLoc.Region
			}
			if req.Location.Zone == "" {
				req.Location.Zone = defaultLoc.Zone
			}
			if req.Location.PhysicalHost == "" {
				req.Location.PhysicalHost = defaultLoc.PhysicalHost
			}
			if req.Location.LogicalEnv == "" {
				req.Location.LogicalEnv = defaultLoc.LogicalEnv
			}
		}

		// Build canonical hierarchical FQSN if not specified
		if req.Name == "" {
			req.Name = req.BuildFQSN()
		}

		mu.Lock()
		registry[req.ID] = req
		mu.Unlock()

		log.Printf("[Registry] Registered service '%s' (FQSN: %s) at %s [Region: %s, Zone: %s, Host: %s, Env: %s]\n",
			req.ID, req.Name, req.URL, req.Location.Region, req.Location.Zone, req.Location.PhysicalHost, req.Location.LogicalEnv)

		function.RespondJSON(w, http.StatusOK, map[string]any{
			"message":  req.ID + " registered",
			"id":       req.ID,
			"fqsn":     req.Name,
			"location": req.Location,
		})
	})

	// DELETE /register/{id}
	mux.HandleFunc("DELETE /register/{id}", func(w http.ResponseWriter, r *http.Request) {
		// Defense mechanism: if ADMIN_TOKEN is set, enforce token authentication
		if adminToken != "" {
			reqToken := r.Header.Get("X-Admin-Token")
			if reqToken == "" {
				authHeader := r.Header.Get("Authorization")
				reqToken = strings.TrimPrefix(authHeader, "Bearer ")
			}
			if reqToken != adminToken {
				log.Printf("[Security Alert] Unauthorized unregister attempt on '%s' from %s\n", r.PathValue("id"), r.RemoteAddr)
				function.RespondJSON(w, http.StatusForbidden, map[string]string{"error": "Unauthorized: admin token required to unregister"})
				return
			}
		}

		target := r.PathValue("id")

		mu.Lock()
		defer mu.Unlock()

		// Allow deletion by ID or FQSN
		var foundKey string
		if _, exists := registry[target]; exists {
			foundKey = target
		} else {
			for k, v := range registry {
				if v.Name == target {
					foundKey = k
					break
				}
			}
		}

		if foundKey != "" {
			deleted := registry[foundKey]
			delete(registry, foundKey)
			log.Printf("[Registry] Unregistered service '%s' (FQSN: %s)\n", deleted.ID, deleted.Name)
			function.RespondJSON(w, http.StatusOK, map[string]string{
				"message": foundKey + " unregistered",
				"fqsn":    deleted.Name,
			})
			return
		}

		function.RespondJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	})

	// GET /services/locate/{query}
	// Rapid Incident Localization endpoint (FCAPS & Project Guideline Mandate):
	// Instantly resolves an incident source (by ID, FQSN, or endpoint URL) to its exact physical machine,
	// logical container, zone, region, and remediation target.
	mux.HandleFunc("GET /services/locate/{query}", func(w http.ResponseWriter, r *http.Request) {
		query := r.PathValue("query")
		if query == "" {
			function.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing query parameter"})
			return
		}

		mu.RLock()
		defer mu.RUnlock()

		// 1. Direct match by ID
		if s, ok := registry[query]; ok {
			function.RespondJSON(w, http.StatusOK, map[string]any{
				"status":   "FOUND",
				"query":    query,
				"incident": s.LocateIncidentDetails(),
			})
			return
		}

		// 2. Match by Kind, FQSN (exact or substring), or URL
		for _, s := range registry {
			if strings.EqualFold(s.Kind, query) || strings.EqualFold(s.Name, query) || strings.Contains(strings.ToLower(s.Name), strings.ToLower(query)) || strings.Contains(s.URL, query) {
				function.RespondJSON(w, http.StatusOK, map[string]any{
					"status":   "FOUND",
					"query":    query,
					"incident": s.LocateIncidentDetails(),
				})
				return
			}
		}

		function.RespondJSON(w, http.StatusNotFound, map[string]any{
			"status": "NOT_FOUND",
			"query":  query,
			"error":  "Service or incident source not found in registry",
		})
	})

	// GET /services/{kind}
	// Supports location query parameters: ?region=...&zone=...&physical_host=...&logical_env=...
	mux.HandleFunc("GET /services/{kind}", func(w http.ResponseWriter, r *http.Request) {
		kind := r.PathValue("kind")
		q := r.URL.Query()
		region := q.Get("region")
		zone := q.Get("zone")
		phys := q.Get("physical_host")
		logic := q.Get("logical_env")

		mu.RLock()
		defer mu.RUnlock()
		var servicesToReturn []models.Service
		for _, serv := range registry {
			if serv.MatchesFilter(region, zone, phys, logic, kind) {
				servicesToReturn = append(servicesToReturn, serv)
			}
		}

		function.RespondJSON(w, http.StatusOK, servicesToReturn)
	})

	// GET /services
	// Supports filtering by query parameters: ?region=...&zone=...&physical_host=...&logical_env=...&kind=...&prefix=...
	mux.HandleFunc("GET /services", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		region := q.Get("region")
		zone := q.Get("zone")
		phys := q.Get("physical_host")
		logic := q.Get("logical_env")
		kind := q.Get("kind")
		prefix := q.Get("prefix")

		mu.RLock()
		defer mu.RUnlock()

		hasFilter := region != "" || zone != "" || phys != "" || logic != "" || kind != "" || prefix != ""

		if !hasFilter {
			// Backwards compatibility: return the full map
			function.RespondJSON(w, http.StatusOK, registry)
			return
		}

		filteredMap := make(map[string]models.Service)
		for id, s := range registry {
			if s.MatchesFilter(region, zone, phys, logic, kind) {
				if prefix != "" && !strings.HasPrefix(s.Name, prefix) {
					continue
				}
				filteredMap[id] = s
			}
		}

		function.RespondJSON(w, http.StatusOK, filteredMap)
	})

	// Wrap mux with generic DoS protection (token-bucket rate limiting and payload clamping)
	dosCfg := function.LoadDoSConfigFromEnv()
	protectedHandler := function.DoSProtectionMiddleware(mux, dosCfg)

	// Wrap with structured access logging for Wazuh SIEM
	accessLogger := function.NewAccessLogger("registry", "")
	defer accessLogger.Close()
	loggingHandler := function.AccessLoggingMiddleware(accessLogger, protectedHandler)

	log.Printf("Hierarchical Service Registry listening on :%s (DoS Defense: RateLimit=%v [RPS=%.1f], MaxBodyBytes=%d)\n",
		port, dosCfg.EnableRateLimit, dosCfg.RateLimitRPS, dosCfg.MaxBodyBytes)

	server := function.NewHardenedServer(":"+port, loggingHandler)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Registry server failed: %v", err)
	}
}
