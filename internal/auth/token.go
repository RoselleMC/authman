package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

func NewOpaqueToken(bytes int) (string, error) {
	if bytes < 16 {
		return "", fmt.Errorf("token must contain at least 16 random bytes")
	}
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func HashToken(scope string, token string) string {
	sum := sha256.Sum256([]byte(scope + "\x00" + token))
	return hex.EncodeToString(sum[:])
}

func TokenFingerprint(token string) string {
	hash := HashToken("fingerprint", token)
	return hash[:12]
}

func ConstantTimeTokenEqual(scope string, token string, expectedHash string) bool {
	actual := HashToken(scope, token)
	return subtle.ConstantTimeCompare([]byte(actual), []byte(strings.ToLower(expectedHash))) == 1
}
