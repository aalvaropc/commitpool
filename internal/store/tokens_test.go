package store_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/aalvaropc/commitpool/internal/store"
)

func TestTokens_HashRoundTrip(t *testing.T) {
	plain, hash, err := store.NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if len(plain) != 43 {
		t.Errorf("len(plain) = %d, want 43 (32 bytes, unpadded base64url)", len(plain))
	}
	raw, err := base64.RawURLEncoding.DecodeString(plain)
	if err != nil || len(raw) != 32 {
		t.Errorf("plain decodes to %d bytes (err %v), want 32", len(raw), err)
	}

	want := sha256.Sum256([]byte(plain))
	if !bytes.Equal(hash, want[:]) {
		t.Errorf("hash is not sha256(plain)")
	}
	if got := store.HashToken(plain); !bytes.Equal(got, hash) {
		t.Errorf("HashToken(plain) differs from the hash NewToken returned")
	}
	if bytes.Equal(store.HashToken(plain+"x"), hash) {
		t.Errorf("HashToken ignores its input")
	}

	other, otherHash, err := store.NewToken()
	if err != nil {
		t.Fatalf("second NewToken: %v", err)
	}
	if other == plain {
		t.Errorf("two NewToken calls returned the same plaintext")
	}
	if bytes.Equal(otherHash, hash) {
		t.Errorf("two NewToken calls returned the same hash")
	}
}
