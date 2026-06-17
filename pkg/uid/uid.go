// Package uid provides lightweight UUID v4 generation with no external dependencies.
package uid

import (
	"crypto/rand"
	"fmt"
)

// New returns a random UUID v4 string (e.g. "550e8400-e29b-41d4-a716-446655440000").
// Panics if the system CSPRNG is unavailable.
func New() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic("uid: crypto/rand unavailable: " + err.Error())
	}
	buf[6] = (buf[6] & 0x0f) | 0x40 // version 4
	buf[8] = (buf[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}
