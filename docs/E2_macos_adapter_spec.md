# E2 — macOS Adapter Technical Specification

> Platform adapter for macOS under strict mTLS enforcement.
> Implements `iface.OSAdapter` (SystemProxy, CertStore, ProcessResolver, ServiceManager, NetworkInfo).
> Specification only. No Go code.

---

## 1. System Proxy Registration

### Implementation: `SystemProxy` interface

The macOS adapter configures the system-wide HTTP/HTTPS proxy via the **System Configuration framework** (`SystemConfiguration.framework`), specifically the `SCDynamicStore` API.

### Register(ctx, host, port)

**Mechanism:**

1. Open an `SCDynamicStore` session.
2. Read the current proxy dictionary from `kSCDynamicStoreDomainState/Network/Global/Proxies`.
3. Check for existing proxy settings:
   - If a proxy is already configured and was NOT set by this agent → return `ErrConflict`.
   - If the same host:port is already set by this agent → return nil (idempotent).
4. Write the following keys to the proxy dictionary:

| Key | Value |
|---|---|
| `kSCPropNetProxiesHTTPEnable` | 1 |
| `kSCPropNetProxiesHTTPProxy` | host (e.g., `"127.0.0.1"`) |
| `kSCPropNetProxiesHTTPPort` | port (e.g., `8080`) |
| `kSCPropNetProxiesHTTPSEnable` | 1 |
| `kSCPropNetProxiesHTTPSProxy` | host |
| `kSCPropNetProxiesHTTPSPort` | port |

5. Store the registered host:port internally for `VerifyIntegrity` comparison.
6. Write an ownership marker: set a custom key `kThemistoAgentProxyMarker` in the proxy dictionary (or persist to a file at `~/Library/Application Support/Themisto/proxy_marker.json`).

**Alternative mechanism (networksetup CLI):**

If `SCDynamicStore` access is restricted (sandboxed environments), fall back to:

```
networksetup -setwebproxy <service> <host> <port>
networksetup -setsecurewebproxy <service> <host> <port>
```

Where `<service>` is discovered via `networksetup -listallnetworkservices` (use the active service, typically "Wi-Fi" or "Ethernet").

**Per-interface vs. global:**

macOS proxy settings are per-network-service (Wi-Fi, Ethernet, etc.). The adapter must:
- Enumerate all active network services.
- Apply proxy settings to each active service.
- Track which services were modified for accurate Unregister cleanup.

### Unregister(ctx)

1. Read the ownership marker to determine which services were modified.
2. For each modified service, clear the proxy settings:
   - Set `HTTPEnable` and `HTTPSEnable` to 0.
   - Remove the host and port keys.
3. Remove the ownership marker.
4. Idempotent: if no marker exists, return nil.

### State()

1. Read current proxy dictionary from `SCDynamicStore` (or `networksetup -getwebproxy`).
2. Populate `domain.ProxyState`:
   - `Host`: current proxy host.
   - `Port`: current proxy port.
   - `Active`: true if `HTTPEnable == 1`.
   - `OwnedByAgent`: true if the ownership marker is present and matches current settings.

### VerifyIntegrity()

1. Read live proxy settings for each service that was registered.
2. Compare against the internally stored host:port.
3. Return `intact=true` only if ALL modified services still have the correct proxy settings AND the ownership marker is present.
4. Return `intact=false` if any service was changed by an external actor (user, MDM, another app).

---

## 2. Launchd Integration

### Implementation: `ServiceManager` interface

The agent runs as a **launchd daemon** (system-level, runs as root) to ensure it starts before user login and survives user session changes.

### Plist Location

`/Library/LaunchDaemons/com.themisto.agent.plist`

### Plist Contents

