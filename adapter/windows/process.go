//go:build windows

package windows

import (
	"bufio"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
)

// Win32 constants.
const (
	processQueryInformation = 0x0400
	processVMRead           = 0x0010
	tokenQuery              = 0x0008
	maxPath                 = 260

	thSnapProcess = 0x00000002

	tokenUser = 1
)

// Win32 DLLs.
var (
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")
	modAdvapi32 = syscall.NewLazyDLL("advapi32.dll")
	modIPHelper = syscall.NewLazyDLL("iphlpapi.dll")

	procOpenProcess                = modKernel32.NewProc("OpenProcess")
	procCloseHandle                = modKernel32.NewProc("CloseHandle")
	procQueryFullProcessImageNameW = modKernel32.NewProc("QueryFullProcessImageNameW")
	procCreateToolhelp32Snapshot   = modKernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW            = modKernel32.NewProc("Process32FirstW")
	procProcess32NextW             = modKernel32.NewProc("Process32NextW")

	procOpenProcessToken    = modAdvapi32.NewProc("OpenProcessToken")
	procGetTokenInformation = modAdvapi32.NewProc("GetTokenInformation")
	procLookupAccountSidW   = modAdvapi32.NewProc("LookupAccountSidW")

	procGetExtendedTcpTable = modIPHelper.NewProc("GetExtendedTcpTable")
)

// PROCESSENTRY32W for CreateToolhelp32Snapshot.
type processEntry32W struct {
	Size            uint32
	Usage           uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	Threads         uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [maxPath]uint16
}

// MIB_TCPROW_OWNER_PID from GetExtendedTcpTable.
type mibTcpRowOwnerPID struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPID  uint32
}

// winProcessResolver implements iface.ProcessResolver using Win32 APIs.
type winProcessResolver struct {
	log   log.Logger
	cache *connCache
}

func newProcessResolver(logger log.Logger) *winProcessResolver {
	return &winProcessResolver{
		log:   logger,
		cache: newConnCache(1 * time.Second),
	}
}

// ---------------------------------------------------------------------------
// ResolveByPID
// ---------------------------------------------------------------------------

func (r *winProcessResolver) ResolveByPID(pid int) (domain.ProcessInfo, error) {
	info := domain.ProcessInfo{PID: pid}

	hProcess, _, err := procOpenProcess.Call(
		uintptr(processQueryInformation|processVMRead),
		0,
		uintptr(pid),
	)
	if hProcess == 0 {
		if isAccessDenied(err) {
			return info, fmt.Errorf("open process %d: %w", pid, iface.ErrPermission)
		}
		return info, fmt.Errorf("open process %d: %w", pid, iface.ErrNotFound)
	}
	defer procCloseHandle.Call(hProcess)

	// Executable path via QueryFullProcessImageNameW.
	var pathBuf [maxPath * 2]uint16
	pathLen := uint32(len(pathBuf))
	ret, _, _ := procQueryFullProcessImageNameW.Call(
		hProcess, 0,
		uintptr(unsafe.Pointer(&pathBuf[0])),
		uintptr(unsafe.Pointer(&pathLen)),
	)
	if ret != 0 {
		info.Path = syscall.UTF16ToString(pathBuf[:pathLen])
		info.Name = filepath.Base(info.Path)
	}

	// Process owner.
	info.User = resolveProcessUser(hProcess)

	// Parent PID via toolhelp snapshot.
	info.ParentPID = resolveParentPID(uint32(pid))

	// Code signature via signtool / PowerShell.
	if info.Path != "" {
		info.Signed, info.SignerID = resolveAuthenticode(info.Path)
	}

	return info, nil
}

// ---------------------------------------------------------------------------
// ResolveByConnection
// ---------------------------------------------------------------------------

