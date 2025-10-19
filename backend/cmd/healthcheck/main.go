package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	client := &http.Client{Timeout: 3 * time.Second}
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:8080/healthz", nil)
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
