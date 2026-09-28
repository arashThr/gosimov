package auth_test

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"sync"
	"testing"

	"github.com/arashthr/skeleton/internal/auth"
	"github.com/arashthr/skeleton/internal/platform/config"
	"github.com/arashthr/skeleton/internal/platform/database/dbtest"
	"github.com/arashthr/skeleton/internal/platform/email"
	"github.com/arashthr/skeleton/internal/platform/errs"
	"github.com/arashthr/skeleton/internal/user"
	"go.uber.org/zap"
)

// outbox records emails instead of sending them.
type outbox struct {
	mu   sync.Mutex
	sent []email.Message
}

func (o *outbox) Send(_ context.Context, m email.Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, m)
	return nil
}

var tokenRe = regexp.MustCompile(`token=([A-Za-z0-9_%-]+)`)

// lastToken returns the token of the most recent email.
func (o *outbox) lastToken(t *testing.T) string {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.sent) == 0 {
		t.Fatal("no email sent")
	}
	m := tokenRe.FindStringSubmatch(o.sent[len(o.sent)-1].Text)
	if m == nil {
		t.Fatal("no token in email")
	}
	token, _ := url.QueryUnescape(m[1])
	return token
}

type fixture struct {
	svc      *auth.Service
	users    *user.Repository
	sessions *auth.SessionStore
	mail     *outbox
}

func newFixture(t *testing.T) *fixture {
	db := dbtest.New(t)
	cfg := &config.Config{AppName: "Test", BaseURL: "http://test.local"}
	users := user.NewRepository(db)
	sessions := auth.NewSessionStore(db)
	mail := &outbox{}
	svc := auth.NewService(cfg, users, sessions, auth.NewTokenStore(db), mail, zap.NewNop())
	return &fixture{svc: svc, users: users, sessions: sessions, mail: mail}
}

func TestPasswordSignUpAndSignIn(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	if _, err := f.svc.SignUp(ctx, "not-an-email", "password123"); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("invalid email: got %v", err)
	}
	if _, err := f.svc.SignUp(ctx, "a@example.com", "short"); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("short password: got %v", err)
	}

	u, err := f.svc.SignUp(ctx, "A@example.com", "password123")
	if err != nil {
		t.Fatalf("sign up: %v", err)
	}
	if _, err := f.svc.SignUp(ctx, "a@example.com", "password123"); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("duplicate sign up: got %v", err)
	}

	if _, err := f.svc.SignIn(ctx, "a@example.com", "wrong-password"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("wrong password: got %v", err)
	}
	if _, err := f.svc.SignIn(ctx, "nobody@example.com", "password123"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("unknown user: got %v", err)
	}
	if got, err := f.svc.SignIn(ctx, "a@example.com", "password123"); err != nil || got.ID != u.ID {
		t.Fatalf("sign in: %v", err)
	}

	// The verification email confirms the address, once.
	f.svc.Wait()
	token := f.mail.lastToken(t)
	if _, err := f.svc.VerifyEmail(ctx, token); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	if got, _ := f.users.GetByID(ctx, u.ID); !got.EmailVerified() {
		t.Fatal("email should be verified")
	}
	if _, err := f.svc.VerifyEmail(ctx, token); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("token reuse: got %v", err)
	}
}

func TestMagicLink(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	if err := f.svc.SendMagicLink(ctx, "new@example.com"); err != nil {
		t.Fatalf("send magic link: %v", err)
	}
	f.svc.Wait()
	token := f.mail.lastToken(t)

	u, err := f.svc.ConsumeMagicLink(ctx, token)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if u.Email != "new@example.com" || !u.EmailVerified() || u.HasPassword() {
		t.Fatalf("unexpected user: %+v", u)
	}
	if _, err := f.svc.ConsumeMagicLink(ctx, token); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("token reuse: got %v", err)
	}

	// A second link signs the same user in.
	_ = f.svc.SendMagicLink(ctx, "NEW@example.com")
	f.svc.Wait()
	again, err := f.svc.ConsumeMagicLink(ctx, f.mail.lastToken(t))
	if err != nil || again.ID != u.ID {
		t.Fatalf("second sign in: %v %+v", err, again)
	}
}