| Key | Value | Rationale |
|---|---|---|
| `Label` | `com.themisto.agent` | Unique reverse-DNS identifier |
| `ProgramArguments` | `["/usr/local/bin/themisto-agent", "--config", "/etc/themisto/agent.yaml"]` | Binary path + config flag |
| `RunAtLoad` | `true` | Start on boot |
| `KeepAlive` | `true` | Restart on crash |
| `ThrottleInterval` | `10` | Minimum seconds between restart attempts |
| `StandardOutPath` | `/var/log/themisto/agent.stdout.log` | Stdout redirect |
| `StandardErrorPath` | `/var/log/themisto/agent.stderr.log` | Stderr redirect |
| `UserName` | `root` | Required for system proxy and keychain access |
| `GroupName` | `wheel` | Standard root group |
| `EnvironmentVariables` | `{"THEMISTO_ENV": "production"}` | Runtime environment |

### Install(ctx)

1. Write the plist file to `/Library/LaunchDaemons/`.
2. Set ownership to `root:wheel`, permissions to `0644`.
3. Run `launchctl bootstrap system /Library/LaunchDaemons/com.themisto.agent.plist` (macOS 10.11+).
4. Idempotent: if plist exists with identical content, return nil. If content differs, overwrite and re-bootstrap.
5. On `ErrPermission`: requires root. Caller (installer) must run with `sudo` or use an `SMJobBless` helper.

### Start(ctx, ready)

1. If running inside launchd (detected via `LAUNCH_DAEMON_SOCKET` or `getppid() == 1`):
   - Call `ready()` to signal the core.
   - Block on `ctx.Done()`.
   - On context cancellation, return nil.
2. If NOT running inside launchd (manual start or testing):
   - Run `launchctl kickstart -k system/com.themisto.agent`.
   - Call `ready()`.
   - Block on `ctx.Done()`.

### Stop(ctx)

1. Run `launchctl bootout system/com.themisto.agent`.
2. Or send `SIGTERM` to the running daemon PID.
3. Wait for process exit, up to context deadline.
4. Idempotent: if service is already stopped, return nil.

### Uninstall(ctx)

1. Verify service is stopped. If running, return `ErrConflict`.
2. Run `launchctl bootout system /Library/LaunchDaemons/com.themisto.agent.plist`.
3. Remove the plist file.
4. Remove log files from `/var/log/themisto/`.
5. Idempotent: if plist doesn't exist, return nil.

### Status()

1. Run `launchctl print system/com.themisto.agent`.
2. Parse output for PID and state.
3. Map to `domain.ServiceStatus`:
   - PID present and running → `ServiceRunning`.
   - Registered but no PID → `ServiceStopped`.
   - Not registered → `ServiceNotInstalled`.
   - Running but exit code non-zero in last run → `ServiceDegraded`.

---

## 3. System Keychain Certificate Installation

### Implementation: `CertStore` interface

Certificates are installed into the **System Keychain** (`/Library/Keychains/System.keychain`) so they are trusted by all users and all applications on the system.

### InstallCA(ctx, id, der)

1. Validate that `der` is a well-formed DER-encoded X.509 certificate. On failure, return `ErrInvalidCert`.
2. Parse the certificate to extract the subject for logging.
3. Check if a certificate with the same `id` already exists in the System Keychain:
   - Use `SecItemCopyMatching` with `kSecAttrLabel` set to `"Themisto-CA-<id>"`.
   - If found and byte-identical → return nil (idempotent).
   - If found but different → delete old, install new.
4. Add the certificate:
   - Use `SecItemAdd` with:
     - `kSecClass`: `kSecClassCertificate`
     - `kSecValueData`: DER bytes
     - `kSecAttrLabel`: `"Themisto-CA-<id>"`
     - `kSecAttrAccessible`: `kSecAttrAccessibleAlways`
   - Set trust to "Always Trust" for SSL:
     - Use `SecTrustSettingsSetTrustSettings` with `kSecTrustSettingsResult = kSecTrustSettingsResultTrustRoot` for `kSecPolicyAppleSSL`.
5. Requires root or admin with keychain authorization prompt.

**Alternative mechanism (security CLI):**

```
security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain <cert.der>
```

The label is set via a preceding `security add-certificates` with a label flag, or by importing then modifying trust settings.

### RemoveCA(ctx, id)

