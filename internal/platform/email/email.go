// Package email sends transactional emails over SMTP. Without SMTP
// configuration, emails are written to the log instead, which keeps local
// development friction-free (click the links straight from the console).
package email

import (
	"context"
	"fmt"

	"github.com/arashthr/skeleton/internal/platform/config"
	"github.com/wneessen/go-mail"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var Module = fx.Module("email", fx.Provide(New))

type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

type Sender interface {
	Send(ctx context.Context, msg Message) error
}

func New(cfg *config.Config, log *zap.Logger) (Sender, error) {
	if !cfg.SMTP.Enabled() {
		log.Warn("SMTP_HOST is not set, emails will be logged instead of sent")
		return &LogSender{log: log.Named("email")}, nil
	}
	return NewSMTPSender(cfg.SMTP)
}

// SMTPSender delivers emails through an SMTP server.
type SMTPSender struct {
	client *mail.Client
	from   string
}

func NewSMTPSender(cfg config.SMTP) (*SMTPSender, error) {
	opts := []mail.Option{mail.WithPort(cfg.Port)}
	if cfg.Port == 465 {
		opts = append(opts, mail.WithSSLPort(false))
	} else {
		opts = append(opts, mail.WithTLSPortPolicy(mail.TLSOpportunistic))
	}
	if cfg.Username != "" {
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover),
			mail.WithUsername(cfg.Username),
			mail.WithPassword(cfg.Password))
	}
	client, err := mail.NewClient(cfg.Host, opts...)
	if err != nil {
		return nil, fmt.Errorf("smtp client: %w", err)
	}
	return &SMTPSender{client: client, from: cfg.From}, nil
}

func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	m := mail.NewMsg()
	if err := m.From(s.from); err != nil {
		return fmt.Errorf("email from: %w", err)
	}
	if err := m.To(msg.To); err != nil {
		return fmt.Errorf("email to: %w", err)
	}
	m.Subject(msg.Subject)
	m.SetBodyString(mail.TypeTextPlain, msg.Text)
	if msg.HTML != "" {
		m.AddAlternativeString(mail.TypeTextHTML, msg.HTML)
	}
	if err := s.client.DialAndSendWithContext(ctx, m); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	return nil
}

// LogSender writes emails to the log. Used in development.
type LogSender struct {
	log *zap.Logger
}

func (s *LogSender) Send(_ context.Context, msg Message) error {
	s.log.Info("email (not sent, SMTP disabled)",
		zap.String("to", msg.To),
		zap.String("subject", msg.Subject))
	fmt.Printf("\n----- email to %s: %s -----\n%s\n-----\n\n", msg.To, msg.Subject, msg.Text)
	return nil
}
