//go:build darwin

package darwin

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/pkg/log"
)

const (
	systemKeychain      = "/Library/Keychains/System.keychain"
	certLabelPrefix     = "Themisto-CA-"
	loginKeychainRelDir = "Library/Keychains/login.keychain-db"
)

type certIndex struct {
	Fingerprint string `json:"fingerprint"`
	Keychain    string `json:"keychain"`
}

// darwinCertStore implements iface.CertStore using the macOS security CLI.
type darwinCertStore struct {
	mu  sync.Mutex
	log log.Logger
}

func newCertStore(logger log.Logger) *darwinCertStore {
	return &darwinCertStore{log: logger}
}

// InstallCA adds a DER-encoded CA certificate to a trusted keychain.
// It prefers the System keychain and falls back to the user login keychain
// when admin permissions are unavailable.
func (cs *darwinCertStore) InstallCA(ctx context.Context, id string, der []byte) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return fmt.Errorf("parse certificate: %w: %w", err, iface.ErrInvalidCert)
	}

	fingerprint := sha256Hex(cert.Raw)
	if idx, err := cs.readIndex(id); err == nil && strings.EqualFold(idx.Fingerprint, fingerprint) {
		for _, keychain := range keychainSearchOrder(idx.Keychain) {
			has, checkErr := certFingerprintInKeychain(ctx, keychain, idx.Fingerprint)
			if checkErr == nil && has {
				return nil
			}
		}
	}

	tmpFile, err := writeTempDER(der, id)
	if err != nil {
		return fmt.Errorf("write temp cert: %w", err)
	}
	defer os.Remove(tmpFile)

	installedKeychain := ""
	if err := installTrustedCert(ctx, systemKeychain, tmpFile, true); err == nil {
		installedKeychain = systemKeychain
	} else {
		cs.log.Warn("system keychain install failed, trying login keychain", "id", id, "error", err)
		if certTrustedInKeychain(ctx, systemKeychain, tmpFile) {
			installedKeychain = systemKeychain
		}
		loginKeychain := userLoginKeychain()
		if installedKeychain == "" && strings.TrimSpace(loginKeychain) == "" {
			return fmt.Errorf("add-trusted-cert: %w", iface.ErrPermission)
		}
		if installedKeychain == "" {
			if loginErr := installTrustedCert(ctx, loginKeychain, tmpFile, false); loginErr != nil {
				if certTrustedInKeychain(ctx, loginKeychain, tmpFile) {
					cs.log.Warn("login keychain trust install failed but certificate is already trusted", "id", id, "error", loginErr)
				} else {
					return fmt.Errorf("add-trusted-cert system=%v login=%v: %w", err, loginErr, iface.ErrPermission)
				}
			}
			installedKeychain = loginKeychain
		}
	}

	if err := cs.writeIndex(id, cert, installedKeychain); err != nil {
		cs.log.Warn("failed to write cert index", "id", id, "error", err)
	}

	cs.log.Info("CA certificate installed", "id", id, "subject", cert.Subject.CommonName, "keychain", installedKeychain)
	return nil
}

// RemoveCA removes a previously installed CA certificate by id.
func (cs *darwinCertStore) RemoveCA(ctx context.Context, id string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if idx, err := cs.readIndex(id); err == nil && strings.TrimSpace(idx.Fingerprint) != "" {
		for _, keychain := range keychainSearchOrder(idx.Keychain) {
			if err := runCmd(ctx, "security", "delete-certificate", "-Z", idx.Fingerprint, keychain); err != nil {
				// Idempotent removal: certificate may already be absent in this keychain.
				cs.log.Debug("delete-certificate returned error (may be absent)", "id", id, "keychain", keychain, "error", err)
			}
		}
		cs.removeIndex(id)
		cs.log.Info("CA certificate removed", "id", id)
		return nil
	}

	// Fallback when index metadata is missing.
	label := certLabelPrefix + id
	for _, keychain := range keychainSearchOrder("") {
		if err := cs.removeCertByLabel(ctx, keychain, label); err != nil {
			cs.log.Debug("delete-certificate by label returned error (may be absent)", "id", id, "keychain", keychain, "error", err)
		}
	}

	cs.removeIndex(id)
	cs.log.Info("CA certificate removed", "id", id)
	return nil
}

