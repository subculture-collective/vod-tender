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
	DownloadsStarted   prometheus.Counter
	DownloadsFailed    prometheus.Counter
	DownloadsSucceeded prometheus.Counter
	UploadsSucceeded   prometheus.Counter
	UploadsFailed      prometheus.Counter
	ProcessingCycles   prometheus.Counter

	// Histograms (seconds)
	DownloadDuration     prometheus.Observer
	UploadDuration       prometheus.Observer
	TotalProcessDuration prometheus.Observer

	// Gauges
	QueueDepthGauge     prometheus.Gauge
	CircuitOpenGauge    prometheus.Gauge // 1=open,0=closed (DEPRECATED: use CircuitStateGauge)
	CircuitStateGauge   prometheus.Gauge // 0=closed, 1=half-open, 2=open
	CircuitFailureCount prometheus.Counter

	// Enhanced metrics
	ChatMessagesRecorded        *prometheus.CounterVec
	ChatReconnections           prometheus.Counter
	OAuthTokenRefresh           *prometheus.CounterVec
	HelixAPICalls               *prometheus.CounterVec
	CircuitBreakerStateChanges  *prometheus.CounterVec
	ProcessingStepDuration      *prometheus.HistogramVec
	DatabaseConnectionPoolSize  prometheus.Gauge
	DatabaseConnectionPoolInUse prometheus.Gauge
)

// Init registers metrics (idempotent).
func Init() {
	once.Do(func() {
		// Existing metrics
		DownloadsStarted = promauto.NewCounter(prometheus.CounterOpts{Name: "vod_downloads_started_total", Help: "Number of VOD downloads started"})
		DownloadsFailed = promauto.NewCounter(prometheus.CounterOpts{Name: "vod_downloads_failed_total", Help: "Number of VOD downloads failed"})
		DownloadsSucceeded = promauto.NewCounter(prometheus.CounterOpts{Name: "vod_downloads_succeeded_total", Help: "Number of VOD downloads succeeded"})
		UploadsSucceeded = promauto.NewCounter(prometheus.CounterOpts{Name: "vod_uploads_succeeded_total", Help: "Number of VOD uploads succeeded"})
		UploadsFailed = promauto.NewCounter(prometheus.CounterOpts{Name: "vod_uploads_failed_total", Help: "Number of VOD uploads failed"})
		ProcessingCycles = promauto.NewCounter(prometheus.CounterOpts{Name: "vod_processing_cycles_total", Help: "Number of processing cycles (processOnce invocations)"})
		
		// Tuned histogram buckets for realistic VOD durations (1m to 2h)
		DownloadDuration = promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "vod_download_duration_seconds",
			Help:    "Download duration seconds",
			Buckets: []float64{60, 300, 600, 1800, 3600, 7200}, // 1m, 5m, 10m, 30m, 1h, 2h
		})
		UploadDuration = promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "vod_upload_duration_seconds",
			Help:    "Upload duration seconds",
			Buckets: []float64{30, 60, 120, 300, 600, 1800}, // 30s, 1m, 2m, 5m, 10m, 30m
		})
		TotalProcessDuration = promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "vod_processing_total_duration_seconds",
			Help:    "Total processing cycle duration seconds",
			Buckets: []float64{60, 300, 900, 1800, 3600, 7200}, // 1m to 2h
		})
		
		QueueDepthGauge = promauto.NewGauge(prometheus.GaugeOpts{Name: "vod_queue_depth", Help: "Current number of unprocessed VODs"})
		CircuitOpenGauge = promauto.NewGauge(prometheus.GaugeOpts{Name: "vod_circuit_open", Help: "Circuit breaker open=1 closed=0 (DEPRECATED: use vod_circuit_breaker_state)"})
		CircuitStateGauge = promauto.NewGauge(prometheus.GaugeOpts{Name: "vod_circuit_breaker_state", Help: "Circuit breaker state: 0=closed, 1=half-open, 2=open"})
		CircuitFailureCount = promauto.NewCounter(prometheus.CounterOpts{Name: "vod_circuit_breaker_failures_total", Help: "Total number of circuit breaker failures"})

		// Enhanced metrics
		ChatMessagesRecorded = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "chat_messages_recorded_total",
				Help: "Total number of chat messages recorded",
			},
			[]string{"channel"},
		)
		
		ChatReconnections = promauto.NewCounter(prometheus.CounterOpts{
			Name: "chat_reconnections_total",
			Help: "Total number of chat reconnections",
		})

		OAuthTokenRefresh = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "oauth_token_refresh_total",
				Help: "Total OAuth token refresh attempts",
			},
			[]string{"provider", "status"},
		)

		HelixAPICalls = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "helix_api_calls_total",
				Help: "Total Twitch Helix API calls",
			},
			[]string{"endpoint", "status"},
		)

		CircuitBreakerStateChanges = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "circuit_breaker_state_changes_total",
				Help: "Circuit breaker state transitions",
			},
			[]string{"from", "to"},
		)

		ProcessingStepDuration = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "vod_processing_step_duration_seconds",
				Help:    "Duration of individual processing steps",
				Buckets: []float64{60, 300, 600, 1800, 3600, 7200},
			},
			[]string{"step"},
		)

		DatabaseConnectionPoolSize = promauto.NewGauge(prometheus.GaugeOpts{
			Name: "database_connection_pool_size",
			Help: "Maximum database connection pool size",
		})

		DatabaseConnectionPoolInUse = promauto.NewGauge(prometheus.GaugeOpts{
			Name: "database_connection_pool_in_use",
			Help: "Current number of database connections in use",
		})
	})
}

