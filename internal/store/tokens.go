package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const tokenBytes = 32

func NewToken() (plain string, hash []byte, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("store: generate token: %w", err)
	}
	plain = base64.RawURLEncoding.EncodeToString(raw)
	return plain, HashToken(plain), nil
}

func HashToken(plain string) []byte {
	sum := sha256.Sum256([]byte(plain))
	return sum[:]
}
