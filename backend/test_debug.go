package server

import (
"fmt"
"io"
"net/http"
"net/http/httptest"
"testing"
"time"

"github.com/onnwee/vod-tender/backend/testutil"
)

func TestDebugSSE(t *testing.T) {
db := testutil.SetupTestDB(t)
handler := NewMux(db)

vodID := "test-debug-123"
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

server := httptest.NewServer(handler)
defer server.Close()

resp, err := http.Get(server.URL + fmt.Sprintf("/vods/%s/chat/stream", vodID))
if err != nil {
t.Fatalf("failed to make request: %v", err)
}
defer resp.Body.Close()

body, _ := io.ReadAll(resp.Body)
t.Logf("Status: %d", resp.StatusCode)
t.Logf("Content-Type: %s", resp.Header.Get("Content-Type"))
t.Logf("Body: %s", string(body))
}
