package telemetry

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestHistogramsInitialized(t *testing.T) {
	// Ensure Init is called
	Init()

	// Check that histograms are initialized
	if DownloadDuration == nil {
		t.Error("DownloadDuration histogram not initialized")
	}
	if UploadDuration == nil {
		t.Error("UploadDuration histogram not initialized")
	}
	if TotalProcessDuration == nil {
		t.Error("TotalProcessDuration histogram not initialized")
	}
}

func TestHistogramObservations(t *testing.T) {
	// Ensure Init is called
	Init()

	tests := []struct {
		name      string
		histogram prometheus.Observer
		duration  time.Duration
	}{
		{"download", DownloadDuration, 5 * time.Minute},
		{"upload", UploadDuration, 2 * time.Minute},
		{"total", TotalProcessDuration, 10 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.histogram == nil {
				t.Fatalf("%s histogram is nil", tt.name)
			}

			// Record observation
			tt.histogram.Observe(tt.duration.Seconds())

			// Verify observation was recorded by checking the histogram collector
			// Note: This is a basic sanity check. In practice, you'd use prometheus testutil
			// or check metrics endpoint output for more thorough validation.
		})
	}
}

func TestTimeFuncRecordsObservation(t *testing.T) {
	// Ensure Init is called
	Init()

	// Create a mock histogram to verify observations
	testHistogram := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "test_duration_seconds",
		Help:    "Test duration",
		Buckets: prometheus.DefBuckets,
	})
	prometheus.MustRegister(testHistogram)
	defer prometheus.Unregister(testHistogram)

	// TimeFunc should measure and record duration
	executed := false
	duration := TimeFunc(testHistogram, func() {
		time.Sleep(10 * time.Millisecond)
		executed = true
	})

	if !executed {
		t.Error("TimeFunc did not execute provided function")
	}

	if duration < 10*time.Millisecond {
		t.Errorf("TimeFunc duration = %v, want >= 10ms", duration)
	}

	// Verify observation was recorded
	metric := &dto.Metric{}
	if err := testHistogram.Write(metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Histogram == nil {
		t.Fatal("Histogram metric is nil")
	}

	if *metric.Histogram.SampleCount == 0 {
		t.Error("TimeFunc did not record observation in histogram")
	}
}

func TestHistogramBuckets(t *testing.T) {
	// Verify histogram buckets are sensible for VOD operations
	tests := []struct {
		name            string
		histogram       prometheus.Observer
		expectedBuckets []float64
	}{
		{
			name:            "download",
			histogram:       DownloadDuration,
			expectedBuckets: []float64{60, 300, 600, 1800, 3600, 7200}, // 1m, 5m, 10m, 30m, 1h, 2h
		},
		{
			name:            "upload",
			histogram:       UploadDuration,
			expectedBuckets: []float64{30, 60, 120, 300, 600, 1800}, // 30s, 1m, 2m, 5m, 10m, 30m
		},
		{
			name:            "total",
			histogram:       TotalProcessDuration,
			expectedBuckets: []float64{60, 300, 900, 1800, 3600, 7200}, // 1m, 5m, 15m, 30m, 1h, 2h
		},
	}

	// Ensure Init is called
	Init()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.histogram == nil {
				t.Fatalf("%s histogram is nil", tt.name)
			}

			// Record observations in different buckets
			for _, bucket := range tt.expectedBuckets {
				tt.histogram.Observe(bucket - 1) // Observe just below bucket threshold
			}

			// This test verifies the histogram accepts observations without panicking
			// Actual bucket validation would require accessing internal prometheus metrics
		})
	}
}

func TestCircuitBreakerMetrics(t *testing.T) {
	Init()

	// Test circuit state setting
	states := []string{"closed", "half-open", "open", "invalid"}
	for _, state := range states {
		SetCircuitState(state)
		// Should not panic
	}

	// Test deprecated circuit gauge
	UpdateCircuitGauge(true)
	UpdateCircuitGauge(false)
}

func TestQueueDepthGauge(t *testing.T) {
	Init()

	depths := []int{0, 10, 50, 100}
	for _, depth := range depths {
		SetQueueDepth(depth)
		// Should not panic
	}
}

func TestCircuitFailureCounter(t *testing.T) {
	Init()

	// Increment should not panic
	IncrementCircuitFailures()
	IncrementCircuitFailures()
}

func TestDatabasePoolMetrics(t *testing.T) {
	Init()

	UpdateDatabasePoolMetrics(10, 5)
	UpdateDatabasePoolMetrics(100, 95)
}

func TestCircuitStateChange(t *testing.T) {
	Init()

	transitions := []struct {
		from string
		to   string
	}{
		{"closed", "open"},
		{"open", "half-open"},
		{"half-open", "closed"},
		{"half-open", "open"},
	}

	for _, tr := range transitions {
		RecordCircuitStateChange(tr.from, tr.to)
		// Should not panic
	}
}
