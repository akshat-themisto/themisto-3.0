package transport

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// mockConn is a minimal net.Conn for testing pipe().
type mockConn struct {
	net.Conn
	readBuf    *bytes.Buffer
	writeBuf   *bytes.Buffer
	mu         sync.Mutex
	closed     bool
	writeClosed bool
	readDone   chan struct{} // closed when readBuf is drained
}

func newMockConn(data []byte) *mockConn {
	return &mockConn{
		readBuf:  bytes.NewBuffer(data),
		writeBuf: &bytes.Buffer{},
		readDone: make(chan struct{}),
	}
}

func (m *mockConn) Read(p []byte) (int, error) {
	m.mu.Lock()
	n := m.readBuf.Len()
	m.mu.Unlock()
	if n == 0 {
		// Block until closed.
		<-m.readDone
		return 0, io.EOF
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	nn, err := m.readBuf.Read(p)
	if m.readBuf.Len() == 0 {
		select {
		case <-m.readDone:
		default:
			close(m.readDone)
		}
	}
	return nn, err
}

func (m *mockConn) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeBuf.Write(p)
}

func (m *mockConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	select {
	case <-m.readDone:
	default:
		close(m.readDone)
	}
	return nil
}

func (m *mockConn) CloseWrite() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeClosed = true
	return nil
}

func (m *mockConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (m *mockConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func (m *mockConn) Written() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeBuf.Bytes()
}

func TestBufferedConn_CloseWrite(t *testing.T) {
	inner := newMockConn(nil)
	bc := &bufferedConn{
		Conn:   inner,
		reader: bufio.NewReader(bytes.NewReader(nil)),
	}

	if err := bc.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite returned error: %v", err)
	}

	inner.mu.Lock()
	defer inner.mu.Unlock()
	if !inner.writeClosed {
		t.Fatal("CloseWrite did not delegate to inner conn")
	}
}

func TestBufferedConn_CloseWrite_Fallback(t *testing.T) {
	// A conn without CloseWrite should fall back to Close.
	inner := &noCloseWriteConn{mockConn: newMockConn(nil)}
	bc := &bufferedConn{
		Conn:   inner,
		reader: bufio.NewReader(bytes.NewReader(nil)),
	}

	if err := bc.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite fallback returned error: %v", err)
	}

	inner.mockConn.mu.Lock()
	defer inner.mockConn.mu.Unlock()
	if !inner.mockConn.closed {
		t.Fatal("CloseWrite fallback did not call Close")
	}
}

// noCloseWriteConn wraps mockConn but hides CloseWrite.
type noCloseWriteConn struct {
	mockConn *mockConn
}

func (n *noCloseWriteConn) Read(p []byte) (int, error)         { return n.mockConn.Read(p) }
func (n *noCloseWriteConn) Write(p []byte) (int, error)        { return n.mockConn.Write(p) }
func (n *noCloseWriteConn) Close() error                       { return n.mockConn.Close() }
func (n *noCloseWriteConn) LocalAddr() net.Addr                { return n.mockConn.LocalAddr() }
func (n *noCloseWriteConn) RemoteAddr() net.Addr               { return n.mockConn.RemoteAddr() }
func (n *noCloseWriteConn) SetDeadline(t time.Time) error      { return n.mockConn.SetDeadline(t) }
func (n *noCloseWriteConn) SetReadDeadline(t time.Time) error  { return n.mockConn.SetReadDeadline(t) }
func (n *noCloseWriteConn) SetWriteDeadline(t time.Time) error { return n.mockConn.SetWriteDeadline(t) }

func TestPipe_CloseWriteSignalsEOF(t *testing.T) {
	// Regression test: pipe() must return promptly when one side's data is
	// consumed, not hang indefinitely waiting for the other direction.
	a := newMockConn([]byte("hello from A"))
	b := newMockConn([]byte("hello from B"))

	done := make(chan struct{})
	go func() {
		pipe(a, b)
		close(done)
	}()

	select {
	case <-done:
		// pipe() returned — success
	case <-time.After(5 * time.Second):
		t.Fatal("pipe() hung — CloseWrite is not signaling EOF")
	}

	if got := string(b.Written()); got != "hello from A" {
		t.Errorf("b received %q, want %q", got, "hello from A")
	}
	if got := string(a.Written()); got != "hello from B" {
		t.Errorf("a received %q, want %q", got, "hello from B")
	}
}

func TestPipe_BidirectionalData(t *testing.T) {
	payload := bytes.Repeat([]byte("X"), 4096)
	a := newMockConn(payload)
	b := newMockConn(payload)

	done := make(chan struct{})
	go func() {
		pipe(a, b)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("pipe() timed out on bidirectional data transfer")
	}

	if len(a.Written()) != len(payload) {
		t.Errorf("a received %d bytes, want %d", len(a.Written()), len(payload))
	}
	if len(b.Written()) != len(payload) {
		t.Errorf("b received %d bytes, want %d", len(b.Written()), len(payload))
	}
}
