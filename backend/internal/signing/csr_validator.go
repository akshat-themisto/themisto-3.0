package signing

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

type CSRValidationError struct {
	Code    string
	Message string
}

func (e *CSRValidationError) Error() string { return e.Message }

func ValidateCSR(csrPEM []byte, expectedCN, expectedO string) ([]byte, error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, &CSRValidationError{Code: "CSR_INVALID", Message: "no valid PEM CERTIFICATE REQUEST block found"}
	}

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, &CSRValidationError{Code: "CSR_INVALID", Message: fmt.Sprintf("parse CSR: %v", err)}
	}

	if err := csr.CheckSignature(); err != nil {
		return nil, &CSRValidationError{Code: "CSR_INVALID", Message: "CSR signature verification failed"}
	}

	if csr.Subject.CommonName != expectedCN {
		return nil, &CSRValidationError{
			Code:    "CSR_INVALID",
			Message: fmt.Sprintf("CN mismatch: got %q, expected %q", csr.Subject.CommonName, expectedCN),
		}
	}

	if len(csr.Subject.Organization) == 0 || csr.Subject.Organization[0] != expectedO {
		got := ""
		if len(csr.Subject.Organization) > 0 {
			got = csr.Subject.Organization[0]
		}
		return nil, &CSRValidationError{
			Code:    "ORG_MISMATCH",
			Message: fmt.Sprintf("Organization mismatch: got %q, expected %q", got, expectedO),
		}
	}

	ecKey, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, &CSRValidationError{Code: "KEY_ALGORITHM_REJECTED", Message: "only ECDSA keys are accepted"}
	}

	curve := ecKey.Curve
	if curve != elliptic.P256() && curve != elliptic.P384() {
		return nil, &CSRValidationError{Code: "KEY_ALGORITHM_REJECTED", Message: "only P-256 or P-384 curves accepted"}
	}

	return block.Bytes, nil
}
