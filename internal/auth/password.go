package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

type PasswordPolicy struct {
	MinLength int
	MaxLength int
}

type Argon2idParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  int
	KeyLength   uint32
}

var DefaultPasswordPolicy = PasswordPolicy{
	MinLength: 8,
	MaxLength: 128,
}

var DefaultArgon2idParams = Argon2idParams{
	MemoryKiB:   64 * 1024,
	Iterations:  3,
	Parallelism: 1,
	SaltLength:  16,
	KeyLength:   32,
}

func ValidatePassword(password string, policy PasswordPolicy) error {
	if policy.MinLength == 0 {
		policy.MinLength = DefaultPasswordPolicy.MinLength
	}
	if policy.MaxLength == 0 {
		policy.MaxLength = DefaultPasswordPolicy.MaxLength
	}
	if len(password) < policy.MinLength {
		return fmt.Errorf("password is too short")
	}
	if len(password) > policy.MaxLength {
		return fmt.Errorf("password is too long")
	}
	return nil
}

func HashPassword(password string, params Argon2idParams) (string, error) {
	if params.MemoryKiB == 0 {
		params = DefaultArgon2idParams
	}
	if err := ValidatePassword(password, DefaultPasswordPolicy); err != nil {
		return "", err
	}
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, params.Iterations, params.MemoryKiB, params.Parallelism, params.KeyLength)
	encodedSalt := base64.RawStdEncoding.EncodeToString(salt)
	encodedHash := base64.RawStdEncoding.EncodeToString(hash)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", params.MemoryKiB, params.Iterations, params.Parallelism, encodedSalt, encodedHash), nil
}

func VerifyPassword(password string, encoded string) (bool, error) {
	params, salt, expected, err := parseArgon2id(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey([]byte(password), salt, params.Iterations, params.MemoryKiB, params.Parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func parseArgon2id(encoded string) (Argon2idParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return Argon2idParams{}, nil, nil, fmt.Errorf("invalid argon2id hash format")
	}
	paramParts := strings.Split(parts[3], ",")
	if len(paramParts) != 3 {
		return Argon2idParams{}, nil, nil, fmt.Errorf("invalid argon2id parameter format")
	}
	params := Argon2idParams{}
	for _, part := range paramParts {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return Argon2idParams{}, nil, nil, fmt.Errorf("invalid argon2id parameter")
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return Argon2idParams{}, nil, nil, err
		}
		switch key {
		case "m":
			params.MemoryKiB = uint32(n)
		case "t":
			params.Iterations = uint32(n)
		case "p":
			params.Parallelism = uint8(n)
		default:
			return Argon2idParams{}, nil, nil, fmt.Errorf("unknown argon2id parameter %q", key)
		}
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Argon2idParams{}, nil, nil, err
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Argon2idParams{}, nil, nil, err
	}
	params.SaltLength = len(salt)
	params.KeyLength = uint32(len(hash))
	return params, salt, hash, nil
}
