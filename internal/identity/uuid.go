package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

type UUID [16]byte

var (
	offlinePassportNamespaceSeed = []byte("authman:offline-passport-uuid:v1")
)

func OfflinePassportUUID(normalizedOfflineName string) UUID {
	return offlineNamespacedUUID(offlinePassportNamespaceSeed, "passport", normalizedOfflineName)
}

func RandomProfileUUID() (UUID, error) {
	var uuid UUID
	if _, err := rand.Read(uuid[:]); err != nil {
		return UUID{}, err
	}
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return uuid, nil
}

func offlineNamespacedUUID(seed []byte, marker string, value string) UUID {
	input := strings.ToLower(strings.TrimSpace(value))
	hash := sha256.New()
	hash.Write(seed)
	hash.Write([]byte{0})
	hash.Write([]byte(marker))
	hash.Write([]byte{0})
	hash.Write([]byte(input))
	sum := hash.Sum(nil)

	var uuid UUID
	copy(uuid[:], sum[:16])
	uuid[6] = (uuid[6] & 0x0f) | 0x80
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return uuid
}

func ParseUUID(value string) (UUID, error) {
	compact := strings.ReplaceAll(strings.TrimSpace(value), "-", "")
	if len(compact) != 32 {
		return UUID{}, fmt.Errorf("uuid must contain 32 hex characters")
	}
	bytes, err := hex.DecodeString(compact)
	if err != nil {
		return UUID{}, err
	}
	var uuid UUID
	copy(uuid[:], bytes)
	return uuid, nil
}

func (u UUID) String() string {
	hexed := hex.EncodeToString(u[:])
	return hexed[0:8] + "-" +
		hexed[8:12] + "-" +
		hexed[12:16] + "-" +
		hexed[16:20] + "-" +
		hexed[20:32]
}

func (u UUID) Compact() string {
	return hex.EncodeToString(u[:])
}
