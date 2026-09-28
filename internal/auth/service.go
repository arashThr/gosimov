package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/arashthr/skeleton/internal/platform/config"
	"github.com/arashthr/skeleton/internal/platform/email"
	"github.com/arashthr/skeleton/internal/platform/errs"
	"github.com/arashthr/skeleton/internal/user"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

// Service implements the authentication use cases. It knows nothing about
// HTTP; handlers translate requests into calls to it.
type Service struct {
	cfg      *config.Config
	users    *user.Repository
	sessions *SessionStore
	tokens   *TokenStore
	mailer   email.Sender
	log      *zap.Logger

	emails sync.WaitGroup // in-flight emails, awaited on shutdown
}

func NewService(
	cfg *config.Config,
	users *user.Repository,
	sessions *SessionStore,
	tokens *TokenStore,
	mailer email.Sender,
	log *zap.Logger,
) *Service {
	return &Service{
		cfg:      cfg,
		users:    users,
		sessions: sessions,
		tokens:   tokens,
		mailer:   mailer,
		log:      log.Named("auth"),
	}
}

// Wait blocks until all queued emails have been handed to the mailer.
func (s *Service) Wait() { s.emails.Wait() }

// SignUp registers a user with email and password and sends a verification email.
func (s *Service) SignUp(ctx context.Context, emailAddr, password string) (*user.User, error) {
	emailAddr = user.NormalizeEmail(emailAddr)
	if err := ValidateEmail(emailAddr); err != nil {
		return nil, err
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	u, err := s.users.Create(ctx, emailAddr, &hash, false)
	if err != nil {
		return nil, err
	}
	s.log.Info("user signed up", zap.Int64("user_id", u.ID), zap.String("method", "password"))
	if err := s.SendVerificationEmail(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// SignIn checks an email and password pair.
func (s *Service) SignIn(ctx context.Context, emailAddr, password string) (*user.User, error) {
	u, err := s.users.GetByEmail(ctx, emailAddr)
	if errors.Is(err, errs.ErrNotFound) {
		CheckPassword(string(dummyHash), password) // keep timing similar to a real check
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if !u.HasPassword() || !CheckPassword(*u.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}
	return u, nil
}

// SendMagicLink emails a one-time sign-in link. It works for both new and
// existing users: the account is created when the link is used.
func (s *Service) SendMagicLink(ctx context.Context, emailAddr string) error {
	emailAddr = user.NormalizeEmail(emailAddr)
	if err := ValidateEmail(emailAddr); err != nil {
		return err
	}
	token, err := s.tokens.Create(ctx, emailAddr, PurposeMagicLink)
	if err != nil {
		return err
	}
	s.sendAction(ctx, emailAddr, "Your sign-in link",
		"Sign in to "+s.cfg.AppName,
		"Click the button below to sign in. The link expires in 15 minutes and can only be used once.",
		"Sign in", s.link("/magic-link/verify", token),
		"If you did not request this email, you can safely ignore it.")
	return nil
}

// ConsumeMagicLink signs in (or signs up) the owner of a magic link.
func (s *Service) ConsumeMagicLink(ctx context.Context, token string) (*user.User, error) {
	emailAddr, err := s.tokens.Consume(ctx, token, PurposeMagicLink)
	if err != nil {
		return nil, err
	}
	u, err := s.users.GetByEmail(ctx, emailAddr)
	switch {
	case errors.Is(err, errs.ErrNotFound):
		u, err = s.users.Create(ctx, emailAddr, nil, true)
		if err != nil {
			return nil, err
		}
		s.log.Info("user signed up", zap.Int64("user_id", u.ID), zap.String("method", "magic_link"))
		return u, nil
	case err != nil:
		return nil, err
	}
	// Clicking the link proves ownership of the address.
	if !u.EmailVerified() {
		if err := s.users.MarkEmailVerified(ctx, u.ID); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// SendVerificationEmail emails a link that confirms the user's address.
func (s *Service) SendVerificationEmail(ctx context.Context, u *user.User) error {
	if u.EmailVerified() {
		return nil
	}
	token, err := s.tokens.Create(ctx, u.Email, PurposeEmailVerification)
	if err != nil {
		return err
	}
	s.sendAction(ctx, u.Email, "Verify your email address",
		"Confirm your email",
		"Thanks for signing up to "+s.cfg.AppName+". Please confirm that this is your email address.",
		"Verify email", s.link("/verify-email", token),
		"The link expires in 3 days. If you did not create an account, you can ignore this email.")
	return nil
}

// VerifyEmail marks the owner of the token as verified.
func (s *Service) VerifyEmail(ctx context.Context, token string) (*user.User, error) {
	emailAddr, err := s.tokens.Consume(ctx, token, PurposeEmailVerification)
	if err != nil {
		return nil, err
	}
	u, err := s.users.GetByEmail(ctx, emailAddr)
	if errors.Is(err, errs.ErrNotFound) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	if err := s.users.MarkEmailVerified(ctx, u.ID); err != nil {
		return nil, err
	}
	return u, nil
}

// SendPasswordReset emails a reset link if the address belongs to a user.
// It reports success either way so the form cannot be used to probe for accounts.
func (s *Service) SendPasswordReset(ctx context.Context, emailAddr string) error {
	emailAddr = user.NormalizeEmail(emailAddr)
	if err := ValidateEmail(emailAddr); err != nil {
		return err
	}
	if _, err := s.users.GetByEmail(ctx, emailAddr); err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			s.log.Info("password reset requested for unknown email")
			return nil
		}
		return err
	}
	token, err := s.tokens.Create(ctx, emailAddr, PurposePasswordReset)
	if err != nil {
		return err
	}
	s.sendAction(ctx, emailAddr, "Reset your password",
		"Reset your password",
		"We received a request to reset the password of your "+s.cfg.AppName+" account.",
		"Choose a new password", s.link("/reset-password", token),
		"The link expires in 1 hour. If you did not request a reset, you can ignore this email.")
	return nil
}

// ResetPassword sets a new password using a reset token and signs the user
// out of all existing sessions.
func (s *Service) ResetPassword(ctx context.Context, token, password string) (*user.User, error) {
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	emailAddr, err := s.tokens.Consume(ctx, token, PurposePasswordReset)
	if err != nil {
		return nil, err
	}
	u, err := s.users.GetByEmail(ctx, emailAddr)
	if errors.Is(err, errs.ErrNotFound) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	if err := s.users.UpdatePassword(ctx, u.ID, hash); err != nil {
		return nil, err
	}
	// The reset link reached the inbox, which proves ownership of the address.
	if err := s.users.MarkEmailVerified(ctx, u.ID); err != nil {
		return nil, err
	}
	if err := s.sessions.DeleteAllForUser(ctx, u.ID); err != nil {
		return nil, err
	}
	s.log.Info("password reset", zap.Int64("user_id", u.ID))
	return u, nil
}

// ChangePassword sets a new password for a signed-in user and ends all their
// sessions. Users without a password (magic link or OAuth signups) can set
// one without providing the current password.
func (s *Service) ChangePassword(ctx context.Context, u *user.User, current, newPassword string) error {
	if u.HasPassword() && !CheckPassword(*u.PasswordHash, current) {
		return errs.Public(errs.ErrInvalid, "Current password is incorrect")
	}
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.users.UpdatePassword(ctx, u.ID, hash); err != nil {
		return err
	}
	if err := s.sessions.DeleteAllForUser(ctx, u.ID); err != nil {
		return err
	}
	s.log.Info("password changed", zap.Int64("user_id", u.ID))
	return nil
}

// ExternalUser is what an OAuth provider tells us about the signed-in person.
type ExternalUser struct {
	Provider      string
	ID            string
	Email         string
	EmailVerified bool
}

// SignInExternal signs in a user coming from an OAuth provider. Unknown
// accounts are linked to an existing user with the same verified email, or
// a new user is created.
func (s *Service) SignInExternal(ctx context.Context, ext ExternalUser) (*user.User, error) {
	u, err := s.users.GetByIdentity(ctx, ext.Provider, ext.ID)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return nil, err
	}

	if ext.Email == "" || !ext.EmailVerified {
		return nil, errs.Public(errs.ErrForbidden, "Your "+ext.Provider+" account has no verified email address")
	}

	u, err = s.users.GetByEmail(ctx, ext.Email)
	switch {
	case errors.Is(err, errs.ErrNotFound):
		u, err = s.users.CreateWithIdentity(ctx, ext.Email, ext.Provider, ext.ID)
		if err != nil {
			return nil, err
		}
		s.log.Info("user signed up", zap.Int64("user_id", u.ID), zap.String("method", ext.Provider))
		return u, nil
	case err != nil:
		return nil, err
	}

	if err := s.users.LinkIdentity(ctx, u.ID, ext.Provider, ext.ID, ext.Email); err != nil {
		return nil, err
	}
	if !u.EmailVerified() {
		if err := s.users.MarkEmailVerified(ctx, u.ID); err != nil {
			return nil, err
		}
	}
	s.log.Info("identity linked", zap.Int64("user_id", u.ID), zap.String("provider", ext.Provider))
	return u, nil
}

// LinkExternal connects a provider account to an already signed-in user.
func (s *Service) LinkExternal(ctx context.Context, u *user.User, ext ExternalUser) error {
	owner, err := s.users.GetByIdentity(ctx, ext.Provider, ext.ID)
	switch {
	case err == nil && owner.ID == u.ID:
		return nil
	case err == nil:
		return errs.Public(errs.ErrConflict, "This "+ext.Provider+" account is already connected to another user")
	case !errors.Is(err, errs.ErrNotFound):
		return err
	}
	if err := s.users.LinkIdentity(ctx, u.ID, ext.Provider, ext.ID, ext.Email); err != nil {
		return err
	}
	s.log.Info("identity linked", zap.Int64("user_id", u.ID), zap.String("provider", ext.Provider))
	return nil
}

// CleanupExpired deletes expired sessions and tokens.
func (s *Service) CleanupExpired(ctx context.Context) error {
	sessions, err := s.sessions.DeleteExpired(ctx)
	if err != nil {
		return err
	}
	tokens, err := s.tokens.DeleteExpired(ctx)
	if err != nil {
		return err
	}
	if sessions+tokens > 0 {
		s.log.Info("expired auth data removed", zap.Int64("sessions", sessions), zap.Int64("tokens", tokens))
	}
	return nil
}

// runCleanup periodically removes expired sessions and tokens until ctx is done.
func (s *Service) runCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if err := s.CleanupExpired(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("cleanup expired auth data", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// registerLifecycle starts the cleanup job and makes shutdown wait for
// queued emails.
func registerLifecycle(lc fx.Lifecycle, s *Service) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				defer close(done)
				s.runCleanup(ctx)
			}()
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			<-done
			s.Wait()
			return nil
		},
	})
}

func (s *Service) link(path, token string) string {
	return s.cfg.URL(path) + "?" + url.Values{"token": {token}}.Encode()
}

// sendAction renders and sends an email in the background, so slow SMTP
// servers do not delay the response.
func (s *Service) sendAction(ctx context.Context, to, subject, heading, intro, button, link, footnote string) {
	var html strings.Builder
	if err := actionEmail(s.cfg.AppName, heading, intro, button, link, footnote).Render(ctx, &html); err != nil {
		s.log.Error("render email", zap.Error(err))
		return
	}
	text := fmt.Sprintf("%s\n\n%s\n\n%s: %s\n\n%s\n", heading, intro, button, link, footnote)

	msg := email.Message{To: to, Subject: subject, Text: text, HTML: html.String()}
	ctx = context.WithoutCancel(ctx)
	s.emails.Go(func() {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := s.mailer.Send(ctx, msg); err != nil {
			s.log.Error("send email", zap.String("subject", subject), zap.Error(err))
		}
	})
}
