# macOS AI Ledger endpoint runbook

## Supported observations

The macOS LaunchDaemon runs as `root` and samples the data-driven endpoint product catalog every five minutes. It emits aggregate `ai.activity.v1` observations for matching executable names and application bundle identifiers. Built-in coverage includes ChatGPT and Claude desktop, Cursor, Claude Code, Windsurf, GitHub Copilot where its process is observable, Antigravity where its published process/bundle identifiers are present, Ollama, LM Studio, and catalog-delivered additions.

Chrome and Safari product use is observed through catalog-domain traffic when it traverses the existing system proxy/interception engine. This covers browser use such as ChatGPT, Claude, and Gemini without using browser history. Native HTTPS product/API activity is observed only for catalog hosts that the existing proxy/interception path can see. The Chromium native bridge provides prompt-protection signals on supported managed extension surfaces. A signed Safari Web Extension/native bridge is not included in this package; Safari domain activity can still be observed through the proxy.

MCP discovery checks known configurations for Claude Desktop, Claude Code, Cursor, Windsurf, and VS Code. Central telemetry contains only the client product key, a configured flag, server count, and transport categories. Server names, paths, commands, arguments, environment variables, credentials, and MCP payloads never leave the endpoint.

The catalog is embedded in `core/domain/ai_products.json`; policy `ai_products` entries extend it without changes to ledger or connector code.

## Privacy and identity

Endpoint events contain product/vendor keys, surface, device identity, activity kind/time, a privacy-safe source application name, optional safe model/project identifiers, opaque hashes, counts, and provenance. They do not contain prompts, bodies, files, file paths, matched values, MCP content, credentials, tokens, command arguments, or repository content. The gateway strictly rejects non-allowlisted fields and strips every `X-Themisto-*` header before forwarding a provider request.

The console username is used locally only to locate the active user's supported MCP configuration directories. It is never transmitted or treated as corporate identity. Assign the device to a normalized directory user through MDM enrollment or the administrator device-assignment API. Ambiguous/shared device intervals remain unattributed.

## Build and package

Requirements: macOS, Xcode Command Line Tools, Go, `clang`, `lipo`, `codesign`, and `pkgbuild`.

```bash
bash scripts/build-macos-installer.sh 0.1.0 dist/installers /path/to/device-agent.json
```

The build compiles Apple Silicon and Intel binaries, combines them into a universal Mach-O, applies an ad-hoc signature for local validation, builds a package, and writes a SHA-256 file. For release signing and notarization, provide:

```bash
MACOS_CODESIGN_IDENTITY="Developer ID Application: …" \
MACOS_INSTALLER_IDENTITY="Developer ID Installer: …" \
MACOS_NOTARY_PROFILE="themisto-notary" \
bash scripts/build-macos-installer.sh 0.1.0 dist/installers /path/to/device-agent.json
```

The notary profile must already exist in Keychain (`xcrun notarytool store-credentials`). No signing or notarization success should be claimed without those credentials.

## Install, enroll, and operate

Install through MDM or locally with administrator approval:

```bash
sudo installer -pkg dist/installers/ThemistoAgent-macos-universal-0.1.0.pkg -target /
sudo /usr/local/bin/themisto-activate -device-id DEVICE_ID -enrollment-token TOKEN -backend-url https://CONTROL
sudo /usr/local/bin/themisto-service install
sudo /usr/local/bin/themisto-service status
```

The canonical configuration is `/etc/themisto/agent.json` (mode `0600`), credentials are under `/etc/themisto/identity`, logs are under `/var/log/themisto`, and launchd uses `/Library/LaunchDaemons/com.themisto.agent.plist`. The installer preserves an existing configuration during upgrades. A newly supplied package configuration becomes the default only when no installed configuration exists.

The daemon installs interception trust only in `/Library/Keychains/System.keychain`; it never falls back to root's login Keychain. Configure required System Settings/TCC permissions through MDM where the deployment needs access to per-user application configuration. The agent continues with other observable surfaces when a user configuration cannot be read.

Stop/restart:

```bash
sudo /usr/local/bin/themisto-service stop
sudo /usr/local/bin/themisto-service start
```

Upgrade by installing the newer package. Preinstall stops the daemon, restores the previously owned proxy state, and records the previous binary, plist, and configuration. Roll back the most recent upgrade with:

```bash
sudo /usr/local/bin/themisto-rollback
```

Full uninstall (removes configuration, logs, proxy state, indexed Themisto trust roots, and package receipts):

```bash
sudo /usr/local/bin/themisto-uninstall
```

## Known limitations and external prerequisites

- System-proxy detection cannot see traffic that bypasses macOS proxy settings, uses unsupported QUIC paths, or is certificate-pinned. Themisto does not currently ship a Network Extension.
- An MDM-managed proxy profile is treated as a conflict instead of being overwritten. Coordinate the network design in MDM.
- Safari prompt-bridge parity requires a separately signed/notarized Safari extension and MDM enablement.
- Signing, notarization, MDM deployment, provider connector credentials, and design-partner validation require external access.
- The repository has a loopback-only semantic-classifier client and model-management scaffolding, but does not include a complete ONNX runtime, tokenizer, model, and production classifier server. Deterministic DLP remains active when the local sidecar is unavailable, and prompt content is never sent to the backend or gateway as fallback.
