// Package dial provides helpers for TCP dials that prefer IPv4 addresses when
// a hostname resolves to both IPv4 and IPv6. This keeps dual-stack
// environments moving when IPv6 is configured but not actually routable.
package dial

import (
	"context"
	"fmt"
	"net"
)

// ContextDialer returns a DialContext function that prefers IPv4 addresses
// when a hostname resolves to both IPv4 and IPv6. This avoids dual-stack
// environments getting stuck on unroutable IPv6 addresses.
func ContextDialer(d *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialContext(ctx, d, network, addr)
	}
}

// TCP dials addr over TCP, preferring IPv4 when the hostname resolves to both
// IPv4 and IPv6 addresses.
func TCP(ctx context.Context, addr string, d *net.Dialer) (net.Conn, error) {
	return dialContext(ctx, d, "tcp", addr)
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