func (r *winProcessResolver) ResolveByConnection(
	localAddr string, localPort uint16,
	remoteAddr string, remotePort uint16,
) (domain.ProcessInfo, error) {
	key := connKey{localAddr, localPort, remoteAddr, remotePort}

	if info, ok := r.cache.get(key); ok {
		return info, nil
	}

	pid, err := r.findPIDByTcpTable(localAddr, localPort, remoteAddr, remotePort)
	if err != nil {
		// Fallback to netstat.
		pid, err = r.findPIDByNetstat(localAddr, localPort, remoteAddr, remotePort)
		if err != nil {
			return domain.ProcessInfo{}, err
		}
	}

	info, err := r.ResolveByPID(pid)
	if err != nil {
		return info, err
	}
	r.cache.set(key, info)
	return info, nil
}

// findPIDByTcpTable uses GetExtendedTcpTable to map a connection to a PID.
func (r *winProcessResolver) findPIDByTcpTable(
	localAddr string, localPort uint16,
	remoteAddr string, remotePort uint16,
) (int, error) {
	if procGetExtendedTcpTable.Find() != nil {
		return 0, fmt.Errorf("GetExtendedTcpTable not available: %w", iface.ErrUnsupported)
	}

	localIP := net.ParseIP(localAddr).To4()
	remoteIP := net.ParseIP(remoteAddr).To4()
	if localIP == nil || remoteIP == nil {
		return 0, fmt.Errorf("invalid IP addresses: %w", iface.ErrNotFound)
	}

	// First call to get buffer size.
	var size uint32
	procGetExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, 2 /*AF_INET*/, 5 /*TCP_TABLE_OWNER_PID_ALL*/, 0)
	if size == 0 {
		return 0, fmt.Errorf("GetExtendedTcpTable returned size 0: %w", iface.ErrNotFound)
	}

	buf := make([]byte, size)
	ret, _, _ := procGetExtendedTcpTable.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		0,
		2,  // AF_INET
		5,  // TCP_TABLE_OWNER_PID_ALL
		0,
	)
	if ret != 0 {
		return 0, fmt.Errorf("GetExtendedTcpTable failed: %d: %w", ret, iface.ErrNotFound)
	}

	numEntries := *(*uint32)(unsafe.Pointer(&buf[0]))
	rows := buf[4:]
	rowSize := unsafe.Sizeof(mibTcpRowOwnerPID{})

	wantLocalAddr := ipToUint32(localIP)
	wantRemoteAddr := ipToUint32(remoteIP)
	wantLocalPort := htons(localPort)
	wantRemotePort := htons(remotePort)

	for i := uint32(0); i < numEntries; i++ {
		row := (*mibTcpRowOwnerPID)(unsafe.Pointer(&rows[uintptr(i)*rowSize]))
		if row.LocalAddr == wantLocalAddr &&
			row.LocalPort == wantLocalPort &&
			row.RemoteAddr == wantRemoteAddr &&
			row.RemotePort == wantRemotePort {
			return int(row.OwningPID), nil
		}
	}

	return 0, fmt.Errorf("no match in TCP table: %w", iface.ErrNotFound)
}

// findPIDByNetstat uses netstat -aon as a fallback.
func (r *winProcessResolver) findPIDByNetstat(
	localAddr string, localPort uint16,
	remoteAddr string, remotePort uint16,
) (int, error) {
	out, err := exec.Command("netstat", "-aon", "-p", "TCP").Output()
	if err != nil {
		return 0, fmt.Errorf("netstat: %w: %w", err, iface.ErrNotFound)
	}

	localTarget := fmt.Sprintf("%s:%d", localAddr, localPort)
	remoteTarget := fmt.Sprintf("%s:%d", remoteAddr, remotePort)

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if fields[1] == localTarget && fields[2] == remoteTarget {
			pid, err := strconv.Atoi(fields[4])
			if err == nil {
				return pid, nil
			}
		}
	}

	return 0, fmt.Errorf("no match in netstat: %w", iface.ErrNotFound)
}

// ---------------------------------------------------------------------------
// Process user resolution
// ---------------------------------------------------------------------------

