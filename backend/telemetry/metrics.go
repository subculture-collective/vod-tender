// Package telemetry provides Prometheus metrics and correlation-id aware logging helpers.
package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    once sync.Once

    // Counters
    DownloadsStarted  prometheus.Counter
    DownloadsFailed   prometheus.Counter
    DownloadsSucceeded prometheus.Counter
    UploadsSucceeded  prometheus.Counter
    UploadsFailed     prometheus.Counter
    ProcessingCycles  prometheus.Counter

    // Histograms (seconds)
    DownloadDuration prometheus.Observer
    UploadDuration   prometheus.Observer
    TotalProcessDuration prometheus.Observer

    // Gauges
    QueueDepthGauge prometheus.Gauge
    CircuitOpenGauge prometheus.Gauge // 1=open,0=closed
)

// Init registers metrics (idempotent).
func Init() {
    once.Do(func() {
        DownloadsStarted = promauto.NewCounter(prometheus.CounterOpts{Name: "vod_downloads_started_total", Help: "Number of VOD downloads started"})
        DownloadsFailed = promauto.NewCounter(prometheus.CounterOpts{Name: "vod_downloads_failed_total", Help: "Number of VOD downloads failed"})
        DownloadsSucceeded = promauto.NewCounter(prometheus.CounterOpts{Name: "vod_downloads_succeeded_total", Help: "Number of VOD downloads succeeded"})
    UploadsSucceeded = promauto.NewCounter(prometheus.CounterOpts{Name: "vod_uploads_succeeded_total", Help: "Number of VOD uploads succeeded"})
    UploadsFailed = promauto.NewCounter(prometheus.CounterOpts{Name: "vod_uploads_failed_total", Help: "Number of VOD uploads failed"})
        ProcessingCycles = promauto.NewCounter(prometheus.CounterOpts{Name: "vod_processing_cycles_total", Help: "Number of processing cycles (processOnce invocations)"})
        DownloadDuration = promauto.NewHistogram(prometheus.HistogramOpts{Name: "vod_download_duration_seconds", Help: "Download duration seconds", Buckets: prometheus.DefBuckets})
        UploadDuration = promauto.NewHistogram(prometheus.HistogramOpts{Name: "vod_upload_duration_seconds", Help: "Upload duration seconds", Buckets: prometheus.DefBuckets})
        TotalProcessDuration = promauto.NewHistogram(prometheus.HistogramOpts{Name: "vod_processing_total_duration_seconds", Help: "Total processing cycle duration seconds", Buckets: prometheus.DefBuckets})
        QueueDepthGauge = promauto.NewGauge(prometheus.GaugeOpts{Name: "vod_queue_depth", Help: "Current number of unprocessed VODs"})
        CircuitOpenGauge = promauto.NewGauge(prometheus.GaugeOpts{Name: "vod_circuit_open", Help: "Circuit breaker open=1 closed=0"})
    })
}

// UpdateCircuitGauge sets gauge to 1 if open else 0.
func UpdateCircuitGauge(open bool) { if CircuitOpenGauge != nil { if open { CircuitOpenGauge.Set(1) } else { CircuitOpenGauge.Set(0) } } }

// SetQueueDepth records current unprocessed VOD count.
func SetQueueDepth(n int) { if QueueDepthGauge != nil { QueueDepthGauge.Set(float64(n)) } }

// TimeFunc measures the duration of fn and records in observer if non-nil.
func TimeFunc(obs prometheus.Observer, fn func()) time.Duration {
    start := time.Now()
    fn()
    d := time.Since(start)
    if obs != nil { obs.Observe(d.Seconds()) }
    return d
}

// Correlation ID helpers ----------------------------------------------------
type corrKeyType struct{}
var corrKey corrKeyType

// WithCorrelation returns a new context embedding correlation id (if absent) and the id.
func WithCorrelation(ctx context.Context, id string) context.Context { return context.WithValue(ctx, corrKey, id) }

// GetCorrelation returns correlation id or empty string.
func GetCorrelation(ctx context.Context) string {
    v := ctx.Value(corrKey)
    if s, ok := v.(string); ok { return s }
    return ""
}

// LoggerWithCorr returns a logger with corr attribute if present.
func LoggerWithCorr(ctx context.Context) *slog.Logger {
    if id := GetCorrelation(ctx); id != "" { return slog.Default().With(slog.String("corr", id)) }
    return slog.Default()
}