1. Query the System Keychain for the certificate with label `"Themisto-CA-<id>"`.
2. If not found → return nil (idempotent).
3. Delete via `SecItemDelete` or:
   ```
   security delete-certificate -c "Themisto-CA-<id>" /Library/Keychains/System.keychain
   ```
4. Verify removal by re-querying.

### HasCA(id)

1. Query the System Keychain for label `"Themisto-CA-<id>"`.
2. Return `(true, nil)` if found, `(false, nil)` if not.
3. Return error only if the keychain query itself fails.

### Security Considerations

- The agent installs ONLY the gateway's CA certificate. It never installs wildcard CAs or modifies existing system CAs.
- The label prefix `"Themisto-CA-"` ensures the adapter only touches certificates it owns.
- Private keys for client mTLS certificates are stored in the System Keychain with restricted ACLs (only the agent binary can access them).
- On macOS 10.14+, modifying the System Keychain triggers a user authorization prompt unless running as root.

---

## 4. Admin Privilege Model

### Elevation Strategy

The agent requires root privileges for:

| Operation | Why Root |
|---|---|
| System proxy registration | Modifying `SCDynamicStore` global proxy settings |
| System Keychain access | Installing/removing trusted CA certificates |
| Launchd daemon management | Writing to `/Library/LaunchDaemons/` |
| Process inspection | Reading process info for PIDs owned by other users |

### Privilege Acquisition

**Installation time (one-time):**
- The installer runs with `sudo` or uses an **Authorization Services** prompt (`AuthorizationExecuteWithPrivileges` or `SMJobBless`).
- Installs the binary to `/usr/local/bin/themisto-agent`.
- Installs the launchd plist.
- Installs the gateway CA certificate.
- Sets appropriate file ownership (`root:wheel`).

**Runtime:**
- The launchd daemon runs as `root` (specified in the plist `UserName` key).
- No runtime privilege escalation is needed — the daemon is already elevated.
- The daemon drops privileges for network operations where possible (proxy listener runs in-process but binds only to loopback).

### Principle of Least Privilege

- The daemon binary is owned by `root:wheel` with permissions `0755`.
- The config file at `/etc/themisto/agent.yaml` is owned by `root:wheel` with permissions `0600`.
- Log directory `/var/log/themisto/` is owned by `root:wheel` with permissions `0700`.
- The daemon does NOT request `com.apple.security.temporary-exception.*` entitlements.
- The daemon is NOT sandboxed (App Sandbox) because it needs system-wide keychain and proxy access.

### Gatekeeper and Notarization

- The binary must be code-signed with a valid Apple Developer ID certificate.
- The binary must be notarized with Apple for macOS 10.15+ (Catalina and later).
- Without notarization, Gatekeeper will block execution, and `launchctl` will refuse to load the daemon.

---

## 5. Process Metadata Extraction

### Implementation: `ProcessResolver` interface

### ResolveByPID(pid)

**Mechanism:**

1. Use `proc_pidpath(pid, buffer, bufferSize)` from `libproc.h` to get the executable path.
2. Use `proc_name(pid, buffer, bufferSize)` to get the short process name.
3. Get process owner via `proc_pidinfo` with `PROC_PIDTASKINFO` → extract UID → resolve to username via `getpwuid`.
4. Get parent PID via `proc_pidinfo` with `PROC_PIDLISTFDS` or `sysctl` with `KERN_PROC`.
5. Get bundle ID:
   - If the path ends with `.app/Contents/MacOS/<name>`, walk up to the `.app` bundle.
   - Read `Info.plist` → extract `CFBundleIdentifier`.
   - For non-app binaries, `BundleID` remains empty.
6. Get code signature info:
   - Use `SecStaticCodeCreateWithPath` to get a `SecStaticCodeRef`.
   - Use `SecStaticCodeCheckValidity` to verify the signature.
   - Use `SecCodeCopySigningInformation` with `kSecCSSigningInformation` to extract the signing identity.
   - Set `Signed = true` if validation passes.
   - Set `SignerID` to the `kSecCodeInfoIdentifier` value.

