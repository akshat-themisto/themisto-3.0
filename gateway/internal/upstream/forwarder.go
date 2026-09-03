package upstream

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type Forwarder struct {
	client *http.Client
}

func NewForwarder(timeout time.Duration) *Forwarder {
	transport := &http.Transport{
		Proxy:               nil,
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
		DialContext: dialWithIPv4Fallback(&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}),
	}

	return &Forwarder{
		client: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (f *Forwarder) Forward(w http.ResponseWriter, r *http.Request) (status int, bytesSent int64, err error) {
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
	if err != nil {
		return 0, 0, err
	}

	copyHeaders(outReq.Header, r.Header)
	removeHopHeaders(outReq.Header)
	removeInternalHeaders(outReq.Header)

	resp, err := f.client.Do(outReq)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	removeHopHeaders(w.Header())
	w.WriteHeader(resp.StatusCode)

	written, _ := io.Copy(w, resp.Body)
	return resp.StatusCode, written, nil
}

func removeInternalHeaders(h http.Header) {
	for key := range h {
		if strings.HasPrefix(strings.ToLower(key), "x-themisto-") {
			h.Del(key)
		}
	}
}

// Tunnel opens a raw CONNECT tunnel and pipes bytes between the client and
// the upstream target.
func (f *Forwarder) Tunnel(w http.ResponseWriter, r *http.Request) (status int, bytesSent int64, err error) {
	target := normalizeConnectTarget(r.Host)
	upstreamConn, err := dialContext(r.Context(), &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}, "tcp", target)
	if err != nil {
		return 0, 0, err
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		upstreamConn.Close()
		return http.StatusInternalServerError, 0, fmt.Errorf("hijack not supported")
	}

	clientConn, clientRW, err := hijacker.Hijack()
	if err != nil {
		upstreamConn.Close()
		return http.StatusInternalServerError, 0, err
	}

	if _, err := io.WriteString(clientRW, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		_ = clientConn.Close()
		_ = upstreamConn.Close()
		return http.StatusBadGateway, 0, err
	}
	if err := clientRW.Flush(); err != nil {
		_ = clientConn.Close()
		_ = upstreamConn.Close()
		return http.StatusBadGateway, 0, err
	}

	var tunnelClient net.Conn = clientConn
	if clientRW != nil && clientRW.Reader.Buffered() > 0 {
		tunnelClient = &bufferedConn{Conn: clientConn, reader: clientRW.Reader}
	}
	copyBidirectional(tunnelClient, upstreamConn)
	return http.StatusOK, 0, nil
}

func (f *Forwarder) Close() {
	f.client.CloseIdleConnections()
}

var hopHeaders = []string{
	"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
	"Te", "Trailers", "Transfer-Encoding", "Upgrade",
}

func removeHopHeaders(h http.Header) {
	for _, hdr := range hopHeaders {
		h.Del(hdr)
	}
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func normalizeConnectTarget(host string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return net.JoinHostPort(host, "443")
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *bufferedConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return c.Conn.Close()
}

func copyBidirectional(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}

	go cp(a, b)
	go cp(b, a)
	<-done
	<-done

	_ = a.Close()
	_ = b.Close()
}

func dialWithIPv4Fallback(d *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialContext(ctx, d, network, addr)
	}
}

func dialContext(ctx context.Context, d *net.Dialer, network, addr string) (net.Conn, error) {
	if network != "tcp" {
		return d.DialContext(ctx, network, addr)
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return d.DialContext(ctx, network, addr)
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return d.DialContext(ctx, network, addr)
	}

	var lastErr error
	for _, candidate := range orderedCandidates(port, ips) {
		conn, err := d.DialContext(ctx, candidate.network, candidate.addr)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("dial %s: no candidate addresses", addr)
}

type dialCandidate struct {
	network string
	addr    string
}

func orderedCandidates(port string, ips []net.IPAddr) []dialCandidate {
	candidates := make([]dialCandidate, 0, len(ips))

	appendFamily := func(wantIPv4 bool) {
		for _, ip := range ips {
			if (ip.IP.To4() != nil) != wantIPv4 {
				continue
			}
			network := "tcp6"
			if wantIPv4 {
				network = "tcp4"
			}
			candidates = append(candidates, dialCandidate{
				network: network,
				addr:    net.JoinHostPort(ip.IP.String(), port),
			})
		}
	}

	appendFamily(true)
	appendFamily(false)
	return candidates
}
