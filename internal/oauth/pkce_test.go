package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestNewPKCEChallengeMatchesVerifier(t *testing.T) {
	p, err := NewPKCE()
	if err != nil {
		t.Fatalf("NewPKCE: %v", err)
	}
	if len(p.Verifier) < 43 || len(p.Verifier) > 128 {
		t.Errorf("verifier length %d outside RFC 7636 range 43-128", len(p.Verifier))
	}
	const allowed = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	if strings.ContainsFunc(p.Verifier, func(r rune) bool { return !strings.ContainsRune(allowed, r) }) {
		t.Errorf("verifier contains characters outside the allowed set: %q", p.Verifier)
	}
	sum := sha256.Sum256([]byte(p.Verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if p.Challenge != want {
		t.Errorf("challenge = %q, want S256 of verifier %q", p.Challenge, want)
	}
	if strings.ContainsAny(p.Challenge, "=+/") {
		t.Errorf("challenge must be base64url without padding: %q", p.Challenge)
	}
}

func TestNewPKCEIsRandom(t *testing.T) {
	a, err := NewPKCE()
	if err != nil {
		t.Fatalf("NewPKCE: %v", err)
	}
	b, err := NewPKCE()
	if err != nil {
		t.Fatalf("NewPKCE: %v", err)
	}
	if a.Verifier == b.Verifier {
		t.Error("two verifiers are identical; generator is not random")
	}
}

func TestNewStateValueLength(t *testing.T) {
	s, err := NewStateValue()
	if err != nil {
		t.Fatalf("NewStateValue: %v", err)
	}
	if len(s) < 43 || len(s) > 512 {
		t.Errorf("state length %d outside required range 43-512", len(s))
	}
}
