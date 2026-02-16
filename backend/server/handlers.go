// Package server exposes the HTTP API handlers.
package server

import (
	"context"
	"database/sql"
	"sync"
	"time"
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
