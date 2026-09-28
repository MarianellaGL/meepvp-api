package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("mysecretpassword")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(hash, "$argon2id$v=19$"), "hash should start with argon2id prefix")

	parts := strings.Split(hash, "$")
	assert.Equal(t, 6, len(parts), "hash should have 6 parts separated by $")
}

func TestVerifyPassword_Correct(t *testing.T) {
	password := "correct-horse-battery-staple"
	hash, err := HashPassword(password)
	require.NoError(t, err)

	match, err := VerifyPassword(password, hash)
	require.NoError(t, err)
	assert.True(t, match, "VerifyPassword should return true for the correct password")
}

func TestVerifyPassword_Wrong(t *testing.T) {
	hash, err := HashPassword("realpassword")
	require.NoError(t, err)

	match, err := VerifyPassword("wrongpassword", hash)
	require.NoError(t, err)
	assert.False(t, match, "VerifyPassword should return false for a wrong password")
}

func TestVerifyPassword_InvalidHashFormat(t *testing.T) {
	_, err := VerifyPassword("password", "not-a-valid-hash")
	assert.Error(t, err, "VerifyPassword should return an error for an invalid hash format")
}

func TestValidatePasswordComplexity(t *testing.T) {
	assert.Equal(t, "password must be at least 8 characters", ValidatePasswordComplexity("Ab1"))
	assert.Equal(t, "password must contain at least one uppercase letter", ValidatePasswordComplexity("abcdefg1"))
	assert.Equal(t, "password must contain at least one lowercase letter", ValidatePasswordComplexity("ABCDEFG1"))
	assert.Equal(t, "password must contain at least one number", ValidatePasswordComplexity("Abcdefgh"))
	assert.Equal(t, "", ValidatePasswordComplexity("Abcdefg1"))
}
