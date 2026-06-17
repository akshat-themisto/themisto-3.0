package unified

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
)

// LogEntry is a structured telemetry record written as JSON.
type LogEntry struct {
	Timestamp string                 `json:"ts"`
	Level     string                 `json:"level"`
	Event     string                 `json:"event"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

// DevTelemetry implements telemetry.Metrics, telemetry.Events, and
// log.Logger by writing structured JSON to a writer (stdout or file).
//
// TODO(gateway-split): Replace with the real telemetry.Emitter that
// pushes batched metrics to the remote gateway's /telemetry endpoint.
type DevTelemetry struct {
	mu     sync.Mutex
	w      io.Writer
	isJSON bool
}

// NewDevTelemetry creates a telemetry logger writing to the configured output.
func NewDevTelemetry(cfg *DevConfig) (*DevTelemetry, error) {
	var w io.Writer
	if cfg.LogOutput == "" || cfg.LogOutput == "stdout" {
		w = os.Stdout
	} else {
		f, err := os.OpenFile(cfg.LogOutput, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		w = f
	}
	return &DevTelemetry{w: w, isJSON: cfg.LogJSON}, nil
}

// --- telemetry.Metrics ---

func (d *DevTelemetry) Counter(name string, value int64, labels map[string]string) {
	fields := map[string]interface{}{"value": value}
	for k, v := range labels {
		fields[k] = v
	}
	d.write("METRIC", name, fields)
}

func (d *DevTelemetry) Gauge(name string, value float64, labels map[string]string) {
	fields := map[string]interface{}{"value": value}
	for k, v := range labels {
		fields[k] = v
	}
	d.write("METRIC", name, fields)
}

func (d *DevTelemetry) Histogram(name string, value float64, labels map[string]string) {
	fields := map[string]interface{}{"value": value}
	for k, v := range labels {
		fields[k] = v
	}
	d.write("METRIC", name, fields)
}

// --- telemetry.Events ---

func (d *DevTelemetry) Emit(event string, payload *domain.EventPayload) error {
	fields := make(map[string]interface{})
	if payload != nil {
		for k, v := range payload.Data {
			fields[k] = v
		}
	}
	d.write("EVENT", event, fields)
	return nil
}

// --- log.Logger ---

func (d *DevTelemetry) Debug(msg string, kv ...interface{}) { d.log("DEBUG", msg, kv) }
func (d *DevTelemetry) Info(msg string, kv ...interface{})  { d.log("INFO", msg, kv) }
func (d *DevTelemetry) Warn(msg string, kv ...interface{})  { d.log("WARN", msg, kv) }
func (d *DevTelemetry) Error(msg string, kv ...interface{}) { d.log("ERROR", msg, kv) }
func (d *DevTelemetry) With(_ ...interface{}) log.Logger    { return d }

func (d *DevTelemetry) log(level, msg string, kv []interface{}) {
	fields := make(map[string]interface{})
	for i := 0; i+1 < len(kv); i += 2 {
		fields[fmt.Sprint(kv[i])] = kv[i+1]
	}
	d.write(level, msg, fields)
}

func (d *DevTelemetry) write(level, event string, fields map[string]interface{}) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.isJSON {
		entry := LogEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     level,
			Event:     event,
			Fields:    fields,
		}
		data, _ := json.Marshal(entry)
		d.w.Write(data)
		d.w.Write([]byte("\n"))
	} else {
		fmt.Fprintf(d.w, "%s level=%s event=%q", time.Now().Format("15:04:05.000"), level, event)
		for k, v := range fields {
			fmt.Fprintf(d.w, " %s=%v", k, v)
		}
		fmt.Fprintln(d.w)
	}
}
