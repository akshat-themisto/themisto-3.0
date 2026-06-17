# E3 — Windows Adapter Technical Specification

> Platform adapter for Windows under strict mTLS enforcement.
> Implements `iface.OSAdapter` (SystemProxy, CertStore, ProcessResolver, ServiceManager, NetworkInfo).
> Specification only. No Go code.

---

## 1. WinHTTP + WinINET Proxy Configuration

### Implementation: `SystemProxy` interface

Windows has two independent HTTP client stacks, each with its own proxy settings. The adapter must configure both to ensure all local applications route traffic through the agent.

### WinINET (Internet Explorer / Edge Legacy / most desktop apps)

**Registry path:** `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`

| Value Name | Type | Data | Purpose |
|---|---|---|---|
| `ProxyEnable` | REG_DWORD | `1` | Enable proxy |
| `ProxyServer` | REG_SZ | `http=127.0.0.1:8080;https=127.0.0.1:8080` | Proxy address for HTTP and HTTPS |
| `ProxyOverride` | REG_SZ | `<local>` | Bypass proxy for local addresses |

**System-wide (all users):** Use `HKLM\Software\Microsoft\Windows\CurrentVersion\Internet Settings` instead. Requires admin privileges.

**Notification:** After writing registry keys, call `InternetSetOption(NULL, INTERNET_OPTION_SETTINGS_CHANGED, NULL, 0)` followed by `InternetSetOption(NULL, INTERNET_OPTION_REFRESH, NULL, 0)` to notify running applications of the proxy change.

### WinHTTP (system services, PowerShell, .NET HttpClient, Windows Update)

**API:** `WinHttpSetDefaultProxyConfiguration` with a `WINHTTP_PROXY_INFO` struct.

**Alternative CLI:** `netsh winhttp set proxy proxy-server="127.0.0.1:8080" bypass-list="<local>"`.

WinHTTP settings are stored in the registry at:
- 64-bit: `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Internet Settings\Connections\WinHttpSettings`
- 32-bit (WoW64): `HKLM\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Internet Settings\Connections\WinHttpSettings`

Both 32-bit and 64-bit entries must be written for full coverage on 64-bit Windows.

### Register(ctx, host, port)

1. Read current WinINET proxy settings from `HKCU` (and `HKLM` for system-wide).
2. Check for existing non-agent proxy:
   - Read the ownership marker registry value `HKLM\SOFTWARE\Themisto\Agent\ProxyOwner`.
   - If proxy is already set and marker is absent → return `ErrConflict`.
   - If proxy matches agent's host:port and marker is present → return nil (idempotent).
3. Save the original proxy settings to `HKLM\SOFTWARE\Themisto\Agent\OriginalProxy` for Unregister restoration.
4. Write WinINET proxy settings to both `HKCU` and `HKLM`.
5. Write WinHTTP proxy settings via `WinHttpSetDefaultProxyConfiguration` (or `netsh`).
6. Write both 32-bit and 64-bit WinHTTP registry entries.
7. Call `InternetSetOption` to notify applications.
8. Write the ownership marker.

### Unregister(ctx)

1. Read the ownership marker. If absent → return nil (idempotent).
2. Read the saved original proxy settings from `HKLM\SOFTWARE\Themisto\Agent\OriginalProxy`.
3. Restore the original WinINET settings (or clear them if no original proxy existed).
4. Restore the original WinHTTP settings.
5. Call `InternetSetOption` to notify applications.
6. Remove the ownership marker.
7. Remove the `OriginalProxy` backup.

### State()

1. Read `ProxyEnable` and `ProxyServer` from `HKCU\...\Internet Settings`.
2. Read the ownership marker.
3. Populate `domain.ProxyState`:
   - `Host`, `Port`: parsed from `ProxyServer`.
   - `Active`: `ProxyEnable == 1`.
   - `OwnedByAgent`: marker present and settings match.

### VerifyIntegrity()

1. Read live WinINET and WinHTTP proxy settings.
2. Compare against the remembered host:port.
3. Check the ownership marker.
4. Return `intact=true` only if ALL of:
   - WinINET `HKCU` matches.
   - WinINET `HKLM` matches.
   - WinHTTP 64-bit matches.
   - WinHTTP 32-bit matches (on 64-bit systems).
   - Ownership marker is present.

---

## 2. Registry Keys

