package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
)

type bodyCipher struct {
	aead cipher.AEAD
}

func newBodyCipherFromEnv() (*bodyCipher, error) {
	secret := strings.TrimSpace(os.Getenv("DLP_BODY_ENCRYPTION_KEY"))
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("INTERNAL_TOKEN"))
	}
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("ADMIN_API_KEY"))
	}
	if secret == "" {
		return nil, nil
	}

	sum := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("new AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new GCM: %w", err)
	}
	return &bodyCipher{aead: aead}, nil
}

func (c *bodyCipher) Decrypt(ciphertext, nonce []byte) (string, error) {
	if len(ciphertext) == 0 {
		return "", nil
	}

	if c == nil {
		if len(nonce) == 0 {
			// Backward compatibility: legacy plaintext rows (no nonce) can still be read.
			return string(ciphertext), nil
		}
		return "", fmt.Errorf("dlp body decryption key not configured")
	}

	if len(nonce) == 0 {
		// Backward/compat path for plaintext rows written before key configuration.
		return string(ciphertext), nil
	}
	if len(nonce) != c.aead.NonceSize() {
		return "", fmt.Errorf("invalid nonce size")
	}
	plain, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
