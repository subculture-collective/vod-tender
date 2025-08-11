package vod

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	youtube "google.golang.org/api/youtube/v3"
)

type VOD struct {
	ID        string
	Title     string
	Date      time.Time
	Duration  int
}

var (
	activeMu      = &sync.Mutex{}
	activeCancels = map[string]context.CancelFunc{}
)

// CancelDownload cancels an in-flight download for a given VOD id if present.
func CancelDownload(id string) bool {
	activeMu.Lock()
	defer activeMu.Unlock()
	if c, ok := activeCancels[id]; ok {
		c()
		delete(activeCancels, id)
		return true
	}
	return false
}

// StartVODProcessingJob sequentially discovers, downloads, and uploads VODs.
func StartVODProcessingJob(ctx context.Context, db *sql.DB) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		if err := processOnce(ctx, db); err != nil {
			slog.Error("vod process cycle error", slog.Any("err", err))
		}
		select {
		case <-ctx.Done():
			slog.Info("vod processing job stopped")
			return
		case <-ticker.C:
		}
	}
}

func processOnce(ctx context.Context, db *sql.DB) error {
	// Ensure data dir exists
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("mkdir data dir: %w", err)
	}

	// Discover new VODs and upsert into DB
	vods, err := fetchChannelVODs(ctx)
	if err != nil {
		return err
	}
	for _, v := range vods {
		_, _ = db.Exec(`INSERT OR IGNORE INTO vods (twitch_vod_id, title, date, duration_seconds, created_at)
						VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`, v.ID, v.Title, v.Date, v.Duration)
	}

	// Pick one unprocessed VOD
	row := db.QueryRow(`SELECT twitch_vod_id, title, date FROM vods WHERE processed = 0 ORDER BY date ASC LIMIT 1`)
	var id, title string
	var date time.Time
	if err := row.Scan(&id, &title, &date); err != nil {
		if err == sql.ErrNoRows {
			slog.Info("no unprocessed vods")
			return nil
		}
		return err
	}

	// Download
	filePath, err := downloadVOD(ctx, db, id, dataDir)
	if err != nil {
		slog.Error("download failed", slog.String("vod_id", id), slog.Any("err", err))
		_, _ = db.Exec(`UPDATE vods SET processing_error=?, updated_at=CURRENT_TIMESTAMP WHERE twitch_vod_id=?`, err.Error(), id)
		return nil
	}
	_, _ = db.Exec(`UPDATE vods SET downloaded_path=?, updated_at=CURRENT_TIMESTAMP WHERE twitch_vod_id=?`, filePath, id)

	// Upload
	ytURL, err := uploadToYouTube(ctx, filePath, title, date)
	if err != nil {
		slog.Error("upload failed", slog.String("vod_id", id), slog.Any("err", err))
		_, _ = db.Exec(`UPDATE vods SET processing_error=?, updated_at=CURRENT_TIMESTAMP WHERE twitch_vod_id=?`, err.Error(), id)
		return nil
	}

	// Mark processed
	_, _ = db.Exec(`UPDATE vods SET youtube_url=?, processed=1, updated_at=CURRENT_TIMESTAMP WHERE twitch_vod_id=?`, ytURL, id)
	slog.Info("processed vod", slog.String("vod_id", id), slog.String("youtube_url", ytURL))
	return nil
}

// fetchChannelVODs uses Twitch Helix to list recent VODs for the channel from env TWITCH_CHANNEL.
func fetchChannelVODs(ctx context.Context) ([]VOD, error) {
	clientID := os.Getenv("TWITCH_CLIENT_ID")
	clientSecret := os.Getenv("TWITCH_CLIENT_SECRET")
	channel := os.Getenv("TWITCH_CHANNEL")
	if clientID == "" || clientSecret == "" || channel == "" {
		// Not configured; no discovery
		return nil, nil
	}
	// Get app access token
	tokReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://id.twitch.tv/oauth2/token", nil)
	q := tokReq.URL.Query()
	q.Set("client_id", clientID)
	q.Set("client_secret", clientSecret)
	q.Set("grant_type", "client_credentials")
	tokReq.URL.RawQuery = q.Encode()
	tokResp, err := http.DefaultClient.Do(tokReq)
	if err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	defer tokResp.Body.Close()
	var tok struct{ AccessToken string `json:"access_token"` }
	if err := json.NewDecoder(tokResp.Body).Decode(&tok); err != nil {
		return nil, fmt.Errorf("decode token: %w", err)
	}

	// Get channel ID
	usersReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.twitch.tv/helix/users", nil)
	uq := usersReq.URL.Query()
	uq.Set("login", channel)
	usersReq.URL.RawQuery = uq.Encode()
	usersReq.Header.Set("Client-Id", clientID)
	usersReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	usersResp, err := http.DefaultClient.Do(usersReq)
	if err != nil {
		return nil, fmt.Errorf("users: %w", err)
	}
	defer usersResp.Body.Close()
	var users struct{ Data []struct{ ID string } `json:"data"` }
	if err := json.NewDecoder(usersResp.Body).Decode(&users); err != nil || len(users.Data) == 0 {
		return nil, fmt.Errorf("users decode: %w", err)
	}
	userID := users.Data[0].ID

	// Get videos (VODs)
	vidsReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.twitch.tv/helix/videos", nil)
	vq := vidsReq.URL.Query()
	vq.Set("user_id", userID)
	vq.Set("type", "archive")
	vq.Set("first", "20")
	vidsReq.URL.RawQuery = vq.Encode()
	vidsReq.Header.Set("Client-Id", clientID)
	vidsReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	vidsResp, err := http.DefaultClient.Do(vidsReq)
	if err != nil {
		return nil, fmt.Errorf("videos: %w", err)
	}
	defer vidsResp.Body.Close()
	var vids struct {
		Data []struct {
			ID        string    `json:"id"`
			Title     string    `json:"title"`
			CreatedAt time.Time `json:"created_at"`
			Duration  string    `json:"duration"`
		} `json:"data"`
	}
	if err := json.NewDecoder(vidsResp.Body).Decode(&vids); err != nil {
		return nil, fmt.Errorf("videos decode: %w", err)
	}
	var out []VOD
	for _, v := range vids.Data {
		out = append(out, VOD{
			ID:       v.ID,
			Title:    v.Title,
			Date:     v.CreatedAt,
			Duration: parseTwitchDuration(v.Duration),
		})
	}
	return out, nil
}

