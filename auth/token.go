package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is what an access token asserts about its bearer. UserID is the JWT
// subject and stays a string here: the token layer does not care that it is a
// UUID, and the middleware parses it exactly once.
type Claims struct {
	UserID string
	Email  string
	Role   string
}

// ErrInvalidToken wraps every parse failure: bad signature, wrong algorithm,
// expired, malformed, missing subject.
var ErrInvalidToken = errors.New("invalid token")

// SignAccessToken issues an HS256 JWT carrying c that expires after ttl.
func SignAccessToken(c Claims, secret string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":   c.UserID,
		"email": c.Email,
		"role":  c.Role,
		"iat":   now.Unix(),
		"exp":   now.Add(ttl).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// ParseAccessToken verifies signature and expiry and returns the claims.
func ParseAccessToken(token, secret string) (Claims, error) {
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil || !parsed.Valid {
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	mc, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return Claims{}, fmt.Errorf("%w: unexpected claims type", ErrInvalidToken)
	}
	sub, _ := mc.GetSubject()
	if sub == "" {
		return Claims{}, fmt.Errorf("%w: missing subject", ErrInvalidToken)
	}
	email, _ := mc["email"].(string)
	role, _ := mc["role"].(string)
	return Claims{UserID: sub, Email: email, Role: role}, nil
}
