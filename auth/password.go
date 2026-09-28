package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argon2Memory      = 65536
	argon2Iterations  = 3
	argon2Parallelism = 2
	argon2SaltLength  = 16
	argon2KeyLength   = 32
)

var (
	hasUppercase = regexp.MustCompile(`[A-Z]`)
	hasLowercase = regexp.MustCompile(`[a-z]`)
	hasDigit     = regexp.MustCompile(`[0-9]`)
)

// ValidatePasswordComplexity returns "" when the password is acceptable, or a
// user-facing reason when it is not.
func ValidatePasswordComplexity(password string) string {
	if len(password) < 8 {
		return "password must be at least 8 characters"
	}
	if !hasUppercase.MatchString(password) {
		return "password must contain at least one uppercase letter"
	}
	if !hasLowercase.MatchString(password) {
		return "password must contain at least one lowercase letter"
	}
	if !hasDigit.MatchString(password) {
		return "password must contain at least one number"
	}
	return ""
}

// HashPassword hashes a password with argon2id and returns the PHC-style string.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argon2Iterations, argon2Memory, argon2Parallelism, argon2KeyLength)

	saltB64 := base64.RawStdEncoding.EncodeToString(salt)
	hashB64 := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argon2Memory, argon2Iterations, argon2Parallelism, saltB64, hashB64), nil
}

// VerifyPassword checks a password against an argon2id hash string.
func VerifyPassword(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, errors.New("invalid hash format")
	}

	var memory uint32
	var iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, fmt.Errorf("failed to parse hash parameters: %w", err)
	}

	if memory < 8*uint32(parallelism) || memory > 65536 || iterations < 1 || iterations > 3 || parallelism < 1 || parallelism > 2 {
		return false, errors.New("invalid argon2 parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != argon2SaltLength {
		return false, errors.New("invalid salt")
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expectedHash) != argon2KeyLength {
		return false, errors.New("invalid hash")
	}

	computedHash := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expectedHash)))

	return subtle.ConstantTimeCompare(computedHash, expectedHash) == 1, nil
}
