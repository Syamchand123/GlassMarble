package product

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Span represents a single timed phase execution span (11.4 / W7-02).
type Span struct {
	Name       string        `json:"name"`
	StartTime  time.Time     `json:"start_time"`
	EndTime    time.Time     `json:"end_time"`
	Duration   time.Duration `json:"duration_ns"`
	DurationMS float64       `json:"duration_ms"`
}

// TelemetryRecorder records phase spans during pipeline execution.
type TelemetryRecorder struct {
	mu         sync.Mutex
	Spans      []Span `json:"spans"`
	StorageDir string `json:"storage_dir"`
}

var globalRecorder = &TelemetryRecorder{}

// StartSpan starts a named phase span.
func StartSpan(name string) func() {
	start := time.Now()
	return func() {
		end := time.Now()
		dur := end.Sub(start)
		span := Span{
			Name:       name,
			StartTime:  start,
			EndTime:    end,
			Duration:   dur,
			DurationMS: float64(dur.Nanoseconds()) / 1e6,
		}
		globalRecorder.RecordSpan(span)
	}
}

// RecordSpan records a completed phase span.
func (tr *TelemetryRecorder) RecordSpan(span Span) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.Spans = append(tr.Spans, span)
}

// SaveTelemetry persists telemetry data to .glassmarble/telemetry.json.
// C6-D21: write to a temp file then atomically rename so concurrent
// `visualize` + `analyze` do not interleave writes and corrupt JSON.
func SaveTelemetry(storageDir string) error {
	globalRecorder.mu.Lock()
	defer globalRecorder.mu.Unlock()

	if storageDir == "" {
		storageDir = ".glassmarble"
	}
	_ = os.MkdirAll(storageDir, 0755)

	outPath := filepath.Join(storageDir, "telemetry.json")
	data, err := json.MarshalIndent(globalRecorder.Spans, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := outPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, outPath)
}

// LoadTelemetry loads saved telemetry spans from .glassmarble/telemetry.json.
func LoadTelemetry(storageDir string) ([]Span, error) {
	if storageDir == "" {
		storageDir = ".glassmarble"
	}
	inPath := filepath.Join(storageDir, "telemetry.json")
	data, err := os.ReadFile(inPath)
	if err != nil {
		return nil, err
	}
	var spans []Span
	err = json.Unmarshal(data, &spans)
	return spans, err
}
