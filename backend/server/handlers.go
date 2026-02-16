// Package server exposes the HTTP API handlers.
package server

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

const (
	// Maximum number of OAuth states to keep in memory
	maxOAuthStates = 10000
)

// Handlers holds dependencies for all HTTP handlers.
type Handlers struct {
	db         *sql.DB
	ctx        context.Context
	stateStore map[string]time.Time
	stateMu    sync.RWMutex
}

// NewHandlers creates a new Handlers instance with the given dependencies.
func NewHandlers(ctx context.Context, db *sql.DB) *Handlers {
	return &Handlers{
		db:         db,
		ctx:        ctx,
		stateStore: make(map[string]time.Time),
	}
}

// cleanExpiredStates removes expired OAuth states from the store.
// This should be called with stateMu locked.
func (h *Handlers) cleanExpiredStates() {
	now := time.Now()
	for state, expiry := range h.stateStore {
		if now.After(expiry) {
			delete(h.stateStore, state)
		}
	}
}

// addOAuthState adds a new OAuth state to the store with cleanup if needed.
func (h *Handlers) addOAuthState(state string, expiry time.Time) {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()

	// Clean expired states periodically to prevent unbounded growth
	if len(h.stateStore)%100 == 0 {
		h.cleanExpiredStates()
	}

	// If we're still over the limit after cleanup, refuse to add more
	if len(h.stateStore) >= maxOAuthStates {
		// Don't add the state - this will cause the OAuth flow to fail
		// which is better than a memory exhaustion attack
		return
	}

	h.stateStore[state] = expiry
}
