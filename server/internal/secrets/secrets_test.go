package secrets

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func testKey(t *testing.T, fill byte) Key {
	t.Helper()
	k, err := ParseKey(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealOpen(t *testing.T) {
	k := testKey(t, 1)
	blob, err := k.Seal("llm", []byte("sk-secret-value"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte("sk-secret-value")) {
		t.Fatal("blob holds plaintext")
	}
	got, err := k.Open("llm", blob)
	if err != nil || string(got) != "sk-secret-value" {
		t.Fatalf("open = %q, %v", got, err)
	}
	blob2, _ := k.Seal("llm", []byte("sk-secret-value"))
	if bytes.Equal(blob, blob2) {
		t.Fatal("nonce reused")
	}
}

func TestOpenFailures(t *testing.T) {
	k := testKey(t, 1)
	blob, _ := k.Seal("llm", []byte("value"))
	tampered := append([]byte(nil), blob...)
	tampered[len(tampered)-1] ^= 1
	for name, c := range map[string]struct {
		key  Key
		aad  string
		blob []byte
	}{
		"wrong key":   {testKey(t, 2), "llm", blob},
		"wrong aad":   {k, "stt", blob},
		"tampered":    {k, "llm", tampered},
		"short":       {k, "llm", blob[:5]},
		"empty":       {k, "llm", nil},
		"zero key":    {Key{}, "llm", blob},
		"bad version": {k, "llm", append([]byte{9}, blob[1:]...)},
	} {
		if _, err := c.key.Open(c.aad, c.blob); !errors.Is(err, ErrDecrypt) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := (Key{}).Seal("llm", []byte("x")); err == nil {
		t.Error("zero key sealed")
	}
}

func TestParseKey(t *testing.T) {
	raw := bytes.Repeat([]byte{0xfb}, 32)
	for name, s := range map[string]string{
		"std":    base64.StdEncoding.EncodeToString(raw),
		"raw":    base64.RawStdEncoding.EncodeToString(raw),
		"url":    base64.URLEncoding.EncodeToString(raw),
		"rawurl": base64.RawURLEncoding.EncodeToString(raw),
		"spaces": " " + base64.StdEncoding.EncodeToString(raw) + "\n",
	} {
		if k, err := ParseKey(s); err != nil || !k.Valid() {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, n := range []int{0, 31, 33} {
		if _, err := ParseKey(base64.StdEncoding.EncodeToString(make([]byte, n))); err == nil {
			t.Errorf("%d bytes accepted", n)
		}
	}
	if _, err := ParseKey("not base64!"); err == nil {
		t.Error("garbage accepted")
	}
}
