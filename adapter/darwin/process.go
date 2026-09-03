//go:build darwin

package darwin

/*
#include <libproc.h>
#include <sys/sysctl.h>
#include <sys/proc_info.h>
#include <pwd.h>
#include <stdlib.h>
#include <string.h>

// proc_pidpath wrapper — returns path length or 0 on error.
static int go_proc_pidpath(int pid, char *buf, int bufsize) {
	return proc_pidpath(pid, buf, (uint32_t)bufsize);
}

// proc_name wrapper.
static int go_proc_name(int pid, char *buf, int bufsize) {
	return proc_name(pid, buf, (uint32_t)bufsize);
}

// Resolve UID to username.
static const char* go_username(uid_t uid) {
	struct passwd *pw = getpwuid(uid);
	if (pw == NULL) return "";
	return pw->pw_name;
}
*/
import "C"

import (
	"bufio"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
)

// darwinProcessResolver implements iface.ProcessResolver using libproc and
// lsof as a fallback for connection-to-PID resolution.
type darwinProcessResolver struct {
	log   log.Logger
	cache *connCache
}

func newProcessResolver(logger log.Logger) *darwinProcessResolver {
	return &darwinProcessResolver{
		log:   logger,
		cache: newConnCache(1 * time.Second),
	}
}

// ---------------------------------------------------------------------------
// ResolveByPID
// ---------------------------------------------------------------------------

func (r *darwinProcessResolver) ResolveByPID(pid int) (domain.ProcessInfo, error) {
	info := domain.ProcessInfo{PID: pid}

	// Executable path.
	var pathBuf [C.PROC_PIDPATHINFO_MAXSIZE]C.char
	ret := C.go_proc_pidpath(C.int(pid), &pathBuf[0], C.int(len(pathBuf)))
	if ret <= 0 {
		return info, fmt.Errorf("proc_pidpath(%d): %w", pid, iface.ErrNotFound)
	}
	info.Path = C.GoString(&pathBuf[0])

	// Short name.
	var nameBuf [256]C.char
	ret = C.go_proc_name(C.int(pid), &nameBuf[0], C.int(len(nameBuf)))
	if ret > 0 {
		info.Name = C.GoString(&nameBuf[0])
	} else if info.Path != "" {
		parts := strings.Split(info.Path, "/")
		info.Name = parts[len(parts)-1]
	}

	// Process owner UID → username via kinfo_proc.
	info.User = r.resolveUser(pid)

	// Parent PID via kinfo_proc.
	info.ParentPID = r.resolveParentPID(pid)

	// Bundle ID (macOS .app bundles).
	info.BundleID = resolveBundleID(info.Path)

	// Code signature — best effort via codesign CLI.
	signed, signerID := resolveCodeSignature(info.Path)
	info.Signed = signed
	info.SignerID = signerID

	return info, nil
}

// ---------------------------------------------------------------------------
// ResolveByConnection
// ---------------------------------------------------------------------------

func (r *darwinProcessResolver) ResolveByConnection(
	localAddr string, localPort uint16,
	remoteAddr string, remotePort uint16,
) (domain.ProcessInfo, error) {
	key := connKey{localAddr, localPort, remoteAddr, remotePort}

	// Check cache.
	if info, ok := r.cache.get(key); ok {
		return info, nil
	}

	pid, err := r.findPIDByConnection(localAddr, localPort, remoteAddr, remotePort)
	if err != nil {
		return domain.ProcessInfo{}, err
	}

	info, err := r.ResolveByPID(pid)
	if err != nil {
		return info, err
	}

	r.cache.set(key, info)
	return info, nil
}

