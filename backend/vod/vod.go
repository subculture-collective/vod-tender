// Package vod implements the VOD model, discovery helpers, downloader integration (yt-dlp),
// and auxiliary utilities like a cancellation registry and circuit breaker helpers.
package vod

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
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

	"github.com/onnwee/vod-tender/backend/telemetry"
	"github.com/onnwee/vod-tender/backend/twitchapi"
)

// VOD is the core model (DB schema defined in db.migrate). It mirrors a subset of Twitch video metadata.
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
		_, _ = db.Exec(`INSERT INTO vods (twitch_vod_id, title, date, duration_seconds, created_at) VALUES ($1,$2,$3,$4,NOW()) ON CONFLICT (twitch_vod_id) DO NOTHING`, v.ID, v.Title, v.Date, v.Duration)
	}
	return nil
}
// (historical catalog logic moved to catalog.go)

// (catalog backfill + duration parsing moved to catalog.go)

// downloadVOD uses yt-dlp to download a Twitch VOD by id.
// Security: when cookies are provided via YTDLP_COOKIES_PATH this function copies
// the file to a private temp path (0600) and passes --cookies, avoiding echoing
// sensitive headers to logs. Avoid enabling verbose yt-dlp logs when secrets are used.
func downloadVOD(ctx context.Context, db *sql.DB, id, dataDir string) (string, error) {
	// Stable output path so yt-dlp can resume (.part file) across restarts
	out := filepath.Join(dataDir, fmt.Sprintf("twitch_%s.mp4", id))
	url := "https://www.twitch.tv/videos/" + id
	logger := slog.Default().With(slog.String("vod_id", id), slog.String("component", "vod_download"))
	if corr := ctx.Value(struct{ string }{"corr"}); corr != nil { logger = logger.With(slog.Any("corr", corr)) }
	logger.Info("download start", slog.String("out", out))
	telemetry.DownloadsStarted.Inc()

	// Resolve yt-dlp path (runtime image installs to /usr/local/bin)
	ytDLP := "yt-dlp"
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		ytDLP = p
	} else if _, err2 := os.Stat("/usr/local/bin/yt-dlp"); err2 == nil {
		ytDLP = "/usr/local/bin/yt-dlp"
	}
	logger.Debug("downloader selected", slog.String("yt_dlp", ytDLP))

	// yt-dlp flags tuned for resilient HLS; let yt-dlp auto-pick best formats (avoid deprecated -f best warning)
	args := []string{
		"--continue",                      // resume partial downloads
		"--retries", "infinite",          // retry network errors
		"--fragment-retries", "infinite", // retry fragment errors (HLS)
		"--concurrent-fragments", "10",   // speed up HLS by parallel fragments
		"--no-cache-dir",                  // avoid writing caches to disk
		"-o", out,                         // output path
		url,
	}

	// Optional Twitch auth via cookies file. Copy to a temp file to avoid writing back to a read-only mount.
	hasSecrets := false
	var tmpCookiesPath string
	// Allow default path when env var is not set
	cf := strings.TrimSpace(os.Getenv("YTDLP_COOKIES_PATH"))
	if cf == "" {
		// Common default mount path in our compose
		if _, err := os.Stat("/run/cookies/twitch-cookies.txt"); err == nil {
			cf = "/run/cookies/twitch-cookies.txt"
		}
	}
	if cf != "" {
		// Create a private temp copy
		f, err := os.Open(cf)
		if err == nil {
			defer f.Close()
			tf, terr := os.CreateTemp("", fmt.Sprintf("yt_cookies_%s_*.txt", id))
			if terr == nil {
				tmpCookiesPath = tf.Name()
				_, _ = io.Copy(tf, f)
				_ = tf.Chmod(0o600)
				_ = tf.Close()
			}
		}
		if tmpCookiesPath == "" {
			// Fallback to using source path directly
			tmpCookiesPath = cf
		}
	logger.Debug("using cookies file for yt-dlp", slog.String("cookies_path", cf))
	args = append([]string{"--cookies", tmpCookiesPath}, args...)
		hasSecrets = true
	}
	if extra := os.Getenv("YTDLP_ARGS"); strings.TrimSpace(extra) != "" {
		args = append(strings.Fields(extra), args...)
	}
	// Avoid -v when credentials are present to prevent yt-dlp echoing secrets in logs
	if !hasSecrets && (strings.EqualFold(os.Getenv("LOG_LEVEL"), "DEBUG") || os.Getenv("YTDLP_VERBOSE") == "1") {
		args = append([]string{"-v"}, args...)
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
		logger.Debug("download attempt", slog.Int("attempt", attempt+1), slog.Int("max", maxAttempts))
		if attempt > 0 {
			backoff := baseBackoff * time.Duration(1<<attempt)
			jitter := time.Duration(rand.Int63n(int64(baseBackoff))) // up to baseBackoff extra
			backoff += jitter
			logger.Warn("retrying download", slog.Int("attempt", attempt), slog.Duration("backoff", backoff))
			time.Sleep(backoff)
			activeMu.Lock()
			delete(activeCancels, id)
			activeMu.Unlock()
		}
		cmd := exec.CommandContext(ctx, ytDLP, args...)
		stderr, errPipe := cmd.StderrPipe()
		if errPipe != nil { lastErr = errPipe; break }
		if err := cmd.Start(); err != nil { lastErr = err; break }
		activeMu.Lock(); activeCancels[id] = func(){ _ = cmd.Process.Kill() }; activeMu.Unlock()
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
		// Reader loop; also capture tail of stderr for diagnostics (with secret scrubbing)
		const maxTail = 100
		lastLines := make([]string, 0, maxTail)
		sanitize := func(s string) string {
			// Redact explicit Cookie headers and auth-token occurrences if any
			if i := strings.Index(s, "Cookie:"); i >= 0 {
				// keep prefix, redact rest of header value
				return s[:i+len("Cookie:")] + " [redacted]"
			}
			if strings.Contains(strings.ToLower(s), "auth-token=") {
				return regexp.MustCompile(`auth-token=[^;\s]+`).ReplaceAllString(s, "auth-token=[redacted]")
			}
			return s
		}
		appendLine := func(s string) {
			if s == "" { return }
			s = sanitize(s)
			if len(lastLines) >= maxTail { lastLines = lastLines[1:] }
			lastLines = append(lastLines, s)
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
								_, _ = db.Exec(`UPDATE vods SET download_state=$1, download_total=$2, download_bytes=$3, progress_updated_at=NOW() WHERE twitch_vod_id=$4`, s, totalBytes, curBytes, id)
							} else if strings.TrimSpace(s) != "" {
								appendLine(strings.TrimSpace(s))
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
		if tmpCookiesPath != "" && !strings.EqualFold(tmpCookiesPath, os.Getenv("YTDLP_COOKIES_PATH")) {
			_ = os.Remove(tmpCookiesPath)
		}
		if err == nil {
			// Finalize progress to 100%; determine actual file size if available
			actual := totalBytes
			if fi, statErr := os.Stat(out); statErr == nil { actual = fi.Size() }
			_, _ = db.Exec(`UPDATE vods SET download_state=$1, download_total=$2, download_bytes=$3, downloaded_path=$4, progress_updated_at=NOW() WHERE twitch_vod_id=$5`, "complete", actual, actual, out, id)
			logger.Info("download finished", slog.Int64("bytes", actual))
			telemetry.DownloadsSucceeded.Inc()
			return out, nil
		}
		// Classify error from stderr state we captured last; fallback to err.Error()
		detail := strings.Join(lastLines, "\n")
		lower := strings.ToLower(detail)
		if strings.Contains(lower, "subscriber-only") || strings.Contains(lower, "only available to subscribers") || strings.Contains(lower, "403") {
			logger.Warn("twitch indicates auth requirement; consider YTDLP_COOKIES_PATH")
		}
		lastErr = fmt.Errorf("yt-dlp: %w\nlast output:\n%s", err, detail)
		// Increment retry counter
		_, _ = db.Exec(`UPDATE vods SET download_retries = COALESCE(download_retries,0) + 1, progress_updated_at=NOW() WHERE twitch_vod_id=$1`, id)
		// If context canceled, stop immediately
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	telemetry.DownloadsFailed.Inc()
	logger.Error("download exhausted retries", slog.Any("err", lastErr))
	return "", lastErr
}

// buildCookieHeaderFromNetscape constructs a Cookie header from a Netscape cookies file.
func buildCookieHeaderFromNetscape(path string, domainSuffix string) (string, error) {
	f, err := os.Open(path)
	if err != nil { return "", err }
	defer f.Close()
	sc := bufio.NewScanner(f)
	pairs := make([]string, 0, 16)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") { continue }
		parts := strings.Split(line, "\t")
		if len(parts) < 7 { parts = strings.Fields(line) }
		if len(parts) < 7 { continue }
		dom, name, val := parts[0], parts[5], parts[6]
		if !strings.HasSuffix(strings.ToLower(dom), strings.ToLower(domainSuffix)) { continue }
		if strings.EqualFold(name, "#httponly_"+name) { name = strings.TrimPrefix(name, "#HttpOnly_") }
		if name == "" { continue }
		pairs = append(pairs, fmt.Sprintf("%s=%s", name, val))
	}
	if err := sc.Err(); err != nil { return "", err }
	return strings.Join(pairs, "; "), nil
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
	_, _ = db.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ('circuit_failures',$1,NOW())
		ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`, fmt.Sprintf("%d", fails))
	if fails >= threshold {
		// open circuit
		cool := 5 * time.Minute
		if s := os.Getenv("CIRCUIT_OPEN_COOLDOWN"); s != "" { if d, err := time.ParseDuration(s); err == nil { cool = d } }
		until := time.Now().Add(cool).UTC().Format(time.RFC3339)
		_, _ = db.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ('circuit_state','open',NOW())
			ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`)
		_, _ = db.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ('circuit_open_until',$1,NOW())
			ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`, until)
		slog.Warn("circuit opened", slog.Int("failures", fails), slog.String("until", until))
		telemetry.UpdateCircuitGauge(true)
	}
}

func resetCircuit(ctx context.Context, db *sql.DB) {
	// success path: if half-open or open we close; reset failures
	var state string
	_ = db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='circuit_state'`).Scan(&state)
	if state == "closed" && os.Getenv("CIRCUIT_FAILURE_THRESHOLD") == "" { return }
	_, _ = db.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ('circuit_failures','0',NOW())
		ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`)
	_, _ = db.ExecContext(ctx, `INSERT INTO kv (key,value,updated_at) VALUES ('circuit_state','closed',NOW())
		ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value, updated_at=NOW()`)
	_, _ = db.ExecContext(ctx, `DELETE FROM kv WHERE key IN ('circuit_open_until')`)
	telemetry.UpdateCircuitGauge(false)
}
