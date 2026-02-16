package server

import (
	"net/http"
	"os"
	"strconv"
	"strings"
)

// parseFloat64Query extracts a float64 parameter from query string with a default value.
func parseFloat64Query(r *http.Request, key string, def float64) float64 {
	if v := r.URL.Query().Get(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

// parseIntQuery extracts an int parameter from query string with a default value.
func parseIntQuery(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

// derivePercent extracts a float percent from yt-dlp progress string, if present.
func derivePercent(state string) *float64 {
	// example: "[download]   4.3% of ~2.19GiB at  3.05MiB/s ETA 11:22"
	i := strings.Index(state, "%")
	if i <= 0 {
		return nil
	}
	// walk backwards to find the number start
	j := i - 1
	for j >= 0 && (state[j] == '.' || (state[j] >= '0' && state[j] <= '9')) {
		j--
	}
	if j+1 >= i {
		return nil
	}
	num := state[j+1 : i]
	if f, err := strconv.ParseFloat(num, 64); err == nil {
		return &f
	}
	return nil
}

// getEnvInt returns an integer environment variable value or default if not set or invalid.
func getEnvInt(key string, defaultVal int) int {
	if s := os.Getenv(key); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return defaultVal
}
