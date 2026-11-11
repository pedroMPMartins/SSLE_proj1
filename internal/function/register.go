package function

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

func RegisterWithRetry(id string, kind string, url string, registryUrl string) {
	if !strings.HasSuffix(registryUrl, "/register") {
		registryUrl = strings.TrimSuffix(registryUrl, "/") + "/register"
	}

	payload := map[string]string{
		"id":   id,
		"kind": kind,
		"url":  url,
	}
	jsonBody, _ := json.Marshal(payload)

	// Simple retry loop in case the registry isn't up yet
	for {
		resp, err := http.Post(registryUrl, "application/json", bytes.NewBuffer(jsonBody))
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				log.Println("Successfully registered with service registry.")
				return
			}
			resp.Body.Close()
		}
		log.Println("Registry not available yet, retrying in 500ms...")
		time.Sleep(500 * time.Millisecond)
	}
}
