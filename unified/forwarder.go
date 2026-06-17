package unified

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/themisto/agent/pkg/dial"
)

// DirectForwarder sends requests directly to the upstream origin — no
// gateway relay, no mTLS. This is the development-mode replacement for
// the production GatewayClient.
//
// TODO(gateway-split): Replace this with transport.MtlsGatewayClient
// that relays via the remote gateway over mTLS. The proxy handler calls
// Forwarder.RoundTrip for HTTP and Forwarder.DialUpstream for CONNECT;
// the production version will relay both through the gateway instead of
// connecting directly.
type DirectForwarder struct {
	client *http.Client
}

// NewDirectForwarder creates a forwarder with sensible timeouts.
func NewDirectForwarder() *DirectForwarder {
	return &DirectForwarder{
		client: &http.Client{
			// TODO(mTLS): In production, this transport will carry the
			// agent's mTLS client certificate and trust only the gateway CA.
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
				DialContext: dial.ContextDialer(&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}),
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse // don't follow redirects
			},
			Timeout: 60 * time.Second,
		},
	}
}

// RoundTrip forwards an HTTP request directly to the upstream origin
// and returns the response. Used for non-CONNECT requests.
func (f *DirectForwarder) RoundTrip(req *http.Request) (*http.Response, error) {
	return f.client.Transport.RoundTrip(req)
}

// DialUpstream opens a raw TCP connection to the given host:port.
// Used for CONNECT tunnel forwarding. Falls back to tcp4 if the default
// tcp dial fails with an IPv6 network-unreachable error.
func (f *DirectForwarder) DialUpstream(host string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return dial.TCP(context.Background(), host, d)
}

// Close releases idle connections.
func (f *DirectForwarder) Close() {
	f.client.CloseIdleConnections()
}

// copyBidirectional pipes data between two connections until one side closes.
func copyBidirectional(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		io.Copy(dst, src)
		if tc, ok := dst.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	<-done
	a.Close()
	b.Close()
}
