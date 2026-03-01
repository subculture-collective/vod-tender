package vod

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	ytDLPStructuredProgressPrefix = "VT_PROGRESS|"
	ytDLPProgressTemplate         = "download:VT_PROGRESS|%(progress._percent_str)s|%(progress.total_bytes)s|%(progress.total_bytes_estimate)s|%(progress.downloaded_bytes)s|%(progress.speed)s|%(progress.eta)s"
)

type parsedDownloadProgress struct {
	State           string
	Percent         *float64
	TotalBytes      int64
	DownloadedBytes int64
}

var (
	legacyDownloadProgressRe = regexp.MustCompile(`(?i)\[download\]\s+([0-9]+(?:\.[0-9]+)?)%.*?of\s+~?([0-9]+(?:\.[0-9]+)?\s*[KMGT]?i?B)`)
	byteQuantityRe           = regexp.MustCompile(`(?i)^\s*([0-9]+(?:\.[0-9]+)?)\s*([KMGT]?I?B|[KMGT]?B|B)\s*$`)
)

func parseDownloadProgressLine(line string) (parsedDownloadProgress, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return parsedDownloadProgress{}, false
	}

	if idx := strings.Index(trimmed, ytDLPStructuredProgressPrefix); idx >= 0 {
		payload := trimmed[idx+len(ytDLPStructuredProgressPrefix):]
		parts := strings.Split(payload, "|")
		if len(parts) >= 6 {
			percent := parsePercentString(parts[0])
			totalBytes, _ := parseOptionalInt64(parts[1])
			estimatedTotalBytes, _ := parseOptionalInt64(parts[2])
			downloadedBytes, _ := parseOptionalInt64(parts[3])
			if totalBytes <= 0 {
				totalBytes = estimatedTotalBytes
			}
			if downloadedBytes <= 0 && percent != nil && totalBytes > 0 {
				downloadedBytes = int64(((*percent) / 100.0) * float64(totalBytes))
			}
			if percent != nil || totalBytes > 0 || downloadedBytes > 0 {
				return parsedDownloadProgress{
					State:           formatProgressState(percent, totalBytes, downloadedBytes, trimmed),
					Percent:         percent,
					TotalBytes:      totalBytes,
					DownloadedBytes: downloadedBytes,
				}, true
			}
		}
	}

	if !strings.Contains(strings.ToLower(trimmed), "[download]") {
		return parsedDownloadProgress{}, false
	}

	percent := extractPercentFromText(trimmed)
	var totalBytes int64

	if m := legacyDownloadProgressRe.FindStringSubmatch(trimmed); len(m) == 3 {
		if percent == nil {
			percent = parsePercentString(m[1] + "%")
		}
		if parsedTotal, ok := parseByteQuantity(m[2]); ok {
			totalBytes = parsedTotal
		}
	}

	downloadedBytes := int64(0)
	if percent != nil && totalBytes > 0 {
		downloadedBytes = int64(((*percent) / 100.0) * float64(totalBytes))
	}

	if percent != nil || totalBytes > 0 || downloadedBytes > 0 {
		return parsedDownloadProgress{
			State:           trimmed,
			Percent:         percent,
			TotalBytes:      totalBytes,
			DownloadedBytes: downloadedBytes,
		}, true
	}

	return parsedDownloadProgress{}, false
}

func parseByteQuantity(raw string) (int64, bool) {
	m := byteQuantityRe.FindStringSubmatch(strings.TrimSpace(raw))
	if len(m) != 3 {
		return 0, false
	}

	value, err := strconv.ParseFloat(m[1], 64)
	if err != nil || value < 0 {
		return 0, false
	}

	unit := strings.ToUpper(strings.TrimSpace(m[2]))
	multiplier := float64(1)
	switch unit {
	case "B":
		multiplier = 1
	case "KB":
		multiplier = 1000
	case "MB":
		multiplier = 1000 * 1000
	case "GB":
		multiplier = 1000 * 1000 * 1000
	case "TB":
		multiplier = 1000 * 1000 * 1000 * 1000
	case "KIB":
		multiplier = 1024
	case "MIB":
		multiplier = 1024 * 1024
	case "GIB":
		multiplier = 1024 * 1024 * 1024
	case "TIB":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, false
	}

	bytes := int64(math.Round(value * multiplier))
	if bytes < 0 {
		return 0, false
	}
	return bytes, true
}

func parsePercentString(raw string) *float64 {
	s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), "%"))
	if s == "" || strings.EqualFold(s, "na") || strings.EqualFold(s, "n/a") {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

func parseOptionalInt64(raw string) (int64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || strings.EqualFold(s, "na") || strings.EqualFold(s, "n/a") {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

func extractPercentFromText(state string) *float64 {
	i := strings.Index(state, "%")
	if i <= 0 {
		return nil
	}
	j := i - 1
	for j >= 0 && (state[j] == '.' || (state[j] >= '0' && state[j] <= '9')) {
		j--
	}
	if j+1 >= i {
		return nil
	}
	num := state[j+1 : i]
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return nil
	}
	return &v
}

func formatProgressState(percent *float64, totalBytes, downloadedBytes int64, fallback string) string {
	if percent != nil && totalBytes > 0 {
		return fmt.Sprintf("[download] %.1f%% of %dB", *percent, totalBytes)
	}
	if percent != nil {
		return fmt.Sprintf("[download] %.1f%%", *percent)
	}
	if downloadedBytes > 0 && totalBytes > 0 {
		return fmt.Sprintf("[download] %dB / %dB", downloadedBytes, totalBytes)
	}
	return strings.TrimSpace(fallback)
}
