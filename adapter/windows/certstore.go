//go:build windows

package windows

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
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
	certLabelPrefix = "Themisto-CA-"
	certIndexDir    = `C:\ProgramData\Themisto\certs`
)

// winCertStore implements iface.CertStore using certutil CLI and the Local
// Machine Root store.
type winCertStore struct {
	mu  sync.Mutex
	log log.Logger
}

func newCertStore(logger log.Logger) *winCertStore {
	return &winCertStore{log: logger}
}

// InstallCA adds a DER-encoded CA certificate to Cert:\LocalMachine\Root.
func (cs *winCertStore) InstallCA(ctx context.Context, id string, der []byte) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return fmt.Errorf("parse certificate: %w: %w", err, iface.ErrInvalidCert)
	}

	fingerprint := sha256Hex(cert.Raw)

	// Check if already installed (idempotent).
	if cs.certExistsByFingerprint(ctx, fingerprint) {
		return nil
	}

	tmpFile, err := writeTempDER(der, id)
	if err != nil {
		return fmt.Errorf("write temp cert: %w", err)
	}
	defer os.Remove(tmpFile)

	// certutil -addstore -f "Root" <file>
	if err := runCmd(ctx, "certutil", "-addstore", "-f", "Root", tmpFile); err != nil {
		return fmt.Errorf("certutil addstore: %w: %w", err, iface.ErrPermission)
	}

	if err := cs.writeIndex(id, fingerprint); err != nil {
		cs.log.Warn("failed to write cert index", "id", id, "error", err)
	}

	cs.log.Info("CA certificate installed", "id", id, "subject", cert.Subject.CommonName)
	return nil
}

// RemoveCA removes a previously installed CA certificate by id.
func (cs *winCertStore) RemoveCA(ctx context.Context, id string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	fingerprint, err := cs.readIndex(id)
	if err != nil {
		cs.log.Debug("cert index not found, skipping removal (idempotent)", "id", id)
		return nil
	}

	// certutil -delstore "Root" <sha256-hash>
	// certutil uses the SHA-1 hash for -delstore, so we use PowerShell
	// as a more reliable option.
	_ = runCmd(ctx, "powershell", "-NoProfile", "-Command",
		fmt.Sprintf(
			`Get-ChildItem Cert:\LocalMachine\Root | Where-Object { $_.Thumbprint -eq '%s' } | Remove-Item -Force`,
			fingerprint,
		),
	)

	// Fallback: try certutil with the serial number.
	_ = runCmd(ctx, "certutil", "-delstore", "Root", fingerprint)

	cs.removeIndex(id)
	cs.log.Info("CA certificate removed", "id", id)
	return nil
}

// HasCA reports whether a certificate with the given id is installed.
func (cs *winCertStore) HasCA(id string) (bool, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	fingerprint, err := cs.readIndex(id)
	if err != nil {
		return false, nil
	}

	return cs.certExistsByFingerprint(context.Background(), fingerprint), nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (cs *winCertStore) certExistsByFingerprint(ctx context.Context, fp string) bool {
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
		fmt.Sprintf(
			`(Get-ChildItem Cert:\LocalMachine\Root | Where-Object { $_.Thumbprint -eq '%s' }).Count`,
			fp,
		),
	).Output()
	if err != nil {
		return false
	}
	count := strings.TrimSpace(string(out))
	return count != "" && count != "0"
}

// ---------------------------------------------------------------------------
// cert index — maps id → fingerprint on disk
// ---------------------------------------------------------------------------

func (cs *winCertStore) writeIndex(id, fingerprint string) error {
	if err := os.MkdirAll(certIndexDir, 0700); err != nil {
		return err
	}
	return os.WriteFile(indexPath(id), []byte(fingerprint), 0600)
}

func (cs *winCertStore) readIndex(id string) (string, error) {
	data, err := os.ReadFile(indexPath(id))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (cs *winCertStore) removeIndex(id string) {
	_ = os.Remove(indexPath(id))
}

func indexPath(id string) string {
	return filepath.Join(certIndexDir, id+".fingerprint")
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return strings.ToUpper(hex.EncodeToString(h[:]))
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