**Error cases:**
- PID not found → `ErrNotFound`.
- Insufficient permissions to inspect PID → `ErrPermission`.
- Partial data available → populate what is possible, leave rest at zero values.

### ResolveByConnection(localAddr, localPort, remoteAddr, remotePort)

**Mechanism:**

1. Use `proc_listpidspath` or enumerate all PIDs via `proc_listallpids`.
2. For each PID, use `proc_pidinfo` with `PROC_PIDLISTFDS` to list file descriptors.
3. For socket file descriptors, use `proc_pidfdinfo` with `PROC_PIDFDSOCKETINFO` to get socket details.
4. Match the four-tuple (localAddr, localPort, remoteAddr, remotePort) against the socket info.
5. When a match is found, call `ResolveByPID(pid)` to populate full `ProcessInfo`.

**Optimized alternative:**

Use `netstat` output parsing or the `sysctl` `net.inet.tcp.pcblist` MIB to enumerate TCP connections and their owning PIDs:

1. Call `sysctlbyname("net.inet.tcp.pcblist", ...)` to get the raw TCP PCB list.
2. Parse the `xinpgen` / `xtcpcb` structures to find the matching connection.
3. Extract the owning PID from the `so_owner` or `inp_socket` → `xsocket` → `so_uid` chain.
4. Call `ResolveByPID(pid)`.

**Performance target:** < 1ms for the connection lookup. The `sysctl` approach is preferred over enumerating all PIDs.

**Caching:** Short-lived cache (≤ 1s TTL) keyed by the connection four-tuple to avoid repeated `sysctl` calls for the same connection (e.g., HTTP keep-alive).

---

## 6. Proxy Tamper Detection

### Mechanism

The core calls `SystemProxy.VerifyIntegrity()` periodically (every 30s). The macOS adapter implements this as follows:

1. Read the live proxy settings from `SCDynamicStore` for each network service that was registered.
2. Compare against the remembered host:port from the last successful `Register` call.
3. Check the ownership marker file at `~/Library/Application Support/Themisto/proxy_marker.json`.
4. **Tamper detected if:**
   - Proxy host or port differs from remembered values.
   - Proxy is disabled (`HTTPEnable == 0`) when it should be enabled.
   - Ownership marker is missing or modified.
   - A new network service became active and does not have proxy settings.
5. On tamper detection, the adapter returns `intact=false`. The core:
   - Emits `proxy.tamper_detected` telemetry event with details.
   - Optionally re-registers the proxy (configurable via `AgentConfig.AutoReregister`).
   - Logs at WARN level.

### Additional detection layer: SCDynamicStore notification

Register a callback with `SCDynamicStoreSetNotificationKeys` to observe changes to `State:/Network/Global/Proxies` and per-service proxy keys. On change notification:
- Read new settings immediately.
- If they differ from the agent's registered values, flag as tampered.
- Notify the core asynchronously (via a channel or callback).

### MDM Profile Override Handling

- macOS MDM profiles can set proxy configurations that override `SCDynamicStore`.
- The adapter must detect MDM-managed proxy profiles via `SCDynamicStore` keys with `Setup:` prefix (MDM profiles appear under `Setup:/Network/...`).
- If an MDM profile is active, the adapter should:
  - Return `ErrConflict` from `Register` rather than overriding MDM.
  - Report `OwnedByAgent=false` in `State`.
  - Log the MDM profile presence at INFO level.

---

## 7. Uninstall Cleanup

The uninstall process must leave no artifacts on the system.

### Cleanup Checklist

| Artifact | Location | Cleanup Action |
|---|---|---|
| Launchd plist | `/Library/LaunchDaemons/com.themisto.agent.plist` | `launchctl bootout` then delete file |
| Agent binary | `/usr/local/bin/themisto-agent` | Delete file |
| Config file | `/etc/themisto/agent.yaml` | Delete file and directory |
| Config directory | `/etc/themisto/` | Remove directory if empty |
| Log files | `/var/log/themisto/` | Remove directory and contents |
| Gateway CA cert | System Keychain, label `"Themisto-CA-*"` | `RemoveCA` for all Themisto-labeled certs |
| Client mTLS cert | System Keychain | Remove by label prefix |
| Proxy settings | Per-service proxy config | `Unregister` (restore original) |
| Ownership marker | `~/Library/Application Support/Themisto/` | Remove file and directory |
| Cached data | `~/Library/Caches/com.themisto.agent/` | Remove directory and contents |

