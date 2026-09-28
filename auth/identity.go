package auth

import (
	"encoding/hex"
	"fmt"
	"github.com/google/uuid"
)

// ParseUserID accepts existing Tablescore keys and UUIDs created by Foundation.
func ParseUserID(raw string) (string, error) {
	if id, err := uuid.Parse(raw); err == nil {
		return id.String(), nil
	}
	if len(raw) == 24 {
		if _, err := hex.DecodeString(raw); err == nil {
			return raw, nil
		}
	}
	return "", fmt.Errorf("invalid user ID")
}
