// Package mailer sends transactional email. Services depend on the Mailer
// interface; main picks an SMTP implementation when smtp.host is configured
// and a logging one otherwise, so a fresh checkout works with no mail server.
package mailer

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"tablescore-api/config"
)

// Message is one email: a plain-text body plus an HTML alternative.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Mailer delivers messages. Implementations must be safe for concurrent use.
type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// Log writes emails to the log instead of sending them. The text body is
// included so links (verification, password reset) can be copied from the
// terminal in local dev.
type Log struct {
	Logger *slog.Logger // nil means slog.Default()
}

func (l *Log) logger() *slog.Logger {
	if l.Logger != nil {
		return l.Logger
	}
	return slog.Default()
}

// Send logs the message and reports success.
func (l *Log) Send(_ context.Context, m Message) error {
	l.logger().Info("email (smtp not configured)", "to", m.To, "subject", m.Subject, "body", m.Text)
	return nil
}

// AsyncMailer sends through another Mailer on a goroutine so request handlers
// never wait on SMTP. Failures are logged, not returned: by the time they
// happen the HTTP response is gone, and a mail failure must not fail
// registration. It also keeps the forgot-password response time independent
// of whether an account exists.
type AsyncMailer struct {
	inner   Mailer
	timeout time.Duration
	wg      sync.WaitGroup
	Logger  *slog.Logger // nil means slog.Default()
}

// Async wraps m. timeout bounds each background delivery.
func Async(m Mailer, timeout time.Duration) *AsyncMailer {
	return &AsyncMailer{inner: m, timeout: timeout}
}

func (a *AsyncMailer) logger() *slog.Logger {
	if a.Logger != nil {
		return a.Logger
	}
	return slog.Default()
}

// Send schedules delivery and returns nil immediately. The caller's context
// is deliberately not used: it is usually a request context that is cancelled
// as soon as the response is written.
func (a *AsyncMailer) Send(_ context.Context, m Message) error {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
		defer cancel()
		if err := a.inner.Send(ctx, m); err != nil {
			a.logger().Error("failed to send email", "to", m.To, "subject", m.Subject, "error", err)
		}
	}()
	return nil
}

// Wait blocks until every scheduled delivery has finished. main calls it on
// shutdown so an email accepted just before SIGTERM still goes out.
func (a *AsyncMailer) Wait() { a.wg.Wait() }

// Recorder is the test double: it keeps every message in memory.
type Recorder struct {
	mu   sync.Mutex
	msgs []Message
}

// Send records the message.
func (r *Recorder) Send(_ context.Context, m Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, m)
	return nil
}

// Sent returns a copy of the recorded messages in send order.
func (r *Recorder) Sent() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Message, len(r.msgs))
	copy(out, r.msgs)
	return out
}

// Reset forgets every recorded message.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = nil
}

// smtpDialTimeout bounds one SMTP connection attempt.
const smtpDialTimeout = 10 * time.Second

// New returns the mailer main should use: SMTP when smtp.host is set, Log
// otherwise. One log line names the choice. When smtp.host is set but the
// mailer cannot be built, that is a startup error — never a silent fallback
// to logging, which would let production emails go unsent without anyone
// noticing.
func New(cfg *config.Config) (Mailer, error) {
	if !cfg.SMTPConfigured() {
		slog.Info("smtp.host is empty — emails will be logged, not sent")
		return &Log{}, nil
	}
	m, err := NewSMTP(cfg.SMTP.Host, cfg.SMTPPort(), cfg.SMTP.Username, cfg.SMTP.Password, cfg.SMTPFrom(), cfg.SMTPTLS(), smtpDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to build smtp mailer: %w", err)
	}
	slog.Info("smtp mailer configured", "host", cfg.SMTP.Host, "port", cfg.SMTPPort(), "tls", cfg.SMTPTLS())
	return m, nil
}
