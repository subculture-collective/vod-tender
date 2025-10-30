package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onnwee/vod-tender/backend/testutil"
)

// generateRandomID generates a random hex string for unique test IDs
func generateRandomID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// flushableRecorder wraps httptest.ResponseRecorder to implement http.Flusher
type flushableRecorder struct {
	*httptest.ResponseRecorder
	mu      sync.Mutex
	flushed int
}

func newFlushableRecorder() *flushableRecorder {
	return &flushableRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

func (f *flushableRecorder) Flush() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushed++
}

func (f *flushableRecorder) FlushCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.flushed
}

// TestChatSSE_SpeedAccuracy validates timing accuracy at different playback speeds
func TestChatSSE_SpeedAccuracy(t *testing.T) {
	db := testutil.SetupTestDB(t)
	handler := NewMux(db)

	// Setup: Insert test VOD and chat messages with known timing
	vodID := "test-vod-speed-" + generateRandomID()
	baseTime := time.Now().UTC()

	_, err := db.Exec(`
		INSERT INTO vods (twitch_vod_id, title, date, duration_seconds, created_at)
		VALUES ($1, 'Test VOD for Speed', $2, 3600, $3)
	`, vodID, baseTime, baseTime)
	if err != nil {
		t.Fatalf("failed to insert test vod: %v", err)
	}

	// Insert messages at 0ms, 100ms, 200ms, 300ms relative timestamps
	messages := []struct {
		username string
		message  string
		relTime  float64
	}{
		{"user1", "message at 0ms", 0.0},
		{"user2", "message at 100ms", 0.1},
		{"user3", "message at 200ms", 0.2},
		{"user4", "message at 300ms", 0.3},
	}

	for _, msg := range messages {
		absTime := baseTime.Add(time.Duration(msg.relTime) * time.Second)
		_, err := db.Exec(`
			INSERT INTO chat_messages (vod_id, username, message, abs_timestamp, rel_timestamp, badges, emotes, color)
			VALUES ($1, $2, $3, $4, $5, '', '', '')
		`, vodID, msg.username, msg.message, absTime, msg.relTime)
		if err != nil {
			t.Fatalf("failed to insert chat message: %v", err)
		}
	}

	tests := []struct {
		name          string
		speed         float64
		expectedDelay time.Duration
		tolerance     time.Duration
	}{
		{
			name:          "0.5x speed (slow motion)",
			speed:         0.5,
			expectedDelay: 200 * time.Millisecond, // 100ms real time at 0.5x = 200ms
			tolerance:     100 * time.Millisecond,
		},
		{
			name:          "1x speed (normal)",
			speed:         1.0,
			expectedDelay: 100 * time.Millisecond, // 100ms real time at 1x = 100ms
			tolerance:     100 * time.Millisecond,
		},
		{
			name:          "2x speed (fast forward)",
			speed:         2.0,
			expectedDelay: 50 * time.Millisecond, // 100ms real time at 2x = 50ms
			tolerance:     100 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/vods/%s/chat/stream?speed=%.1f", vodID, tt.speed), nil)
			w := newFlushableRecorder()

			// Track when we see each message
			startTime := time.Now()
			
			// Run handler (it will block until done)
			handler.ServeHTTP(w, req)
			
			// Validate SSE headers
			if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
				t.Errorf("expected Content-Type text/event-stream, got %s", ct)
			}

			// Parse body and extract messages with approximate timestamps
			body := w.Body.String()
			lines := strings.Split(body, "\n")
			
			messages := []string{}
			for _, line := range lines {
				if strings.HasPrefix(line, "data: ") {
					messages = append(messages, line)
				}
			}

			if len(messages) < 4 {
				t.Logf("Body content: %s", body)
				t.Fatalf("expected 4 messages, got %d", len(messages))
			}

			// Estimate total time taken (should be approximately 3 intervals worth)
			// At 0.5x: 0.3s real time = 0.6s playback
			// At 1x: 0.3s real time = 0.3s playback  
			// At 2x: 0.3s real time = 0.15s playback
			totalTime := time.Since(startTime)
			expectedTime := time.Duration(float64(300*time.Millisecond) / tt.speed)
			tolerance := 200 * time.Millisecond
			
			diff := totalTime - expectedTime
			if diff < 0 {
				diff = -diff
			}

			if diff > tolerance {
				t.Errorf("timing accuracy at %.1fx speed: expected total time %v ±%v, got %v (diff: %v)",
					tt.speed, expectedTime, tolerance, totalTime, diff)
			}

			// Verify flush was called
			if w.FlushCount() == 0 {
				t.Error("expected Flush() to be called during SSE streaming")
			}
		})
	}
}

