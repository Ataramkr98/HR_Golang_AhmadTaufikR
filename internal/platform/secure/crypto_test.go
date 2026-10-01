package secure

import (
	"strings"
	"testing"
	"time"
)

func TestCipherRoundTripAndNonceRandomness(t *testing.T) {
	cipher, err := NewCipher([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := cipher.Encrypt("3173010101010001")
	if err != nil {
		t.Fatal(err)
	}
	second, err := cipher.Encrypt("3173010101010001")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("AES-GCM ciphertext must use a fresh nonce")
	}
	plain, err := cipher.Decrypt(first)
	if err != nil || plain != "3173010101010001" {
		t.Fatalf("round trip failed: plain=%q err=%v", plain, err)
	}
}

func TestTokenTypeAndExpiryAreEnforced(t *testing.T) {
	manager := NewTokenManager(strings.Repeat("secret", 8), time.Minute)
	raw, err := manager.Issue(Claims{UserID: "user", TokenType: "access"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := manager.Parse(raw, "access")
	if err != nil || claims.UserID != "user" {
		t.Fatalf("valid access token rejected: %v", err)
	}
	if _, err := manager.Parse(raw, "realtime"); err == nil {
		t.Fatal("token must not be reusable for another purpose")
	}
}
