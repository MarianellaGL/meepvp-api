package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret-key"

func TestSignAndParseAccessToken_RoundTrip(t *testing.T) {
	in := Claims{UserID: "user-456", Email: "valid@example.com", Role: RoleAdmin}
	tok, err := SignAccessToken(in, testSecret, time.Hour)
	require.NoError(t, err)

	out, err := ParseAccessToken(tok, testSecret)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestParseAccessToken_Expired(t *testing.T) {
	tok, err := SignAccessToken(Claims{UserID: "u"}, testSecret, -time.Minute)
	require.NoError(t, err)
	_, err = ParseAccessToken(tok, testSecret)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestParseAccessToken_WrongSecret(t *testing.T) {
	tok, err := SignAccessToken(Claims{UserID: "u"}, testSecret, time.Hour)
	require.NoError(t, err)
	_, err = ParseAccessToken(tok, "other-secret")
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestParseAccessToken_MissingSubject(t *testing.T) {
	tok, err := SignAccessToken(Claims{Email: "x@example.com"}, testSecret, time.Hour)
	require.NoError(t, err)
	_, err = ParseAccessToken(tok, testSecret)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestParseAccessToken_Garbage(t *testing.T) {
	_, err := ParseAccessToken("invalid.token.here", testSecret)
	assert.ErrorIs(t, err, ErrInvalidToken)
}
