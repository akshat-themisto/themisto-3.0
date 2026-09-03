package ailedger

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

type CredentialCipher struct {
	aead cipher.AEAD
}

func NewCredentialCipher(key []byte) (*CredentialCipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("AI Ledger credential key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &CredentialCipher{aead: aead}, nil
}

func NewCredentialCipherFromEnv() (*CredentialCipher, error) {
	raw := strings.TrimSpace(os.Getenv("AI_LEDGER_CREDENTIAL_KEY"))
	if raw == "" {
		return nil, fmt.Errorf("AI_LEDGER_CREDENTIAL_KEY is not configured")
	}
	for _, decode := range []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		hex.DecodeString,
	} {
		if key, err := decode(raw); err == nil && len(key) == 32 {
			return NewCredentialCipher(key)
		}
	}
	if len(raw) == 32 {
		return NewCredentialCipher([]byte(raw))
	}
	return nil, fmt.Errorf("AI_LEDGER_CREDENTIAL_KEY must decode to 32 bytes")
}

func (c *CredentialCipher) Seal(plaintext, associatedData []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, fmt.Errorf("credential encryption is unavailable")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plaintext, associatedData), nil
}

func (c *CredentialCipher) Open(ciphertext, associatedData []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, fmt.Errorf("credential encryption is unavailable")
	}
	if len(ciphertext) < c.aead.NonceSize() {
		return nil, fmt.Errorf("encrypted credentials are truncated")
	}
	nonce := ciphertext[:c.aead.NonceSize()]
	return c.aead.Open(nil, nonce, ciphertext[c.aead.NonceSize():], associatedData)
}
