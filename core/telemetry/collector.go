package telemetry

import (
	"sync"
	"time"

	"github.com/themisto/agent/core/domain"
)

// entryKind distinguishes metric entries from event entries in the ring buffer.
type entryKind int

const (
	kindCounter   entryKind = iota
	kindGauge
	kindHistogram
	kindEvent
)

// entry is a single buffered telemetry record.
type entry struct {
	Kind      entryKind
	Name      string
	IValue    int64
	FValue    float64
	Labels    map[string]string
	Payload   *domain.EventPayload
	Timestamp time.Time
}

// Collector implements the Metrics and Events interfaces with an in-memory
// ring buffer. Oldest entries are silently dropped when the buffer is full.
type Collector struct {
	mu   sync.Mutex
	buf  []entry
	cap  int
	head int // next write position
	size int // number of valid entries
}

// NewCollector creates a collector with the given buffer capacity.
func NewCollector(capacity int) *Collector {
	if capacity <= 0 {
		capacity = 10000
	}
	return &Collector{
		buf: make([]entry, capacity),
		cap: capacity,
	}
}

// Counter increments a counter metric.
func (c *Collector) Counter(name string, value int64, labels map[string]string) {
	c.push(entry{Kind: kindCounter, Name: name, IValue: value, Labels: labels, Timestamp: time.Now()})
}

// Gauge sets a gauge metric.
func (c *Collector) Gauge(name string, value float64, labels map[string]string) {
	c.push(entry{Kind: kindGauge, Name: name, FValue: value, Labels: labels, Timestamp: time.Now()})
}

// Histogram records a histogram observation.
func (c *Collector) Histogram(name string, value float64, labels map[string]string) {
	c.push(entry{Kind: kindHistogram, Name: name, FValue: value, Labels: labels, Timestamp: time.Now()})
}

// Emit records a discrete event.
func (c *Collector) Emit(event string, payload *domain.EventPayload) error {
	c.push(entry{Kind: kindEvent, Name: event, Payload: payload, Timestamp: time.Now()})
	return nil
}

func (c *Collector) push(e entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf[c.head] = e
	c.head = (c.head + 1) % c.cap
	if c.size < c.cap {
		c.size++
	}
}

// Drain removes and returns all buffered entries in insertion order.
func (c *Collector) Drain() []entry {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.size == 0 {
		return nil
	}

	out := make([]entry, c.size)
	start := (c.head - c.size + c.cap) % c.cap
	for i := 0; i < c.size; i++ {
		out[i] = c.buf[(start+i)%c.cap]
	}
	c.size = 0
	c.head = 0
	return out
}

// Size returns the number of buffered entries.
func (c *Collector) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.size
}

// Peek returns up to limit recent entries without removing them from the buffer.
// Entries are returned in insertion order (oldest first).
func (c *Collector) Peek(limit int) []entry {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.size == 0 {
		return nil
	}

	n := c.size
	if limit > 0 && limit < n {
		n = limit
	}

	out := make([]entry, n)
	// Read the most recent n entries
	start := (c.head - n + c.cap) % c.cap
	for i := 0; i < n; i++ {
		out[i] = c.buf[(start+i)%c.cap]
	}
	return out
}
