package ailedger

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu       sync.RWMutex
	adapters map[string]ConnectorAdapter
}

func NewRegistry() *Registry {
	return &Registry{adapters: make(map[string]ConnectorAdapter)}
}

func (r *Registry) Register(adapter ConnectorAdapter) error {
	if adapter == nil {
		return fmt.Errorf("adapter is nil")
	}
	descriptor := adapter.Descriptor()
	key := normalizeKey(descriptor.AdapterKey)
	if key == "" || strings.TrimSpace(descriptor.DisplayName) == "" {
		return fmt.Errorf("adapter descriptor requires adapter_key and display_name")
	}
	if len(descriptor.AllowedHosts) == 0 {
		return fmt.Errorf("adapter %s must declare allowed_hosts", key)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.adapters[key]; exists {
		return fmt.Errorf("adapter %s already registered", key)
	}
	r.adapters[key] = adapter
	return nil
}

func (r *Registry) Lookup(key string) (ConnectorAdapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[normalizeKey(key)]
	return adapter, ok
}

func (r *Registry) Descriptors() []ConnectorDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ConnectorDescriptor, 0, len(r.adapters))
	for _, adapter := range r.adapters {
		out = append(out, adapter.Descriptor())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AdapterKey < out[j].AdapterKey })
	return out
}