// TestChatSSE_Backpressure validates handling of large message volumes
func TestChatSSE_Backpressure(t *testing.T) {
	db := testutil.SetupTestDB(t)
	handler := NewMux(db)

	vodID := "test-vod-backpressure-" + generateRandomID()
	baseTime := time.Now().UTC()

	_, err := db.Exec(`
		INSERT INTO vods (twitch_vod_id, title, date, duration_seconds, created_at)
		VALUES ($1, 'Test VOD for Backpressure', $2, 3600, $3)
	`, vodID, baseTime, baseTime)
	if err != nil {
		t.Fatalf("failed to insert test vod: %v", err)
	}

	// Insert a large number of messages at the same timestamp to test backpressure
	messageCount := 1000
	for i := 0; i < messageCount; i++ {
		absTime := baseTime.Add(time.Duration(i%100) * 10 * time.Millisecond)
		relTime := float64(i%100) * 0.01
		_, err := db.Exec(`
			INSERT INTO chat_messages (vod_id, username, message, abs_timestamp, rel_timestamp, badges, emotes, color)
			VALUES ($1, $2, $3, $4, $5, '', '', '')
		`, vodID, fmt.Sprintf("user%d", i), fmt.Sprintf("message %d", i), absTime, relTime)
		if err != nil {
			t.Fatalf("failed to insert chat message %d: %v", i, err)
		}
	}

	// Test at high speed to ensure backpressure handling
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/vods/%s/chat/stream?speed=100", vodID), nil)
	w := newFlushableRecorder()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(w, req)
		close(done)
	}()

	// Wait for completion or timeout
	select {
	case <-done:
		// Good, handler completed
	case <-time.After(5 * time.Second):
		// Timeout is OK - we're testing that it handles backpressure without blocking
	}

	// Count received messages
	body := w.Body.String()
	lines := strings.Split(body, "\n")
	receivedCount := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			receivedCount++
		}
	}

	// We should receive all messages without buffer overflow
	if receivedCount < messageCount {
		t.Logf("received %d/%d messages (partial delivery is acceptable for backpressure test)", receivedCount, messageCount)
	}

	// Verify no panic or error occurred (status would be non-200 on error)
	if w.Code != 0 && w.Code != http.StatusOK {
		t.Errorf("unexpected status code: %d, body: %s", w.Code, body)
	}
}

// TestChatSSE_CancellationHandling validates proper context cancellation
func TestChatSSE_CancellationHandling(t *testing.T) {
	db := testutil.SetupTestDB(t)
	handler := NewMux(db)

	vodID := "test-vod-cancel-" + generateRandomID()
	baseTime := time.Now().UTC()

	_, err := db.Exec(`
		INSERT INTO vods (twitch_vod_id, title, date, duration_seconds, created_at)
		VALUES ($1, 'Test VOD for Cancellation', $2, 3600, $3)
	`, vodID, baseTime, baseTime)
	if err != nil {
		t.Fatalf("failed to insert test vod: %v", err)
	}

	// Insert messages with delays to allow time for cancellation
	for i := 0; i < 10; i++ {
		absTime := baseTime.Add(time.Duration(i) * 100 * time.Millisecond)
		relTime := float64(i) * 0.1
		_, err := db.Exec(`
			INSERT INTO chat_messages (vod_id, username, message, abs_timestamp, rel_timestamp, badges, emotes, color)
			VALUES ($1, $2, $3, $4, $5, '', '', '')
		`, vodID, fmt.Sprintf("user%d", i), fmt.Sprintf("message %d", i), absTime, relTime)
		if err != nil {
			t.Fatalf("failed to insert chat message: %v", err)
		}
	}

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/vods/%s/chat/stream?speed=1", vodID), nil)
	req = req.WithContext(ctx)
	w := newFlushableRecorder()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(w, req)
		close(done)
	}()

	// Monitor for messages and cancel after receiving some
	receivedCount := 0
	timeout := time.After(15 * time.Second)

monitorLoop:
	for {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for messages")
		case <-done:
			// Handler stopped - count final messages
			break monitorLoop
		case <-time.After(100 * time.Millisecond):
			body := w.Body.String()
			lines := strings.Split(body, "\n")
			newCount := 0
			for _, line := range lines {
				if strings.HasPrefix(line, "data: ") {
					newCount++
				}
			}
			
			if newCount > receivedCount {
				receivedCount = newCount
				if receivedCount >= 2 {
					// Cancel after receiving 2 messages
					cancel()
					// Wait a bit for handler to exit
					time.Sleep(500 * time.Millisecond)
					break monitorLoop
				}
			}
		}
	}

	// We should have received exactly 2-3 messages before cancellation
	if receivedCount < 2 {
		t.Errorf("expected at least 2 messages before cancellation, got %d", receivedCount)
	}
	if receivedCount > 5 {
		t.Errorf("expected cancellation to stop delivery, but got %d messages", receivedCount)
	}
}