// HasCA reports whether a CA certificate with the given id is currently
// present in a trusted keychain.
func (cs *darwinCertStore) HasCA(id string) (bool, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	idx, err := cs.readIndex(id)
	if err != nil {
		return false, nil
	}
	if strings.TrimSpace(idx.Fingerprint) == "" {
		return false, nil
	}

	ctx := context.Background()
	for _, keychain := range keychainSearchOrder(idx.Keychain) {
		has, checkErr := certFingerprintInKeychain(ctx, keychain, idx.Fingerprint)
		if checkErr != nil {
			cs.log.Debug("find-certificate failed", "id", id, "keychain", keychain, "error", checkErr)
			continue
		}
		if has {
			return true, nil
		}
	}
	return false, nil
}

func (cs *darwinCertStore) removeCertByLabel(ctx context.Context, keychain, label string) error {
	return runCmd(ctx, "security", "delete-certificate", "-c", label, keychain)
}

// ---------------------------------------------------------------------------
// cert index — maps our id -> fingerprint + keychain
// ---------------------------------------------------------------------------

func (cs *darwinCertStore) writeIndex(id string, cert *x509.Certificate, keychain string) error {
	dir := certStoreDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	entry := certIndex{
		Fingerprint: sha256Hex(cert.Raw),
		Keychain:    strings.TrimSpace(keychain),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return os.WriteFile(indexPath(id), data, 0600)
}

func (cs *darwinCertStore) readIndex(id string) (certIndex, error) {
	data, err := os.ReadFile(indexPath(id))
	if err != nil {
		return certIndex{}, err
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return certIndex{}, fmt.Errorf("empty cert index")
	}

	// Backward compatibility: legacy index files contained only the fingerprint.
	if !strings.HasPrefix(raw, "{") {
		return certIndex{
			Fingerprint: raw,
			Keychain:    systemKeychain,
		}, nil
	}

	var entry certIndex
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return certIndex{}, err
	}
	return entry, nil
}

func (cs *darwinCertStore) removeIndex(id string) {
	_ = os.Remove(indexPath(id))
}

func indexPath(id string) string {
	return filepath.Join(certStoreDir(), id+".fingerprint")
}

func certStoreDir() string {
	if cfgDir, err := os.UserConfigDir(); err == nil && strings.TrimSpace(cfgDir) != "" {
		return filepath.Join(cfgDir, "Themisto", "certs")
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".themisto", "certs")
	}
	return filepath.Join(os.TempDir(), "themisto-certs")
}

func keychainSearchOrder(preferred string) []string {
	var keychains []string
	appendIfMissing := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		for _, existing := range keychains {
			if strings.EqualFold(existing, path) {
				return
			}
		}
		keychains = append(keychains, path)
	}

	appendIfMissing(preferred)
	appendIfMissing(systemKeychain)
	appendIfMissing(userLoginKeychain())
	return keychains
}

func userLoginKeychain() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, loginKeychainRelDir)
}

func installTrustedCert(ctx context.Context, keychain, certPath string, adminStore bool) error {
	if err := runCmd(ctx, "security", "add-certificates", "-k", keychain, certPath); err != nil {
		// add-certificates can fail for duplicates; trust command is authoritative.
	}

	args := []string{"add-trusted-cert", "-r", "trustRoot", "-k", keychain, certPath}
	if adminStore {
		args = []string{"add-trusted-cert", "-d", "-r", "trustRoot", "-k", keychain, certPath}
	}
	return runCmd(ctx, "security", args...)
}

func certFingerprintInKeychain(ctx context.Context, keychain, fingerprint string) (bool, error) {
	out, err := exec.CommandContext(ctx, "security", "find-certificate", "-Z", "-a", keychain).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("find-certificate %s: %s: %w", keychain, strings.TrimSpace(string(out)), err)
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(strings.TrimSpace(fingerprint))), nil
}

func certTrustedInKeychain(ctx context.Context, keychain, certPath string) bool {
	out, err := exec.CommandContext(ctx, "security", "verify-cert", "-c", certPath, "-k", keychain).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "successful")
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func writeTempDER(der []byte, id string) (string, error) {
	f, err := os.CreateTemp("", "themisto-cert-"+id+"-*.der")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(der); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}