### Themisto Agent Registry Hive

All agent-specific state is stored under `HKLM\SOFTWARE\Themisto\Agent\`.

| Key Path | Value Name | Type | Purpose |
|---|---|---|---|
| `HKLM\SOFTWARE\Themisto\Agent` | `ProxyOwner` | REG_SZ | Ownership marker (`"themisto-agent"`) |
| `HKLM\SOFTWARE\Themisto\Agent` | `OriginalProxyEnable` | REG_DWORD | Backup of original `ProxyEnable` |
| `HKLM\SOFTWARE\Themisto\Agent` | `OriginalProxyServer` | REG_SZ | Backup of original `ProxyServer` |
| `HKLM\SOFTWARE\Themisto\Agent` | `OriginalProxyOverride` | REG_SZ | Backup of original `ProxyOverride` |
| `HKLM\SOFTWARE\Themisto\Agent` | `RegisteredHost` | REG_SZ | Last registered proxy host |
| `HKLM\SOFTWARE\Themisto\Agent` | `RegisteredPort` | REG_DWORD | Last registered proxy port |
| `HKLM\SOFTWARE\Themisto\Agent` | `InstallPath` | REG_SZ | Agent binary install path |
| `HKLM\SOFTWARE\Themisto\Agent` | `ConfigPath` | REG_SZ | Configuration file path |
| `HKLM\SOFTWARE\Themisto\Agent` | `Version` | REG_SZ | Installed agent version |

### Registry Access Requirements

- Reading `HKCU`: no elevation needed (current user context).
- Writing `HKCU`: no elevation needed.
- Reading/Writing `HKLM`: requires administrator privileges.
- The Windows Service runs as `LOCAL SYSTEM`, which has full `HKLM` access.

### Registry Cleanup on Uninstall

Remove the entire `HKLM\SOFTWARE\Themisto\` subtree.

---

## 3. Windows Service Setup

### Implementation: `ServiceManager` interface

The agent runs as a **Windows Service** managed by the Service Control Manager (SCM).

### Service Configuration

| Property | Value |
|---|---|
| Service Name | `ThemistoAgent` |
| Display Name | `Themisto Traffic Governance Agent` |
| Description | `Intercepts and governs local HTTP/HTTPS traffic via policy-based routing.` |
| Binary Path | `"C:\Program Files\Themisto\themisto-agent.exe" --service` |
| Start Type | `SERVICE_AUTO_START` (automatic on boot) |
| Service Account | `LOCAL SYSTEM` |
| Recovery: First failure | Restart service (delay: 10s) |
| Recovery: Second failure | Restart service (delay: 30s) |
| Recovery: Subsequent | Restart service (delay: 60s) |
| Recovery: Reset fail count | After 86400 seconds (1 day) |
| Dependencies | `Tcpip`, `Dnscache` (networking must be up) |

### Install(ctx)

1. Open the SCM via `OpenSCManager(NULL, NULL, SC_MANAGER_CREATE_SERVICE)`.
2. Call `CreateService` with the parameters above.
3. Set the service description via `ChangeServiceConfig2` with `SERVICE_CONFIG_DESCRIPTION`.
4. Set recovery options via `ChangeServiceConfig2` with `SERVICE_CONFIG_FAILURE_ACTIONS`.
5. Write `InstallPath` and `Version` to the registry.
6. Idempotent: if service exists with identical config, return nil. If config differs, call `ChangeServiceConfig` to update.
7. On `ErrPermission`: requires administrator privileges.

### Start(ctx, ready)

1. If running as a Windows Service (detected via `golang.org/x/sys/windows/svc`):
   - Register the service handler via `svc.Run`.
   - In the handler's `Execute` method:
     - Report `svc.StartPending` to SCM.
     - Call `ready()` to signal the core.
     - Report `svc.Running` to SCM.
     - Wait for `svc.Stop` or `svc.Shutdown` command.
     - Report `svc.StopPending` to SCM.
     - Return.
2. If NOT running as a service (console mode for testing):
   - Call `ready()`.
   - Block on `ctx.Done()`.

### Stop(ctx)

1. Open the service via `OpenService(scm, "ThemistoAgent", SERVICE_STOP | SERVICE_QUERY_STATUS)`.
2. Send `SERVICE_CONTROL_STOP` via `ControlService`.
3. Poll `QueryServiceStatus` until `SERVICE_STOPPED` or context deadline.
4. Idempotent: if already stopped, return nil.

### Uninstall(ctx)

1. Check service status. If running → return `ErrConflict`.
2. Open the service with `DELETE` access.
3. Call `DeleteService`.
4. Close the service handle.
5. Idempotent: if service does not exist, return nil.

### Status()

1. Open the SCM and query the service.
2. Map `SERVICE_STATUS.dwCurrentState` to `domain.ServiceStatus`:
   - `SERVICE_RUNNING` → `ServiceRunning`
   - `SERVICE_STOPPED` → `ServiceStopped`
   - `SERVICE_START_PENDING` / `SERVICE_STOP_PENDING` → `ServiceRunning` (transitioning)
   - Service not found → `ServiceNotInstalled`
   - Running but win32 exit code non-zero → `ServiceDegraded`

---

## 4. Certificate Installation in Local Machine Store

### Implementation: `CertStore` interface

Certificates are installed into the **Local Machine Trusted Root Certification Authorities** store so they are trusted by all users and applications.

### Store Path

`Cert:\LocalMachine\Root` (certificate store name: `"ROOT"`, store location: `CERT_SYSTEM_STORE_LOCAL_MACHINE`)

### InstallCA(ctx, id, der)

1. Validate that `der` is a well-formed DER-encoded X.509 certificate. Return `ErrInvalidCert` if malformed.
2. Parse the certificate.
3. Open the Local Machine Root store via `CertOpenStore(CERT_STORE_PROV_SYSTEM, 0, 0, CERT_SYSTEM_STORE_LOCAL_MACHINE, "ROOT")`.
4. Check if a certificate with the same thumbprint already exists:
   - Use `CertFindCertificateInStore` with `CERT_FIND_SHA1_HASH`.
   - If found → return nil (idempotent).
5. Set a friendly name property on the certificate context:
   - Use `CertSetCertificateContextProperty` with `CERT_FRIENDLY_NAME_PROP_ID` set to `"Themisto-CA-<id>"`.
6. Add to the store via `CertAddCertificateContextToStore` with `CERT_STORE_ADD_REPLACE_EXISTING`.
7. Close the store.
8. Requires administrator privileges (`LOCAL SYSTEM` has these).

**Alternative (certutil CLI):**

```
certutil -addstore -f "Root" <cert.der>
```

**Alternative (PowerShell):**

```powershell
Import-Certificate -FilePath <cert.der> -CertStoreLocation Cert:\LocalMachine\Root
```

### RemoveCA(ctx, id)

1. Open the Local Machine Root store.
2. Enumerate certificates, searching for one with friendly name `"Themisto-CA-<id>"`.
   - Use `CertEnumCertificatesInStore` + `CertGetCertificateContextProperty` with `CERT_FRIENDLY_NAME_PROP_ID`.
3. If not found → return nil (idempotent).
4. Delete via `CertDeleteCertificateFromStore`.
5. Close the store.

### HasCA(id)

1. Open the Local Machine Root store.
2. Search for friendly name `"Themisto-CA-<id>"`.
3. Return `(true, nil)` if found, `(false, nil)` if not.
4. Return error only if the store query fails.

### Security Considerations

- The agent installs ONLY the gateway's CA. The friendly name prefix `"Themisto-CA-"` ensures the adapter only operates on certificates it owns.
- Private keys for client mTLS certificates are stored in the `MY` store (`Cert:\LocalMachine\My`) with non-exportable flag set.
- On Windows, adding a certificate to the Root store may trigger a security dialog unless running as `LOCAL SYSTEM` (the Windows Service context).
- Group Policy can restrict certificate installation. The adapter must detect `STATUS_ACCESS_DENIED` from the crypto API and return `ErrPermission`.

---

## 5. Required Privileges

### Windows Service Account: LOCAL SYSTEM

The agent runs as `LOCAL SYSTEM`, which provides:

| Privilege | Purpose |
|---|---|
| `SeTcbPrivilege` | Act as part of the OS (service registration) |
| `SeDebugPrivilege` | Inspect processes owned by other users (ProcessResolver) |
| Full `HKLM` access | Read/write registry (proxy settings, agent state) |
| Local Machine cert store access | Install/remove certificates |
| Service control | Start/stop/install services |
| Network bind | Bind to loopback address and port |

### Why Not a Custom Service Account?

- `LOCAL SYSTEM` is the simplest account with all required privileges.
- A dedicated service account (e.g., `NT SERVICE\ThemistoAgent`) would need explicit grants for cert store access and `SeDebugPrivilege`, adding installation complexity.
- The agent only binds to loopback and communicates outbound via mTLS — the attack surface from running as `LOCAL SYSTEM` is limited.

### User Account Control (UAC)

- Installation and uninstallation require UAC elevation (administrator prompt).
- The running service is not affected by UAC (services run outside the user session).
- Interactive CLI commands (e.g., `themisto-agent status`) should work without elevation by reading from `HKLM` registry keys (read access is available to all users for the `Themisto` subtree via explicit ACL set during install).

---

## 6. Defender Interaction Risks

### Potential Conflicts

| Risk | Description | Mitigation |
|---|---|---|
| **Binary flagged as suspicious** | The agent intercepts network traffic, which matches behavior of malware proxies. | Code-sign the binary with an EV (Extended Validation) certificate. Submit to Microsoft for MAPS/SmartScreen reputation. |
| **Real-time protection blocks proxy** | Defender may block the agent from binding to a port or intercepting traffic. | Add exclusion for the agent binary path in Defender via PowerShell: `Add-MpPreference -ExclusionPath "C:\Program Files\Themisto\"`. |
| **Tamper protection blocks registry writes** | Defender's Tamper Protection guards certain registry areas. | The proxy-related registry keys (`Internet Settings`) are NOT protected by Tamper Protection. The agent's own `HKLM\SOFTWARE\Themisto\` subtree is also not protected. |
| **Certificate installation alerts** | Adding a Root CA may generate a Windows Security alert or event log entry. | Expected behavior. The installer should document this. Running as `LOCAL SYSTEM` suppresses the interactive dialog. |
| **Network inspection conflict** | If Defender's network protection or SmartScreen inspects outbound traffic, it may interfere with the agent's mTLS connections. | The agent connects only to the known gateway host. If needed, add the gateway URL to Defender's exclusion list. |
| **Controlled Folder Access** | Defender CFA may block the agent from writing to protected folders. | Agent writes only to `C:\Program Files\Themisto\`, `C:\ProgramData\Themisto\`, and `HKLM` — none are CFA-protected by default. |

### Recommended Installer Actions

1. Code-sign the binary and installer with an EV certificate.
2. Submit the binary to Microsoft for SmartScreen reputation via the WDSI portal.
3. During installation, add a Defender exclusion for the install path:
   ```
   Add-MpPreference -ExclusionPath "C:\Program Files\Themisto"
   Add-MpPreference -ExclusionProcess "themisto-agent.exe"
   ```
4. Document the exclusion in the admin deployment guide.

---

## 7. Process Metadata Extraction

### Implementation: `ProcessResolver` interface

### ResolveByPID(pid)

**Mechanism:**

1. Open the process via `OpenProcess(PROCESS_QUERY_INFORMATION | PROCESS_VM_READ, FALSE, pid)`.
2. Get executable path via `QueryFullProcessImageName(hProcess, 0, buffer, &size)`.
3. Get process name: extract the filename from the path.
4. Get process owner:
   - `OpenProcessToken(hProcess, TOKEN_QUERY, &hToken)`.
   - `GetTokenInformation(hToken, TokenUser, ...)` → SID.
   - `LookupAccountSid(NULL, sid, name, &nameSize, domain, &domainSize, &sidType)` → `domain\username`.
5. Get parent PID:
   - Use `NtQueryInformationProcess` with `ProcessBasicInformation` → `PROCESS_BASIC_INFORMATION.InheritedFromUniqueProcessId`.
   - Or use `CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)` + `Process32First/Next` to walk the process list.
6. Code signature:
   - Use `WinVerifyTrust` with `WINTRUST_ACTION_GENERIC_VERIFY_V2` to verify the Authenticode signature of the executable.
   - If verification succeeds: `Signed = true`.
   - Extract signer info via `CryptQueryObject` + `CryptMsgGetParam(CMSG_SIGNER_INFO_PARAM)` → signer name from the certificate's subject.
   - Set `SignerID` to the signer's Common Name.
7. `BundleID`: always empty on Windows (macOS-only concept).

**Error cases:**
- `OpenProcess` fails with `ERROR_ACCESS_DENIED` → `ErrPermission`.
- `OpenProcess` fails with `ERROR_INVALID_PARAMETER` → `ErrNotFound`.
- Partial data: populate available fields, leave others zero.

### ResolveByConnection(localAddr, localPort, remoteAddr, remotePort)

**Mechanism:**

1. Call `GetExtendedTcpTable(NULL, &size, FALSE, AF_INET, TCP_TABLE_OWNER_PID_ALL, 0)` to get the buffer size.
2. Allocate buffer and call `GetExtendedTcpTable` again to get the `MIB_TCPTABLE_OWNER_PID` structure.
3. Iterate `dwNumEntries` entries. For each `MIB_TCPROW_OWNER_PID`:
   - Compare `dwLocalAddr:dwLocalPort` with `localAddr:localPort`.
   - Compare `dwRemoteAddr:dwRemotePort` with `remoteAddr:remotePort`.
   - Ports are in network byte order — use `ntohs()` for comparison.
4. When a match is found, extract `dwOwningPid` and call `ResolveByPID(pid)`.
5. For IPv6 connections, use `GetExtendedTcpTable` with `AF_INET6` and `MIB_TCP6TABLE_OWNER_PID`.

**Performance target:** < 1ms. `GetExtendedTcpTable` is fast; the iteration is O(n) on the number of TCP connections.

**Caching:** Short-lived cache (≤ 1s TTL) keyed by the connection four-tuple.

---

## 8. Proxy Tamper Detection

### Mechanism

The core calls `SystemProxy.VerifyIntegrity()` periodically (every 30s). The Windows adapter:

1. Read live WinINET proxy settings from `HKCU\...\Internet Settings`.
2. Read live WinINET proxy settings from `HKLM\...\Internet Settings`.
3. Read live WinHTTP proxy settings (64-bit and 32-bit entries).
4. Compare all values against the stored `RegisteredHost` and `RegisteredPort` from `HKLM\SOFTWARE\Themisto\Agent\`.
5. Check the `ProxyOwner` marker.
6. **Tamper detected if** any of the four proxy locations differ from expected values or the marker is missing.

### Additional detection: Registry change notification

Use `RegNotifyChangeKeyValue` on the `Internet Settings` key with `REG_NOTIFY_CHANGE_LAST_SET` flag to receive asynchronous notifications when proxy settings are modified:

1. During `Register`, set up a registry notification on `HKCU\...\Internet Settings`.
2. When the notification fires:
   - Read the new values.
   - Compare against expected.
   - If tampered, notify the core.
3. Re-register the notification (it's one-shot).

### Group Policy Override Handling

- Group Policy can push proxy settings via `HKLM\SOFTWARE\Policies\Microsoft\Windows\CurrentVersion\Internet Settings`.
- If Group Policy settings are present, they take precedence over `HKCU`/`HKLM` Internet Settings.
- The adapter must:
  - Check the `Policies` registry path during `Register`.
  - If GP proxy settings exist, return `ErrConflict`.
  - Report `OwnedByAgent=false` in `State`.

---

## 9. Uninstall Procedure

### Cleanup Checklist

| Artifact | Location | Cleanup Action |
|---|---|---|
| Windows Service | SCM: `ThemistoAgent` | `DeleteService` |
| Agent binary | `C:\Program Files\Themisto\themisto-agent.exe` | Delete directory |
| Config file | `C:\ProgramData\Themisto\agent.yaml` | Delete file |
| Data directory | `C:\ProgramData\Themisto\` | Remove recursively |
| Log files | `C:\ProgramData\Themisto\logs\` | Remove recursively |
| Gateway CA cert | `Cert:\LocalMachine\Root`, friendly name `"Themisto-CA-*"` | Remove matching certs |
| Client mTLS cert | `Cert:\LocalMachine\My` | Remove matching certs |
| WinINET proxy (HKCU) | `HKCU\...\Internet Settings` | Restore original |
| WinINET proxy (HKLM) | `HKLM\...\Internet Settings` | Restore original |
| WinHTTP proxy | `HKLM\...\WinHttpSettings` | Restore original (both 32/64) |
| Agent registry hive | `HKLM\SOFTWARE\Themisto\` | Remove subtree |
| Defender exclusions | Windows Defender preferences | Remove `ExclusionPath` and `ExclusionProcess` |
| Ownership marker | `HKLM\SOFTWARE\Themisto\Agent\ProxyOwner` | Removed with registry hive |

### Uninstall Sequence

1. `ServiceManager.Stop(ctx)` — stop the running service.
2. `SystemProxy.Unregister(ctx)` — restore original proxy settings.
3. `CertStore.RemoveCA(ctx, <all known IDs>)` — remove all Themisto certificates from Root and My stores.
4. `ServiceManager.Uninstall(ctx)` — delete the Windows Service.
5. Remove Defender exclusions:
   ```
   Remove-MpPreference -ExclusionPath "C:\Program Files\Themisto"
   Remove-MpPreference -ExclusionProcess "themisto-agent.exe"
   ```
6. Remove files:
   - `C:\Program Files\Themisto\` (recursive).
   - `C:\ProgramData\Themisto\` (recursive).
7. Remove registry:
   - `HKLM\SOFTWARE\Themisto\` (recursive delete).
8. Verify:
   - `sc query ThemistoAgent` → `FAILED 1060: The specified service does not exist`.
   - `Get-ChildItem Cert:\LocalMachine\Root | Where-Object { $_.FriendlyName -like "Themisto-*" }` → empty.
   - `reg query HKLM\SOFTWARE\Themisto` → `ERROR: The system was unable to find the specified registry key`.

### MSI / NSIS Considerations

- If packaged as an MSI, the uninstall sequence should be encoded in the MSI's Custom Actions (deferred, system context).
- If using NSIS, the `un.` section performs the above steps.
- The installer must handle "repair" scenarios: re-running install on an existing installation should update in place (idempotent install steps).

### Partial Uninstall Resilience

- Each step must tolerate the artifact already being absent.
- If the service is not found, skip service deletion.
- If registry keys are missing, skip registry cleanup.
- If certificates are not found, skip certificate removal.

---

## 10. OS Compatibility

### Supported Versions

| Windows Version | Support Level | Notes |
|---|---|---|
| Windows 11 (23H2, 24H2) | Full | Primary development target |
| Windows 11 (21H2, 22H2) | Full | |
| Windows 10 (22H2) | Full | Last supported Windows 10 version |
| Windows 10 (21H2, 21H1) | Best-effort | Approaching EOL |
| Windows Server 2022 | Full | Server deployment |
| Windows Server 2019 | Full | |
| Windows Server 2016 | Best-effort | |
| Windows 8.1 / Server 2012 R2 | Unsupported | EOL |

### Architecture Support

| Architecture | Support |
|---|---|
| amd64 (x86-64) | Full — primary target |
| arm64 | Full — Windows 11 on ARM |
| x86 (32-bit) | Unsupported |

### Version-Specific Considerations

| Feature | Version Constraint | Adapter Behavior |
|---|---|---|
| `GetExtendedTcpTable` | Windows Vista+ | Available on all supported versions. |
| `QueryFullProcessImageName` | Windows Vista+ | Available on all supported versions. |
| `WinVerifyTrust` (Authenticode) | All supported versions | Stable API. |
| WinHTTP proxy API | All supported versions | Stable. |
| Service recovery options | Windows Vista+ | Available on all supported versions. |
| `RegNotifyChangeKeyValue` | All supported versions | Stable. |
| TLS 1.3 (SChannel) | Windows 10 1903+ / Server 2022 | Go's crypto/tls handles TLS independently of SChannel. |
| Defender Tamper Protection | Windows 10 1903+ | Does not affect agent's registry keys. |

### Build Tags

The Windows adapter uses the `windows` build tag:

```
//go:build windows
```

All files in `adapter/windows/` carry this tag. The adapter is only compiled when targeting Windows.

### Windows Subsystem for Linux (WSL)

- WSL has its own network stack. The agent's proxy settings (WinINET/WinHTTP) do NOT affect WSL applications.
- Governing WSL traffic is out of scope for this adapter. It would require a separate Linux adapter inside the WSL environment.
- The adapter must not crash or behave unexpectedly when the agent binary is (accidentally) run inside WSL.

### Antivirus Compatibility (Beyond Defender)

- Third-party antivirus products (Norton, McAfee, Kaspersky, etc.) may flag the agent similarly to Defender.
- EV code signing is the primary mitigation.
- Per-vendor exclusion documentation should be provided in the admin deployment guide.
- The adapter itself does not interact with third-party AV APIs.
