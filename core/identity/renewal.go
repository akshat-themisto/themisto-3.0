package identity

import (
	"crypto/x509"
	"encoding/pem"
	"time"
)

// RenewalCheck holds the result of checking whether the current cert needs renewal.
type RenewalCheck struct {
	NeedsRenewal bool
	ExpiresAt    time.Time
	DaysLeft     int
}

// CheckRenewal inspects the current certificate and determines if renewal is needed.
// threshold is how many days before expiry to trigger renewal (default 14).
func (s *IdentityStore) CheckRenewal(threshold int) RenewalCheck {
	if threshold <= 0 {
		threshold = 14
	}

	s.mu.RLock()
	creds := s.creds
	s.mu.RUnlock()

	if creds == nil || len(creds.certDER) == 0 {
		return RenewalCheck{NeedsRenewal: false}
	}

	cert, err := x509.ParseCertificate(creds.certDER)
	if err != nil {
		return RenewalCheck{NeedsRenewal: false}
	}

	daysLeft := int(time.Until(cert.NotAfter).Hours() / 24)
	return RenewalCheck{
		NeedsRenewal: daysLeft <= threshold,
		ExpiresAt:    cert.NotAfter,
		DaysLeft:     daysLeft,
	}
}

// GetCertExpiry returns the NotAfter time of the current certificate, if loaded.
func (s *IdentityStore) GetCertExpiry() (time.Time, bool) {
	s.mu.RLock()
	creds := s.creds
	s.mu.RUnlock()

	if creds == nil || len(creds.certDER) == 0 {
		return time.Time{}, false
	}

	cert, err := x509.ParseCertificate(creds.certDER)
	if err != nil {
		return time.Time{}, false
	}

	return cert.NotAfter, true
}

// BootstrapFromPEM loads credentials from PEM-encoded cert, key, and CA chain.
// Returns the parsed certificate's serial as a hex string.
func (s *IdentityStore) BootstrapFromPEM(caID string, certPEM, keyPEM, caChainPEM []byte) (string, error) {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return "", &RenewalError{Message: "invalid certificate PEM"}
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return "", &RenewalError{Message: "invalid key PEM"}
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return "", &RenewalError{Message: "parse cert: " + err.Error()}
	}

	clientID := cert.Subject.CommonName
	s.Bootstrap(caID, clientID, certBlock.Bytes, keyBlock.Bytes, caChainPEM)

	return cert.SerialNumber.Text(16), nil
}

// RenewalError represents a certificate renewal error.
type RenewalError struct {
	Message string
}

func (e *RenewalError) Error() string { return "renewal: " + e.Message }
