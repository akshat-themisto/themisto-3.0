//go:build !windows

package main

// ensureCurrentUserProxyRegistration is a no-op on non-Windows platforms.
// Windows needs a best-effort HKCU WinINET fixup after install/reinstall
// because the Windows Service can become healthy while the current user's
// proxy hive is still stale. macOS uses networksetup (system-wide) and has
// no per-user proxy registry to reconcile, so this call is a no-op there.
func (a *App) ensureCurrentUserProxyRegistration() {}
