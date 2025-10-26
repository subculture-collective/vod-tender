package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// MockTwitchServer creates a test server that mocks Twitch Helix API responses
type MockTwitchServer struct {
	*httptest.Server
	Handlers map[string]http.HandlerFunc
}

// NewMockTwitchServer creates a new mock Twitch API server
func NewMockTwitchServer(t *testing.T) *MockTwitchServer {
	t.Helper()
	m := &MockTwitchServer{
		Handlers: make(map[string]http.HandlerFunc),
	}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if handler, ok := m.Handlers[key]; ok {
			handler(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(m.Close)
	return m
}

// MockUserResponse adds a handler for /helix/users endpoint
func (m *MockTwitchServer) MockUserResponse(userID, login string) {
	m.Handlers["/helix/users"] = func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"data": []map[string]string{
				{"id": userID, "login": login},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response) //nolint:errcheck // test mock response
	}
}

// MockVideosResponse adds a handler for /helix/videos endpoint
func (m *MockTwitchServer) MockVideosResponse(videos []map[string]string, cursor string) {
	m.Handlers["/helix/videos"] = func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"data": videos,
			"pagination": map[string]string{
				"cursor": cursor,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response) //nolint:errcheck // test mock response
	}
}

// MockStreamsResponse adds a handler for /helix/streams endpoint
func (m *MockTwitchServer) MockStreamsResponse(streams []map[string]interface{}) {
	m.Handlers["/helix/streams"] = func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"data": streams,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response) //nolint:errcheck // test mock response
	}
}

// MockOAuthTokenResponse adds a handler for OAuth token endpoint
func (m *MockTwitchServer) MockOAuthTokenResponse(accessToken string, expiresIn int) {
	m.Handlers["/oauth2/token"] = func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"access_token": accessToken,
			"expires_in":   expiresIn,
			"token_type":   "bearer",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response) //nolint:errcheck // test mock response
	}
}
