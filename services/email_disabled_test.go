package services

import (
	"context"
	"testing"

	"tablescore-api/config"
	"tablescore-api/mailer"

	"github.com/stretchr/testify/require"
)

func TestDisabledEmailDoesNotAccessDatabaseOrIssueTokens(t *testing.T) {
	cfg := &config.Config{Env: "production"}
	recorder := &mailer.Recorder{}
	// Nil database and user prove these flows stop before issuing tokens or
	// looking up whether an account exists when delivery is unavailable.
	require.ErrorIs(t, SendVerificationEmail(context.Background(), nil, recorder, cfg, nil), mailer.ErrDisabled)
	require.ErrorIs(t, RequestPasswordReset(context.Background(), nil, recorder, cfg, "user@example.com"), mailer.ErrDisabled)
	require.Empty(t, recorder.Sent())
}
