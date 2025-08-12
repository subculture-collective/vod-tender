package vod

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/onnwee/vod-tender/backend/twitchapi"
)

// Core VOD model (DB schema lives in migrations elsewhere)
type VOD struct {
	ID       string
	Title    string
	Date     time.Time
	Duration int
}

// download cancellation registry
var (
	activeMu      = &sync.Mutex{}
	activeCancels = map[string]context.CancelFunc{}
)

func CancelDownload(id string) bool {
	activeMu.Lock(); defer activeMu.Unlock()
	if c, ok := activeCancels[id]; ok { c(); delete(activeCancels, id); return true }
	return false
}

// FetchChannelVODs lists recent archive VODs using Twitch Helix (simple unpaged variant).
// (Historical / paged listing lives in catalog.go)
func FetchChannelVODs(ctx context.Context) ([]VOD, error) {
	channel := os.Getenv("TWITCH_CHANNEL")
	if channel == "" { return nil, nil }
	hc := &twitchapi.HelixClient{AppTokenSource: &twitchapi.TokenSource{ClientID: os.Getenv("TWITCH_CLIENT_ID"), ClientSecret: os.Getenv("TWITCH_CLIENT_SECRET")}, ClientID: os.Getenv("TWITCH_CLIENT_ID")}
	uid, err := hc.GetUserID(ctx, channel)
	if err != nil { return nil, err }
	videos, _, err := hc.ListVideos(ctx, uid, "", 20)
	if err != nil { return nil, err }
	out := make([]VOD, 0, len(videos))
	for _, v := range videos {
		created, _ := time.Parse(time.RFC3339, v.CreatedAt)
		out = append(out, VOD{ID: v.ID, Title: v.Title, Date: created, Duration: parseTwitchDuration(v.Duration)})
	}
	return out, nil
}

// DiscoverAndUpsert inserts newly discovered VODs (idempotent via INSERT OR IGNORE)
func DiscoverAndUpsert(ctx context.Context, db *sql.DB) error {
	vods, err := FetchChannelVODs(ctx)
	if err != nil { return err }
	for _, v := range vods {
		_, _ = db.Exec(`INSERT OR IGNORE INTO vods (twitch_vod_id, title, date, duration_seconds, created_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`, v.ID, v.Title, v.Date, v.Duration)
	}
	return nil
}
// (historical catalog logic moved to catalog.go)

// (catalog backfill + duration parsing moved to catalog.go)

