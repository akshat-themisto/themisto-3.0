package core

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestWaitForProxyListener_EarlyError(t *testing.T) {
	a := &Agent{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	errCh <- errors.New("proxy failed")

	err := a.waitForProxyListener(ctx, "127.0.0.1", 65001, errCh, time.Second)
	if err == nil {
		t.Fatal("expected proxy error")
	}
	if err.Error() != "proxy failed" {
		t.Fatalf("error = %q, want %q", err.Error(), "proxy failed")
	}
}

func TestWaitForProxyListener_ContextCancelled(t *testing.T) {
	a := &Agent{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := a.waitForProxyListener(ctx, "127.0.0.1", 65001, make(chan error, 1), time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestWaitForProxyListener_BecomesReadyDuringStartup(t *testing.T) {
	a := &Agent{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.ParseUint(portStr, 10, 16)
	targetPort := uint16(port)

	ln.Close()

	go func() {
		time.Sleep(150 * time.Millisecond)
		lateLn, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", portStr))
		if err == nil {
			defer lateLn.Close()
			<-ctx.Done()
		}
	}()

	if err := a.waitForProxyListener(ctx, "127.0.0.1", targetPort, make(chan error, 1), time.Second); err != nil {
		t.Fatalf("waitForProxyListener() unexpected error: %v", err)
	}
}

func TestProbeListenAddr_ActiveListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.ParseUint(portStr, 10, 16)

	a := &Agent{}
	if !a.probeListenAddr("127.0.0.1", uint16(port)) {
		t.Error("probeListenAddr should return true for active listener")
	}
}

func TestProbeListenAddr_DeadPort(t *testing.T) {
	a := &Agent{}
	if a.probeListenAddr("127.0.0.1", 19) {
		t.Error("probeListenAddr should return false for port 19 (chargen, typically unused)")
	}
}

func TestProbeListenAddr_EmptyHost(t *testing.T) {
	// Empty host should default to 127.0.0.1 and not panic.
	a := &Agent{}
	// This should not panic regardless of whether the port is open.
	_ = a.probeListenAddr("", 65432)
}
