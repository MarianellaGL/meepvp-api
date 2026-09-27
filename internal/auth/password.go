package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"

	"crypto/pbkdf2"
)

const iterations = 600000

var ErrInvalidPassword = errors.New("password must be 12 to 1024 bytes")

// Hash uses PBKDF2-HMAC-SHA256 with a per-user random salt.
func Hash(password string) (string, error) {
	if len(password) < 12 || len(password) > 1024 {
		return "", ErrInvalidPassword
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return "", err
	}
	return "pbkdf2_sha256$" + strconv.Itoa(iterations) + "$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}

func Verify(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" || len(password) > 1024 {
		return false
	}
	count, err := strconv.Atoi(parts[1])
	if err != nil || count < iterations || count > 2000000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) != 16 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) != 32 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, count, 32)
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}
