package mailer

import (
	"testing"
	"time"

	"tablescore-api/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSMTP_AcceptsEveryTLSMode(t *testing.T) {
	for _, mode := range []string{config.SMTPTLSStartTLS, config.SMTPTLSImplicit, config.SMTPTLSNone} {
		m, err := NewSMTP("smtp.example.com", 587, "user", "pass", "App <no-reply@example.com>", mode, time.Second)
		require.NoError(t, err, mode)
		assert.NotNil(t, m.client, mode)
		assert.Equal(t, "App <no-reply@example.com>", m.from)
	}
}

func TestNewSMTP_RejectsUnknownTLSMode(t *testing.T) {
	_, err := NewSMTP("smtp.example.com", 587, "", "", "no-reply@example.com", "ssl", time.Second)
	assert.Error(t, err)
}

func TestNewSMTP_RejectsEmptyHost(t *testing.T) {
	_, err := NewSMTP("", 587, "", "", "no-reply@example.com", config.SMTPTLSNone, time.Second)
	assert.Error(t, err)
}
