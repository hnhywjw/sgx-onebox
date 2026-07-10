package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

func init() {
	if os.Getenv("PLATFORM_SECRET") == "" && os.Getenv("GO_TEST_MODE") == "" {
		log.Println("[WARNING] PLATFORM_SECRET environment variable is not set. Using default key. Set a strong random key in production.")
	}
}

func platformSecret() []byte {
	secret := os.Getenv("PLATFORM_SECRET")
	if secret == "" {
		if os.Getenv("GO_TEST_MODE") != "" {
			return []byte("test-secret-key-for-unit-tests-only")
		}
		log.Fatal("[SECURITY] PLATFORM_SECRET must be set in production. Refusing to use default key.")
	}
	return []byte(secret)
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt password hashing failed: %w", err)
	}
	return string(hash), nil
}

func VerifyPassword(hash, password string) bool {
	if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	log.Println("[SECURITY] non-bcrypt password hash detected, rejecting authentication")
	return false
}

func GenerateToken(userID string) string {
	randomBytes := make([]byte, 24)
	if _, err := rand.Read(randomBytes); err != nil {
		log.Printf("[SECURITY] crypto/rand.Read failed: %v", err)
		// Fallback: use timestamp as entropy when crypto/rand fails
		for i := range randomBytes {
			randomBytes[i] = byte(time.Now().UnixNano() >> (i % 8))
		}
	}
	createdAt := time.Now().UTC().Unix()
	payload := fmt.Sprintf("%s.%s.%d", hex.EncodeToString(randomBytes), userID, createdAt)
	mac := hmac.New(sha256.New, hmacKey())
	mac.Write([]byte(payload))
	sig := mac.Sum(nil)
	return payload + "." + hex.EncodeToString(sig)
}

func VerifyToken(token string) (string, bool) {
	lastDot := strings.LastIndex(token, ".")
	if lastDot < 0 {
		return "", false
	}
	payload := token[:lastDot]
	payloadParts := strings.SplitN(payload, ".", 3)
	if len(payloadParts) < 2 {
		return "", false
	}
	expectedSig, err := hex.DecodeString(token[lastDot+1:])
	if err != nil {
		return "", false
	}
	mac := hmac.New(sha256.New, hmacKey())
	mac.Write([]byte(payload))
	actualSig := mac.Sum(nil)
	if !hmac.Equal(expectedSig, actualSig) {
		return "", false
	}
	// Check token age (max 14 days)
	if len(payloadParts) == 3 {
		if createdAt, parseErr := strconv.ParseInt(payloadParts[2], 10, 64); parseErr == nil {
			const maxTokenAge = 14 * 24 * time.Hour
			if time.Since(time.Unix(createdAt, 0)) > maxTokenAge {
				return "", false
			}
		}
	}
	return payloadParts[1], true
}

func hmacKey() []byte {
	return deriveKey([]byte("token-hmac"))
}

func encryptKey() []byte {
	return deriveKey([]byte("data-encrypt"))
}

func deriveKey(purpose []byte) []byte {
	h := sha256.New()
	h.Write(platformSecret())
	h.Write(purpose)
	sum := h.Sum(nil)
	return sum[:]
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func EncryptString(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.New("cannot encrypt empty value")
	}
	key := encryptKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(value), nil)
	payload := append(nonce, sealed...)
	return base64.StdEncoding.EncodeToString(payload), nil
}

func DecryptString(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	payload, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	key := encryptKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce := payload[:gcm.NonceSize()]
	ciphertext := payload[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