### Uninstall Sequence

1. `ServiceManager.Stop(ctx)` — stop the running daemon.
2. `SystemProxy.Unregister(ctx)` — restore original proxy settings.
3. `CertStore.RemoveCA(ctx, <all known IDs>)` — remove all Themisto certificates.
4. `ServiceManager.Uninstall(ctx)` — remove the launchd plist.
5. Remove files:
   - `/usr/local/bin/themisto-agent`
   - `/etc/themisto/` (recursive)
   - `/var/log/themisto/` (recursive)
   - `~/Library/Application Support/Themisto/` (recursive)
   - `~/Library/Caches/com.themisto.agent/` (recursive)
6. Verify: run `launchctl print system/com.themisto.agent` → should show "Could not find service".
7. Verify: `security find-certificate -c "Themisto" /Library/Keychains/System.keychain` → should return no results.

### Partial Uninstall Resilience

If uninstall is interrupted (power loss, kill -9):
- The next uninstall attempt must succeed (idempotent operations).
- The next install attempt must detect stale artifacts and clean them first.
- Each cleanup step must tolerate the artifact already being absent.

---

## 8. OS Compatibility

### Supported Versions

| macOS Version | Codename | Support Level | Notes |
|---|---|---|---|
| 15.x | Sequoia | Full | Primary development target |
| 14.x | Sonoma | Full | |
| 13.x | Ventura | Full | |
| 12.x | Monterey | Full | Minimum required version |
| 11.x | Big Sur | Best-effort | Approaching EOL |
| 10.15 | Catalina | Unsupported | Notarization required but limited testing |
| < 10.15 | — | Unsupported | No notarization support, no modern launchctl |

### Architecture Support

| Architecture | Support |
|---|---|
| arm64 (Apple Silicon) | Full — primary target |
| amd64 (Intel) | Full |
| Universal Binary | Required — ship a single fat binary via `lipo` |

### Version-Specific Considerations

| Feature | Version Constraint | Adapter Behavior |
|---|---|---|
| `launchctl bootstrap/bootout` | macOS 10.11+ | Required. Earlier versions unsupported. |
| System Keychain trust settings | macOS 10.12+ | Use `SecTrustSettingsSetTrustSettings`. |
| Notarization requirement | macOS 10.15+ | Binary must be notarized or Gatekeeper blocks execution. |
| `proc_pidpath` | All supported versions | Available since macOS 10.5. |
| `SCDynamicStore` proxy APIs | All supported versions | Stable. |
| TLS 1.3 | macOS 10.15+ | On older versions (12+), Go's crypto/tls handles TLS, not Security.framework. |
| Hardened Runtime | macOS 10.14+ | Required for notarization. Entitlements: none needed beyond default. |

### Build Tags

The macOS adapter uses the `darwin` build tag:

```
//go:build darwin
```

All files in `adapter/darwin/` carry this tag. The adapter is only compiled when targeting macOS.

### System Integrity Protection (SIP)

- SIP does NOT block the agent's operations:
  - `/usr/local/bin/` is writable (not SIP-protected).
  - `/Library/LaunchDaemons/` is writable by root.
  - System Keychain is modifiable by root.
  - `SCDynamicStore` is accessible by root.
- The adapter must NOT attempt to modify SIP-protected locations (`/usr/bin/`, `/System/`).

### FileVault / Full Disk Encryption

- No impact on the adapter. The agent operates in-memory for proxy handling.
- Config and logs on disk are accessible after login (daemon starts at boot, before FileVault unlock for user volumes, but `/etc/` and `/Library/` are on the system volume which is always unlocked).