func parseTwitchDuration(s string) int {
	// Twitch duration format like "3h15m42s"
	var total int
	cur := ""
	for _, r := range s {
		if r >= '0' && r <= '9' {
			cur += string(r)
			continue
		}
		if cur == "" {
			continue
		}
		n := 0
		for _, d := range cur {
			n = n*10 + int(d-'0')
		}
		switch r {
		case 'h':
			total += n * 3600
		case 'm':
			total += n * 60
		case 's':
			total += n
		}
		cur = ""
	}
	return total
}

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

	// Retry loop with classification
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<attempt) * time.Second
			slog.Warn("retrying download", slog.String("vod_id", id), slog.Any("attempt", attempt), slog.Any("backoff", backoff))
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

// uploadToYouTube is a placeholder that simulates upload and returns a fake URL.
// Replace with YouTube Data API upload.
func uploadToYouTube(ctx context.Context, path, title string, date time.Time) (string, error) {
	// Construct title and description
	datePrefix := date.Format("2006-01-02")
	finalTitle := strings.TrimSpace(fmt.Sprintf("%s %s", datePrefix, title))
	description := fmt.Sprintf("Uploaded from Twitch VOD on %s", date.Format(time.RFC3339))

	// OAuth2: expects GOOGLE_CLIENT_ID/SECRET and a saved token in YT_TOKEN_JSON or file YT_TOKEN_FILE.
	// Prefer application default credentials if available.
	var httpClient *http.Client

	// 1) Try ADC first (e.g., GCP, local gcloud auth)
	if adc, err := google.FindDefaultCredentials(ctx, youtube.YoutubeUploadScope); err == nil {
		httpClient = oauth2.NewClient(ctx, adc.TokenSource)
	}

	// 2) Try explicit client secret JSON from env var YT_CREDENTIALS_JSON or file YT_CREDENTIALS_FILE
	if httpClient == nil {
		var credsData []byte
		if p := os.Getenv("YT_CREDENTIALS_FILE"); p != "" {
			b, err := os.ReadFile(p)
			if err == nil {
				credsData = b
			}
		}
		if len(credsData) == 0 {
			if s := os.Getenv("YT_CREDENTIALS_JSON"); s != "" {
				credsData = []byte(s)
			}
		}
		if len(credsData) > 0 {
			cfg, err := google.ConfigFromJSON(credsData, youtube.YoutubeUploadScope)
			if err != nil {
				return "", fmt.Errorf("parse youtube creds: %w", err)
			}
			// Token source from file or env
			var tok *oauth2.Token
			if p := os.Getenv("YT_TOKEN_FILE"); p != "" {
				if b, err := os.ReadFile(p); err == nil {
					if err := json.Unmarshal(b, &tok); err != nil {
						return "", fmt.Errorf("token json: %w", err)
					}
				}
			}
			if tok == nil {
				if s := os.Getenv("YT_TOKEN_JSON"); s != "" {
					if err := json.Unmarshal([]byte(s), &tok); err != nil {
						return "", fmt.Errorf("token env json: %w", err)
					}
				}
			}
			if tok == nil {
				return "", fmt.Errorf("missing YT_TOKEN_JSON or YT_TOKEN_FILE for YouTube OAuth token")
			}
			httpClient = cfg.Client(ctx, tok)
		}
	}

	if httpClient == nil {
		return "", fmt.Errorf("no YouTube credentials available; set ADC or YT_CREDENTIALS_JSON and YT_TOKEN_JSON")
	}

	svc, err := youtube.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return "", fmt.Errorf("youtube service: %w", err)
	}

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	snippet := &youtube.VideoSnippet{
		Title:       finalTitle,
		Description: description,
	}
	status := &youtube.VideoStatus{PrivacyStatus: "private"}
	vid := &youtube.Video{Snippet: snippet, Status: status}

	call := svc.Videos.Insert([]string{"snippet", "status"}, vid)
	call = call.Media(f)
	res, err := call.Do()
	if err != nil {
		return "", fmt.Errorf("youtube upload: %w", err)
	}
	if res.Id == "" {
		return "", fmt.Errorf("youtube upload: empty video id")
	}
	return "https://www.youtube.com/watch?v=" + res.Id, nil
}