// findPIDByConnection uses lsof to map a connection four-tuple to a PID.
// This is the portable fallback; a faster approach would use
// sysctlbyname("net.inet.tcp.pcblist") but the struct layout is fragile.
func (r *darwinProcessResolver) findPIDByConnection(
	localAddr string, localPort uint16,
	remoteAddr string, remotePort uint16,
) (int, error) {
	// lsof -i TCP@<remoteAddr>:<remotePort> -sTCP:ESTABLISHED -Fn -n -P
	target := fmt.Sprintf("TCP@%s:%d", remoteAddr, remotePort)
	out, err := exec.Command("lsof", "-i", target, "-sTCP:ESTABLISHED", "-Fn", "-n", "-P").Output()
	if err != nil {
		return 0, fmt.Errorf("lsof: %w: %w", err, iface.ErrNotFound)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	var lastPID int
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "p") {
			pid, err := strconv.Atoi(line[1:])
			if err == nil {
				lastPID = pid
			}
		}
		if strings.HasPrefix(line, "n") {
			// Match the local side: n<localAddr>:<localPort>->...
			connStr := line[1:]
			parts := strings.SplitN(connStr, "->", 2)
			if len(parts) == 2 {
				lh, lp := parseHostPort(parts[0])
				if lh == localAddr && lp == localPort && lastPID > 0 {
					return lastPID, nil
				}
				// Also try matching with IPv4-mapped IPv6.
				if lh == "::1" && localAddr == "127.0.0.1" && lp == localPort && lastPID > 0 {
					return lastPID, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("no process found for %s:%d->%s:%d: %w",
		localAddr, localPort, remoteAddr, remotePort, iface.ErrNotFound)
}

// ---------------------------------------------------------------------------
// kinfo_proc helpers (sysctl KERN_PROC)
// ---------------------------------------------------------------------------

func (r *darwinProcessResolver) resolveUser(pid int) string {
	mib := [4]C.int{C.CTL_KERN, C.KERN_PROC, C.KERN_PROC_PID, C.int(pid)}
	var kp C.struct_kinfo_proc
	size := C.size_t(unsafe.Sizeof(kp))

	ret := C.sysctl(&mib[0], 4, unsafe.Pointer(&kp), &size, nil, 0)
	if ret != 0 {
		return ""
	}
	uid := kp.kp_eproc.e_ucred.cr_uid
	username := C.go_username(uid)
	return C.GoString(username)
}

func (r *darwinProcessResolver) resolveParentPID(pid int) int {
	mib := [4]C.int{C.CTL_KERN, C.KERN_PROC, C.KERN_PROC_PID, C.int(pid)}
	var kp C.struct_kinfo_proc
	size := C.size_t(unsafe.Sizeof(kp))

	ret := C.sysctl(&mib[0], 4, unsafe.Pointer(&kp), &size, nil, 0)
	if ret != 0 {
		return 0
	}
	return int(kp.kp_eproc.e_ppid)
}

// ---------------------------------------------------------------------------
// Bundle ID resolution
// ---------------------------------------------------------------------------

func resolveBundleID(path string) string {
	// Walk up from the binary to find a .app bundle.
	// Expected: /Applications/Foo.app/Contents/MacOS/Foo
	idx := strings.Index(path, ".app/")
	if idx < 0 {
		return ""
	}
	bundlePath := path[:idx+4] // includes ".app"
	plistPath := bundlePath + "/Contents/Info.plist"

	// Use defaults read to extract CFBundleIdentifier.
	out, err := exec.Command("defaults", "read", plistPath, "CFBundleIdentifier").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ---------------------------------------------------------------------------
// Code signature resolution
// ---------------------------------------------------------------------------

func resolveCodeSignature(path string) (signed bool, signerID string) {
	if path == "" {
		return false, ""
	}
	out, err := exec.Command("codesign", "-dvv", path).CombinedOutput()
	if err != nil {
		return false, ""
	}

	output := string(out)
	if strings.Contains(output, "valid on disk") || strings.Contains(output, "Identifier=") {
		signed = true
	}

	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Authority=") {
			signerID = strings.TrimPrefix(line, "Authority=")
			break
		}
		if strings.HasPrefix(line, "Identifier=") && signerID == "" {
			signerID = strings.TrimPrefix(line, "Identifier=")
		}
	}
	return signed, signerID
}

// ---------------------------------------------------------------------------
// connection cache
// ---------------------------------------------------------------------------

type connKey struct {
	localAddr  string
	localPort  uint16
	remoteAddr string
	remotePort uint16
}

type connCacheEntry struct {
	info   domain.ProcessInfo
	expiry time.Time
}

type connCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[connKey]connCacheEntry
}

func newConnCache(ttl time.Duration) *connCache {
	return &connCache{ttl: ttl, items: make(map[connKey]connCacheEntry)}
}

func (c *connCache) get(key connKey) (domain.ProcessInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok || time.Now().After(e.expiry) {
		delete(c.items, key)
		return domain.ProcessInfo{}, false
	}
	return e.info, true
}

func (c *connCache) set(key connKey, info domain.ProcessInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = connCacheEntry{info: info, expiry: time.Now().Add(c.ttl)}
}

func parseHostPort(s string) (string, uint16) {
	// Handle [::1]:port and 127.0.0.1:port
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return s, 0
	}
	p, _ := strconv.ParseUint(portStr, 10, 16)
	return host, uint16(p)
}