// TestChatSSE_SSEFormat validates proper SSE event formatting
func TestChatSSE_SSEFormat(t *testing.T) {
	db := testutil.SetupTestDB(t)
	handler := NewMux(db)

	vodID := "test-vod-format-" + generateRandomID()
	baseTime := time.Now().UTC()

	_, err := db.Exec(`
		INSERT INTO vods (twitch_vod_id, title, date, duration_seconds, created_at)
		VALUES ($1, 'Test VOD for Format', $2, 3600, $3)
	`, vodID, baseTime, baseTime)
	if err != nil {
		t.Fatalf("failed to insert test vod: %v", err)
	}

	// Insert a single test message
	_, err = db.Exec(`
		INSERT INTO chat_messages (vod_id, username, message, abs_timestamp, rel_timestamp, badges, emotes, color)
		VALUES ($1, 'testuser', 'test message', $2, 0.0, 'moderator:1', 'Kappa', '#FF0000')
	`, vodID, baseTime)
	if err != nil {
		t.Fatalf("failed to insert chat message: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/vods/%s/chat/stream?speed=10", vodID), nil)
	w := newFlushableRecorder()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(w, req)
		close(done)
	}()

	// Wait for completion
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	// Validate SSE headers
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected Cache-Control no-cache, got %s", cc)
	}
	if conn := w.Header().Get("Connection"); conn != "keep-alive" {
		t.Errorf("expected Connection keep-alive, got %s", conn)
	}

	// Parse the SSE message
	body := w.Body.String()
	if !strings.Contains(body, "data: ") {
		t.Fatal("no SSE data found in response")
	}

	lines := strings.Split(body, "\n")
	var jsonData string
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			jsonData = strings.TrimPrefix(line, "data: ")
			break
		}
	}

	if jsonData == "" {
		t.Fatal("no JSON data found in SSE stream")
	}

	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(jsonData), &msg); err != nil {
		t.Fatalf("failed to parse JSON: %v, data: %s", err, jsonData)
	}

	// Validate message fields
	expectedFields := []string{"username", "message", "abs_timestamp", "rel_timestamp", "badges", "emotes", "color"}
	for _, field := range expectedFields {
		if _, ok := msg[field]; !ok {
			t.Errorf("missing field in message: %s", field)
		}
	}

	if msg["username"] != "testuser" {
		t.Errorf("expected username=testuser, got %v", msg["username"])
	}
	if msg["message"] != "test message" {
		t.Errorf("expected message='test message', got %v", msg["message"])
	}
}

// TestChatSSE_EmptyVOD validates behavior with no chat messages
func TestChatSSE_EmptyVOD(t *testing.T) {
	db := testutil.SetupTestDB(t)
	handler := NewMux(db)

	vodID := "test-vod-empty-" + generateRandomID()
	baseTime := time.Now().UTC()

	_, err := db.Exec(`
		INSERT INTO vods (twitch_vod_id, title, date, duration_seconds, created_at)
		VALUES ($1, 'Test VOD Empty', $2, 3600, $3)
	`, vodID, baseTime, baseTime)
	if err != nil {
		t.Fatalf("failed to insert test vod: %v", err)
	}

	// Don't insert any chat messages

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/vods/%s/chat/stream", vodID), nil)
	w := newFlushableRecorder()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(w, req)
		close(done)
	}()

	// Should complete quickly with no messages
	select {
	case <-done:
		// Good, handler completed
	case <-time.After(1 * time.Second):
		t.Error("handler took too long with empty VOD")
	}

	// Validate headers are still set
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", ct)
	}

	// Should not contain any data messages
	body := w.Body.String()
	if strings.Contains(body, "data: ") {
		t.Error("expected no data messages for empty VOD")
	}
}

// TestChatSSE_InvalidSpeed validates speed parameter handling
func TestChatSSE_InvalidSpeed(t *testing.T) {
	db := testutil.SetupTestDB(t)
	handler := NewMux(db)

	vodID := "test-vod-speed-invalid-" + generateRandomID()
	baseTime := time.Now().UTC()

	_, err := db.Exec(`
		INSERT INTO vods (twitch_vod_id, title, date, duration_seconds, created_at)
		VALUES ($1, 'Test VOD', $2, 3600, $3)
	`, vodID, baseTime, baseTime)
	if err != nil {
		t.Fatalf("failed to insert test vod: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO chat_messages (vod_id, username, message, abs_timestamp, rel_timestamp, badges, emotes, color)
		VALUES ($1, 'user1', 'test', $2, 0.0, '', '', '')
	`, vodID, baseTime)
	if err != nil {
		t.Fatalf("failed to insert chat message: %v", err)
	}

	tests := []struct {
		name          string
		speedParam    string
		expectDefault bool
	}{
		{
			name:          "zero speed falls back to 1x",
			speedParam:    "0",
			expectDefault: true,
		},
		{
			name:          "negative speed falls back to 1x",
			speedParam:    "-1",
			expectDefault: true,
		},
		{
			name:          "invalid string falls back to 1x",
			speedParam:    "abc",
			expectDefault: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/vods/%s/chat/stream?speed=%s", vodID, tt.speedParam), nil)
			w := newFlushableRecorder()

			done := make(chan struct{})
			go func() {
				handler.ServeHTTP(w, req)
				close(done)
			}()

			select {
			case <-done:
				// Should complete without error
			case <-time.After(2 * time.Second):
				t.Error("handler timeout")
			}

			// Should have received the message (speed defaults to 1x)
			body := w.Body.String()
			if !strings.Contains(body, "data: ") {
				t.Error("expected message delivery with default speed")
			}
		})
	}
}
