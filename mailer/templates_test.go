package mailer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const link = "https://app.example.com/verify-email?token=abc_DEF-123"

func TestVerificationMessage(t *testing.T) {
	m, err := VerificationMessage("Acme", "jane@example.com", "Jane", link)
	require.NoError(t, err)
	assert.Equal(t, "jane@example.com", m.To)
	assert.Equal(t, "Verify your email for Acme", m.Subject)
	assert.Contains(t, m.Text, "Hi Jane")
	assert.Contains(t, m.Text, link)
	assert.Contains(t, m.Text, "24 hours")
	assert.Contains(t, m.HTML, `href="`+link+`"`)
	assert.Contains(t, m.HTML, "Acme")
	assert.Contains(t, m.HTML, ">"+link+"<", "raw link printed for clients that strip buttons")
}

func TestPasswordResetMessage(t *testing.T) {
	m, err := PasswordResetMessage("Acme", "jane@example.com", "", "https://app.example.com/reset-password?token=xyz")
	require.NoError(t, err)
	assert.Equal(t, "Reset your Acme password", m.Subject)
	assert.Contains(t, m.Text, "Hi there", "empty name falls back")
	assert.Contains(t, m.Text, "1 hour")
	assert.Contains(t, m.Text, "reset-password?token=xyz")
	assert.Contains(t, m.HTML, `href="https://app.example.com/reset-password?token=xyz"`)
}

func TestTemplates_EscapeHTMLInName(t *testing.T) {
	m, err := VerificationMessage("Acme", "x@example.com", "<script>", link)
	require.NoError(t, err)
	assert.NotContains(t, m.HTML, "<script>")
	assert.Contains(t, m.HTML, "&lt;script&gt;")
	assert.Contains(t, m.Text, "<script>", "text body is not html-escaped")
}
