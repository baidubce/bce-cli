package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// PKCE holds one authorization request's PKCE material.
// The verifier stays in process memory only: it must never be logged or
// placed in a browser URL.
type PKCE struct {
	Verifier  string
	Challenge string
}

// NewPKCE generates a code_verifier and its S256 code_challenge.
// The verifier is 43 base64url characters, well within the 43-128 range
// allowed by RFC 7636, and its character set is a subset of the permitted
// [A-Za-z0-9-._~].
func NewPKCE() (*PKCE, error) {
	verifier, err := randomBase64URL(32)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(verifier))
	return &PKCE{
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]),
	}, nil
}

// NewStateValue returns a 43-character Base64URL random value suitable for
// the `state` and `nonce` parameters, both of which must be 43-512 chars.
func NewStateValue() (string, error) {
	return randomBase64URL(32)
}

func randomBase64URL(numBytes int) (string, error) {
	buf := make([]byte, numBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
