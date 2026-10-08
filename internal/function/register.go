package function

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

func RegisterWithRetry(id string, kind string, url string, registryUrl string) {
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
			resp.Body.Close()
			log.Println("Successfully registered with service registry.")
			return
		}
		log.Println("Registry not available yet, retrying in 2 seconds...")
		time.Sleep(2 * time.Second)
	}
}
