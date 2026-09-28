package mailer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"tablescore-api/config"

	"github.com/wneessen/go-mail"
)

// SMTP delivers through an SMTP server using go-mail. One connection is
// opened per message; transactional volume does not justify pooling.
type SMTP struct {
	client *mail.Client
	from   string
}

// NewSMTP builds an SMTP mailer. tlsMode is one of config.SMTPTLSStartTLS,
// config.SMTPTLSImplicit or config.SMTPTLSNone. When username is non-empty
// the auth mechanism is negotiated with the server.
func NewSMTP(host string, port int, username, password, from, tlsMode string, timeout time.Duration) (*SMTP, error) {
	if host == "" {
		return nil, errors.New("smtp host is empty")
	}
	opts := []mail.Option{mail.WithPort(port), mail.WithTimeout(timeout)}
	switch tlsMode {
	case config.SMTPTLSStartTLS:
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	case config.SMTPTLSImplicit:
		opts = append(opts, mail.WithSSL())
	case config.SMTPTLSNone:
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	default:
		return nil, fmt.Errorf("unknown smtp tls mode %q", tlsMode)
	}
	if username != "" {
		opts = append(opts, mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover), mail.WithUsername(username), mail.WithPassword(password))
	}
	client, err := mail.NewClient(host, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create smtp client: %w", err)
	}
	return &SMTP{client: client, from: from}, nil
}

// Send connects, delivers m as text with an HTML alternative, and disconnects.
func (s *SMTP) Send(ctx context.Context, m Message) error {
	msg := mail.NewMsg()
	if err := msg.From(s.from); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	if err := msg.To(m.To); err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}
	msg.Subject(m.Subject)
	msg.SetBodyString(mail.TypeTextPlain, m.Text)
	if m.HTML != "" {
		msg.AddAlternativeString(mail.TypeTextHTML, m.HTML)
	}
	if err := s.client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("smtp send failed: %w", err)
	}
	return nil
}
