package security

import (
	"os"
	"testing"
)

func init() {
	os.Setenv("GO_TEST_MODE", "1")
}

func TestHashPassword(t *testing.T) {
	pw := "test-password-123"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if hash == pw {
		t.Fatal("hash should not equal plaintext password")
	}
	if len(hash) < 20 {
		t.Fatalf("hash too short: %d", len(hash))
	}
}

func TestVerifyPasswordBcrypt(t *testing.T) {
	pw := "test-bcrypt-456"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if !VerifyPassword(hash, pw) {
		t.Fatal("bcrypt verify should succeed")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Fatal("bcrypt verify should fail for wrong password")
	}
}

func TestVerifyPasswordLegacySHA256(t *testing.T) {
	hash := "8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92"
	if VerifyPassword(hash, "123456") {
		t.Fatal("legacy SHA-256 verify should fail since fallback was removed")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("legacy SHA-256 verify should fail for wrong password")
	}
}

func TestEncryptDecryptString(t *testing.T) {
	ciphertext, err := EncryptString("ssh-password-123")
	if err != nil {
		t.Fatalf("EncryptString failed: %v", err)
	}
	if ciphertext == "" || ciphertext == "ssh-password-123" {
		t.Fatal("expected encrypted ciphertext")
	}
	plain, err := DecryptString(ciphertext)
	if err != nil {
		t.Fatalf("DecryptString failed: %v", err)
	}
	if plain != "ssh-password-123" {
		t.Fatalf("unexpected plaintext: %q", plain)
	}
}

func TestGenerateToken(t *testing.T) {
	token := GenerateToken("test-user")
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	parts := 0
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			parts++
		}
	}
	if parts != 3 {
		t.Fatalf("token should have exactly three dot separators, got %d", parts)
	}
}

func TestVerifyToken(t *testing.T) {
	valid := GenerateToken("test-user")
	uid, ok := VerifyToken(valid)
	if !ok {
		t.Fatal("VerifyToken should succeed for valid token")
	}
	if uid != "test-user" {
		t.Fatalf("VerifyToken should return correct userID, got %q", uid)
	}
	if _, ok := VerifyToken("invalid-token"); ok {
		t.Fatal("VerifyToken should fail for invalid token")
	}
	if _, ok := VerifyToken(""); ok {
		t.Fatal("VerifyToken should fail for empty token")
	}
	if _, ok := VerifyToken("abc.def"); ok {
		t.Fatal("VerifyToken should fail for non-hex token")
	}
}

func TestVerifyTokenTampered(t *testing.T) {
	token := GenerateToken("test-user")
	tampered := token + "0"
	if _, ok := VerifyToken(tampered); ok {
		t.Fatal("VerifyToken should detect tampered token")
	}
}

func TestTokenUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		token := GenerateToken("test-user")
		if seen[token] {
			t.Fatal("duplicate token generated")
		}
		seen[token] = true
	}
}
