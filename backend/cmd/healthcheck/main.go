package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	// Use readyz for Docker healthchecks - more comprehensive check
	// (DB connectivity, circuit breaker state, credentials presence)
	endpoint := os.Getenv("HEALTHCHECK_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8080/readyz"
	}

	client := &http.Client{Timeout: 3 * time.Second}
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		os.Exit(1)
	}
	resp, err := client.Do(req)
	if err != nil {
		os.Exit(1)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("failed to close response body: %v", err)
		}
	}()
	if resp.StatusCode != 200 {
		os.Exit(1)
	}
}