func resolveProcessUser(hProcess uintptr) string {
	var hToken syscall.Token
	ret, _, _ := procOpenProcessToken.Call(hProcess, tokenQuery, uintptr(unsafe.Pointer(&hToken)))
	if ret == 0 {
		return ""
	}
	defer syscall.CloseHandle(syscall.Handle(hToken))

	// Get token user size.
	var needed uint32
	procGetTokenInformation.Call(uintptr(hToken), tokenUser, 0, 0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 {
		return ""
	}

	buf := make([]byte, needed)
	ret, _, _ = procGetTokenInformation.Call(
		uintptr(hToken), tokenUser,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(needed),
		uintptr(unsafe.Pointer(&needed)),
	)
	if ret == 0 {
		return ""
	}

	// TOKEN_USER starts with a SID pointer.
	type tokenUserStruct struct {
		User struct {
			Sid        *syscall.SID
			Attributes uint32
		}
	}
	tu := (*tokenUserStruct)(unsafe.Pointer(&buf[0]))

	var nameLen, domainLen uint32
	var sidType uint32
	nameLen = 128
	domainLen = 128
	nameBuf := make([]uint16, nameLen)
	domainBuf := make([]uint16, domainLen)

	ret, _, _ = procLookupAccountSidW.Call(
		0,
		uintptr(unsafe.Pointer(tu.User.Sid)),
		uintptr(unsafe.Pointer(&nameBuf[0])), uintptr(unsafe.Pointer(&nameLen)),
		uintptr(unsafe.Pointer(&domainBuf[0])), uintptr(unsafe.Pointer(&domainLen)),
		uintptr(unsafe.Pointer(&sidType)),
	)
	if ret == 0 {
		return ""
	}

	domain := syscall.UTF16ToString(domainBuf[:domainLen])
	name := syscall.UTF16ToString(nameBuf[:nameLen])
	if domain != "" {
		return domain + `\` + name
	}
	return name
}

// ---------------------------------------------------------------------------
// Parent PID via Toolhelp32 snapshot
// ---------------------------------------------------------------------------

func resolveParentPID(pid uint32) int {
	hSnap, _, _ := procCreateToolhelp32Snapshot.Call(thSnapProcess, 0)
	if hSnap == 0 || hSnap == ^uintptr(0) {
		return 0
	}
	defer procCloseHandle.Call(hSnap)

	var pe processEntry32W
	pe.Size = uint32(unsafe.Sizeof(pe))

	ret, _, _ := procProcess32FirstW.Call(hSnap, uintptr(unsafe.Pointer(&pe)))
	for ret != 0 {
		if pe.ProcessID == pid {
			return int(pe.ParentProcessID)
		}
		ret, _, _ = procProcess32NextW.Call(hSnap, uintptr(unsafe.Pointer(&pe)))
	}
	return 0
}

// ---------------------------------------------------------------------------
// Authenticode signature
// ---------------------------------------------------------------------------

func resolveAuthenticode(path string) (bool, string) {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf(`(Get-AuthenticodeSignature '%s').Status`, path),
	).Output()
	if err != nil {
		return false, ""
	}
	status := strings.TrimSpace(string(out))
	if status != "Valid" {
		return false, ""
	}

	// Get signer subject.
	out, err = exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf(`(Get-AuthenticodeSignature '%s').SignerCertificate.Subject`, path),
	).Output()
	if err != nil {
		return true, ""
	}

	subject := strings.TrimSpace(string(out))
	// Extract CN from "CN=..., O=..., ..."
	for _, part := range strings.Split(subject, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "CN=") {
			return true, strings.TrimPrefix(part, "CN=")
		}
	}
	return true, subject
}

// ---------------------------------------------------------------------------
// Network byte order helpers
// ---------------------------------------------------------------------------

func htons(port uint16) uint32 {
	return uint32(port>>8 | port<<8)
}

func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	if ip == nil {
		return 0
	}
	return uint32(ip[0]) | uint32(ip[1])<<8 | uint32(ip[2])<<16 | uint32(ip[3])<<24
}

func isAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Access is denied")
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