// downloadVOD uses yt-dlp to download a Twitch VOD by id.
func downloadVOD(ctx context.Context, db *sql.DB, id, dataDir string) (string, error) {
	// Stable output path so yt-dlp can resume (.part file) across restarts
	out := filepath.Join(dataDir, fmt.Sprintf("twitch_%s.mp4", id))
	url := "https://www.twitch.tv/videos/" + id

	// yt-dlp resume-friendly flags
	args := []string{
		"--continue",                 // resume partial downloads
		"--retries", "infinite",     // retry network errors
		"--fragment-retries", "infinite", // retry fragment errors (HLS)
		"--concurrent-fragments", "10",   // speed up HLS by parallel fragments
		"-f", "best",               // best available format
		"-o", out,                   // output path
		url,
	}

	// If aria2c available, prefer it for robustness on direct HTTP downloads
	if _, err := exec.LookPath("aria2c"); err == nil {
		// Reasonable defaults; yt-dlp will ignore for HLS fragment downloads
		args = append([]string{"--external-downloader", "aria2c", "--downloader-args", "aria2c:-x16 -s16 -k1M --file-allocation=none"}, args...)
	}

	// Retry loop with exponential backoff + jitter and configurable attempts
	maxAttempts := 5
	if s := os.Getenv("DOWNLOAD_MAX_ATTEMPTS"); s != "" { if n, err := strconv.Atoi(s); err == nil && n > 0 { maxAttempts = n } }
	baseBackoff := 2 * time.Second
	if s := os.Getenv("DOWNLOAD_BACKOFF_BASE"); s != "" { if d, err := time.ParseDuration(s); err == nil && d > 0 { baseBackoff = d } }
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			backoff := baseBackoff * time.Duration(1<<attempt)
			jitter := time.Duration(rand.Int63n(int64(baseBackoff))) // up to baseBackoff extra
			backoff += jitter
			slog.Warn("retrying download", slog.String("vod_id", id), slog.Int("attempt", attempt), slog.Duration("backoff", backoff))
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoff):
			}
		}

		// Create a child context we can cancel via API
		dlCtx, cancel := context.WithCancel(ctx)
		activeMu.Lock()
		activeCancels[id] = cancel
		activeMu.Unlock()

		cmd := exec.CommandContext(dlCtx, "yt-dlp", args...)
		// Capture progress from stderr lines e.g.: "[download]   4.3% of ~2.19GiB at  3.05MiB/s ETA 11:22"
		stderr, _ := cmd.StderrPipe()
		cmd.Stdout = os.Stdout
		if err := cmd.Start(); err != nil {
			lastErr = err
			activeMu.Lock()
			delete(activeCancels, id)
			activeMu.Unlock()
			continue
		}
		progressRe := regexp.MustCompile(`(?i)\[download\]\s+([0-9.]+)%.*?of\s+~?([0-9.]+)([KMG]iB).*?at\s+([0-9.]+)([KMG]iB)/s.*?ETA\s+([0-9:]+)`) // best-effort
		totalBytes := int64(0)
		var lastPercent float64
		bytesRe := regexp.MustCompile(`(?i)([0-9.]+)([KMG]iB)`) // for size groups
		decUnit := func(val string, unit string) int64 {
			// Convert to bytes
			f := 0.0
			for _, c := range val {
				if c == '.' { continue }
			}
			fmt.Sscanf(val, "%f", &f)
			mult := float64(1)
			switch strings.ToUpper(unit) {
			case "KIB": mult = 1024
			case "MIB": mult = 1024 * 1024
			case "GIB": mult = 1024 * 1024 * 1024
			}
			return int64(f * mult)
		}
		// Reader loop
		go func() {
			buf := make([]byte, 16*1024)
			var line strings.Builder
			for {
				n, err := stderr.Read(buf)
				if n > 0 {
					chunk := string(buf[:n])
					for _, r := range chunk {
						if r == '\n' || r == '\r' {
							s := line.String()
							line.Reset()
							if m := progressRe.FindStringSubmatch(s); len(m) > 0 {
								// m[1]=percent, m[2]=size, m[3]=unit
								if totalBytes == 0 {
									if mm := bytesRe.FindStringSubmatch(m[2]+m[3]); len(mm) == 3 {
										totalBytes = decUnit(mm[1], mm[2])
									}
								}
								if p, err := strconv.ParseFloat(m[1], 64); err == nil {
									lastPercent = p
								}
								// approximate current bytes
								curBytes := int64(0)
								if totalBytes > 0 && lastPercent > 0 {
									curBytes = int64((lastPercent / 100.0) * float64(totalBytes))
								}
								// Update DB with approximate progress
								_, _ = db.Exec(`UPDATE vods SET download_state=?, download_total=?, download_bytes=?, progress_updated_at=CURRENT_TIMESTAMP WHERE twitch_vod_id=?`, s, totalBytes, curBytes, id)
							}
						} else {
							line.WriteRune(r)
						}
					}
				}
				if err != nil {
					break
				}
			}
		}()
		err := cmd.Wait()
		activeMu.Lock()
		delete(activeCancels, id)
		activeMu.Unlock()
		if err == nil {
			// Finalize progress to 100%
			_, _ = db.Exec(`UPDATE vods SET download_state=?, download_total=?, download_bytes=?, downloaded_path=?, progress_updated_at=CURRENT_TIMESTAMP WHERE twitch_vod_id=?`, "complete", totalBytes, totalBytes, out, id)
			return out, nil
		}
		// Classify error from stderr state we captured last; fallback to err.Error()
		lastErr = fmt.Errorf("yt-dlp: %w", err)
		// Increment retry counter
		_, _ = db.Exec(`UPDATE vods SET download_retries = COALESCE(download_retries,0) + 1, progress_updated_at=CURRENT_TIMESTAMP WHERE twitch_vod_id=?`, id)
		// If context canceled, stop immediately
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	return "", lastErr
}

// uploadToYouTube uploads the given video file using stored OAuth token.
// (moved uploadToYouTube implementation to processing.go)

// Circuit breaker helpers
func updateCircuitOnFailure(ctx context.Context, db *sql.DB) {
	threshold := 0
	if s := os.Getenv("CIRCUIT_FAILURE_THRESHOLD"); s != "" { if n, err := strconv.Atoi(s); err == nil { threshold = n } }
	if threshold <= 0 { return }
	var fails int
	row := db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_failures'`)
	var val string
	_ = row.Scan(&val)
	if val != "" { _ = func() error { n, e := strconv.Atoi(val); if e==nil { fails = n }; return nil }() }
	fails++
	_, _ = db.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ('circuit_failures',?,CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`, fmt.Sprintf("%d", fails))
	if fails >= threshold {
		// open circuit
		cool := 5 * time.Minute
		if s := os.Getenv("CIRCUIT_OPEN_COOLDOWN"); s != "" { if d, err := time.ParseDuration(s); err == nil { cool = d } }
		until := time.Now().Add(cool).UTC().Format(time.RFC3339)
		_, _ = db.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ('circuit_state','open',CURRENT_TIMESTAMP)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`)
		_, _ = db.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ('circuit_open_until',?,CURRENT_TIMESTAMP)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`, until)
		slog.Warn("circuit opened", slog.Int("failures", fails), slog.String("until", until))
	}
}

func resetCircuit(ctx context.Context, db *sql.DB) {
	// success path: if half-open or open we close; reset failures
	var state string
	_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_state'`).Scan(&state)
	if state == "closed" && os.Getenv("CIRCUIT_FAILURE_THRESHOLD") == "" { return }
	_, _ = db.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ('circuit_failures','0',CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`)
	_, _ = db.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ('circuit_state','closed',CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`)
	_, _ = db.ExecContext(ctx, `DELETE FROM kv WHERE key IN ('circuit_open_until')`)
}
