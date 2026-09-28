package mailer

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"tablescore-api/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var sample = Message{To: "a@example.com", Subject: "Hello", Text: "body text", HTML: "<p>body</p>"}

func TestLog_WritesRecipientSubjectAndBody(t *testing.T) {
	var buf bytes.Buffer
	l := &Log{Logger: slog.New(slog.NewTextHandler(&buf, nil))}
	require.NoError(t, l.Send(context.Background(), sample))
	out := buf.String()
	assert.Contains(t, out, "smtp not configured")
	assert.Contains(t, out, "to=a@example.com")
	assert.Contains(t, out, "subject=Hello")
	assert.Contains(t, out, "body text")
}

func TestRecorder_RecordsInOrderAndResets(t *testing.T) {
	r := &Recorder{}
	require.NoError(t, r.Send(context.Background(), sample))
	second := sample
	second.Subject = "Second"
	require.NoError(t, r.Send(context.Background(), second))

	sent := r.Sent()
	require.Len(t, sent, 2)
	assert.Equal(t, "Hello", sent[0].Subject)
	assert.Equal(t, "Second", sent[1].Subject)

	r.Reset()
	assert.Empty(t, r.Sent())
}

func TestAsync_DeliversInBackground(t *testing.T) {
	r := &Recorder{}
	a := Async(r, time.Second)
	require.NoError(t, a.Send(context.Background(), sample))
	a.Wait()
	require.Len(t, r.Sent(), 1)
	assert.Equal(t, "Hello", r.Sent()[0].Subject)
}

type failing struct{}

func (failing) Send(context.Context, Message) error { return errors.New("smtp down") }

func TestAsync_LogsFailuresInsteadOfReturningThem(t *testing.T) {
	var buf bytes.Buffer
	a := Async(failing{}, time.Second)
	a.Logger = slog.New(slog.NewTextHandler(&buf, nil))
	require.NoError(t, a.Send(context.Background(), sample), "async send never fails the caller")
	a.Wait()
	assert.Contains(t, buf.String(), "smtp down")
	assert.Contains(t, buf.String(), "to=a@example.com")
}

func TestNew_PicksLogWithoutHostAndSMTPWithHost(t *testing.T) {
	var cfg config.Config
	m, err := New(&cfg)
	require.NoError(t, err)
	_, isLog := m.(*Log)
	assert.True(t, isLog)

	cfg.SMTP.Host = "smtp.example.com"
	cfg.SMTP.Port = 587
	m, err = New(&cfg)
	require.NoError(t, err)
	_, isSMTP := m.(*SMTP)
	assert.True(t, isSMTP)
}

func TestNew_InvalidPortIsAStartupErrorNotAFallback(t *testing.T) {
	var cfg config.Config
	cfg.SMTP.Host = "smtp.example.com"
	cfg.SMTP.Port = 70000

	m, err := New(&cfg)
	require.Error(t, err)
	assert.Nil(t, m)
}

func TestProductionWithoutSMTPDisablesDelivery(t *testing.T) {
	var cfg config.Config
	cfg.Env = "production"
	m, err := New(&cfg)
	require.NoError(t, err)
	require.IsType(t, &Disabled{}, m)
	require.ErrorIs(t, m.Send(context.Background(), sample), ErrDisabled)
}
