package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

func Generate() (raw string, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate random bytes: %w", err)
	}

	raw = base64.URLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(raw))
	hash = base64.URLEncoding.EncodeToString(h[:])
	return raw, hash, nil
}

func Hash(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return base64.URLEncoding.EncodeToString(h[:])
}
