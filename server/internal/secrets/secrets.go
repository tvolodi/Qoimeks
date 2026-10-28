// Package secrets encrypts small secrets (provider API keys) for storage in the database.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
)

const (
	keyLen  = 32
	version = 0x01
)

// ErrDecrypt is the only error Open returns: it never says why, so nothing about the
// key or the data can leak through it.
var ErrDecrypt = errors.New("cannot decrypt")

// Key is an AES-256 key. The zero Key is "no key": Valid reports false.
type Key struct{ b []byte }

// ParseKey reads the base64 text (standard or URL alphabet, padding optional) of
// exactly 32 bytes, as made by `openssl rand -base64 32`.
func ParseKey(s string) (Key, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	b, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil || len(b) != keyLen {
		return Key{}, errors.New("expected base64 of 32 bytes")
	}
	return Key{b: b}, nil
}

func (k Key) Valid() bool { return len(k.b) == keyLen }

func (k Key) gcm() (cipher.AEAD, error) {
	blk, err := aes.NewCipher(k.b)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}

// Seal returns version || nonce(12) || ciphertext+tag. aad binds the blob to its use.
func (k Key) Seal(aad string, plaintext []byte) ([]byte, error) {
	if !k.Valid() {
		return nil, errors.New("no key")
	}
	g, err := k.gcm()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{version}, nonce...)
	return g.Seal(out, nonce, plaintext, []byte(aad)), nil
}

// Open reverses Seal. Any failure is ErrDecrypt.
func (k Key) Open(aad string, blob []byte) ([]byte, error) {
	if !k.Valid() {
		return nil, ErrDecrypt
	}
	g, err := k.gcm()
	if err != nil || len(blob) < 1+g.NonceSize()+g.Overhead() || blob[0] != version {
		return nil, ErrDecrypt
	}
	n := 1 + g.NonceSize()
	pt, err := g.Open(nil, blob[1:n], blob[n:], []byte(aad))
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}