// UpdateCircuitGauge sets gauge to 1 if open else 0 (DEPRECATED: use SetCircuitState).
func UpdateCircuitGauge(open bool) {
	if CircuitOpenGauge != nil {
		if open {
			CircuitOpenGauge.Set(1)
		} else {
			CircuitOpenGauge.Set(0)
		}
	}
}

// SetCircuitState sets the circuit state gauge. States: closed=0, half-open=1, open=2.
func SetCircuitState(state string) {
	if CircuitStateGauge != nil {
		switch state {
		case "closed":
			CircuitStateGauge.Set(0)
		case "half-open":
			CircuitStateGauge.Set(1)
		case "open":
			CircuitStateGauge.Set(2)
		default:
			CircuitStateGauge.Set(0) // default to closed
		}
	}
}

// IncrementCircuitFailures increments the circuit failure counter.
func IncrementCircuitFailures() {
	if CircuitFailureCount != nil {
		CircuitFailureCount.Inc()
	}
}

// SetQueueDepth records current unprocessed VOD count.
func SetQueueDepth(n int) {
	if QueueDepthGauge != nil {
		QueueDepthGauge.Set(float64(n))
	}
}

// TimeFunc measures the duration of fn and records in observer if non-nil.
func TimeFunc(obs prometheus.Observer, fn func()) time.Duration {
	start := time.Now()
	fn()
	d := time.Since(start)
	if obs != nil {
		obs.Observe(d.Seconds())
	}
	return d
}

// UpdateDatabasePoolMetrics updates the database connection pool metrics.
func UpdateDatabasePoolMetrics(maxOpen, inUse int) {
	if DatabaseConnectionPoolSize != nil {
		DatabaseConnectionPoolSize.Set(float64(maxOpen))
	}
	if DatabaseConnectionPoolInUse != nil {
		DatabaseConnectionPoolInUse.Set(float64(inUse))
	}
}

// RecordCircuitStateChange records a state transition in the circuit breaker.
func RecordCircuitStateChange(from, to string) {
	if CircuitBreakerStateChanges != nil {
		CircuitBreakerStateChanges.WithLabelValues(from, to).Inc()
	}
}

// Correlation ID helpers ----------------------------------------------------
type corrKeyType struct{}

var corrKey corrKeyType

// WithCorrelation returns a new context embedding correlation id (if absent) and the id.
func WithCorrelation(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, corrKey, id)
}

// GetCorrelation returns correlation id or empty string.
func GetCorrelation(ctx context.Context) string {
	v := ctx.Value(corrKey)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// LoggerWithCorr returns a logger with corr attribute if present.
func LoggerWithCorr(ctx context.Context) *slog.Logger {
	if id := GetCorrelation(ctx); id != "" {
		return slog.Default().With(slog.String("corr", id))
	}
	return slog.Default()
}
