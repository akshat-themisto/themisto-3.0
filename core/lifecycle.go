// Package core implements the top-level agent that composes all core modules.
package core

import (
	"context"
	"sync"
)

// goroutineTracker manages background goroutines with a WaitGroup.
type goroutineTracker struct {
	wg  sync.WaitGroup
	ctx context.Context
}

func newGoroutineTracker(ctx context.Context) *goroutineTracker {
	return &goroutineTracker{ctx: ctx}
}

func (g *goroutineTracker) go_(name string, fn func(ctx context.Context)) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		fn(g.ctx)
	}()
}

func (g *goroutineTracker) wait() {
	g.wg.Wait()
}