func TestPasswordReset(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	u, _ := f.svc.SignUp(ctx, "a@example.com", "password123")
	if _, err := f.sessions.Create(ctx, u.ID, "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Unknown emails succeed silently and send nothing.
	f.svc.Wait()
	before := len(f.mail.sent)
	if err := f.svc.SendPasswordReset(ctx, "nobody@example.com"); err != nil {
		t.Fatalf("reset unknown: %v", err)
	}
	f.svc.Wait()
	if len(f.mail.sent) != before {
		t.Fatal("no email should be sent for unknown addresses")
	}

	if err := f.svc.SendPasswordReset(ctx, "a@example.com"); err != nil {
		t.Fatalf("send reset: %v", err)
	}
	f.svc.Wait()
	if _, err := f.svc.ResetPassword(ctx, f.mail.lastToken(t), "new-password"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := f.svc.SignIn(ctx, "a@example.com", "new-password"); err != nil {
		t.Fatalf("sign in with new password: %v", err)
	}
	if _, err := f.svc.SignIn(ctx, "a@example.com", "password123"); err == nil {
		t.Fatal("old password should no longer work")
	}
}

func TestSessions(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	u, _ := f.svc.SignUp(ctx, "a@example.com", "password123")

	token, err := f.sessions.Create(ctx, u.ID, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got, err := f.sessions.User(ctx, token); err != nil || got.ID != u.ID {
		t.Fatalf("session user: %v", err)
	}
	if _, err := f.sessions.User(ctx, "bogus"); !errors.Is(err, errs.ErrUnauthorized) {
		t.Fatalf("unknown token: got %v", err)
	}

	// Changing the password ends all sessions.
	if err := f.svc.ChangePassword(ctx, u, "wrong", "another-password"); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("wrong current password: got %v", err)
	}
	if err := f.svc.ChangePassword(ctx, u, "password123", "another-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if _, err := f.sessions.User(ctx, token); !errors.Is(err, errs.ErrUnauthorized) {
		t.Fatalf("session should be gone: got %v", err)
	}
}

func TestSignInExternal(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	// Unverified provider emails are rejected.
	_, err := f.svc.SignInExternal(ctx, auth.ExternalUser{Provider: "github", ID: "1", Email: "x@example.com"})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("unverified email: got %v", err)
	}

	// New identity creates a user.
	gh := auth.ExternalUser{Provider: "github", ID: "1", Email: "a@example.com", EmailVerified: true}
	u, err := f.svc.SignInExternal(ctx, gh)
	if err != nil {
		t.Fatalf("first sign in: %v", err)
	}

	// Same identity returns the same user.
	if again, err := f.svc.SignInExternal(ctx, gh); err != nil || again.ID != u.ID {
		t.Fatalf("second sign in: %v", err)
	}

	// Another provider with the same verified email is linked to that user.
	google := auth.ExternalUser{Provider: "google", ID: "g", Email: "A@example.com", EmailVerified: true}
	if linked, err := f.svc.SignInExternal(ctx, google); err != nil || linked.ID != u.ID {
		t.Fatalf("link by email: %v", err)
	}

	// An identity owned by someone else cannot be linked.
	other, _ := f.svc.SignUp(ctx, "b@example.com", "password123")
	if err := f.svc.LinkExternal(ctx, other, gh); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("link owned identity: got %v", err)
	}
}

func TestSafeRedirect(t *testing.T) {
	cases := map[string]string{
		"":                  "/home",
		"/account":          "/account",
		"/a?b=c":            "/a?b=c",
		"//evil.com":        "/home",
		"https://evil.com":  "/home",
		"/\\evil.com":       "/home",
		"javascript:alert1": "/home",
	}
	for in, want := range cases {
		if got := auth.SafeRedirect(in, "/home"); got != want {
			t.Errorf("SafeRedirect(%q) = %q, want %q", in, got, want)
		}
	}
}
