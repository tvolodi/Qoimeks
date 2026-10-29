package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"strings"
)

// pairingAlphabet has 30 characters: digits 2-9 and letters without I, L, O, U, so no
// look-alikes (0/O, 1/I/L, U/V) can be read wrong.
const pairingAlphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ"

const pairingCodeLen = 8

// DeviceKeyPrefix starts every device key, so the middleware can tell it from a tenant code.
const DeviceKeyPrefix = "qdk_"

// NewPairingCode returns a random code shown as XXXX-XXXX. It draws from crypto/rand with
// rejection sampling, so every character is uniform (no modulo bias).
func NewPairingCode() (string, error) {
	n := len(pairingAlphabet)
	limit := byte(256 / n * n) // 240: bytes at or above it are dropped
	out := make([]byte, 0, pairingCodeLen)
	buf := make([]byte, 16)
	for len(out) < pairingCodeLen {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b >= limit {
				continue
			}
			out = append(out, pairingAlphabet[int(b)%n])
			if len(out) == pairingCodeLen {
				break
			}
		}
	}
	return string(out[:4]) + "-" + string(out[4:]), nil
}

// NormalizePairingCode upper-cases typed input and removes spaces and hyphens. ok is false
// when the result is not a well-formed code (wrong length or a character outside the
// alphabet, look-alikes included). Such input can never match a stored code.
func NormalizePairingCode(s string) (code string, ok bool) {
	s = strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(s))
	if len(s) != pairingCodeLen {
		return s, false
	}
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(pairingAlphabet, s[i]) < 0 {
			return s, false
		}
	}
	return s, true
}

// NewDeviceKey returns "qdk_" plus 32 random bytes in base32 (52 characters).
func NewDeviceKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return DeviceKeyPrefix + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

func hashBytes(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}
