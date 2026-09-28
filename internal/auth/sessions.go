package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/arashthr/skeleton/internal/platform/database"
	"github.com/arashthr/skeleton/internal/platform/errs"
	"github.com/arashthr/skeleton/internal/user"
)

const (
	SessionDuration = 30 * 24 * time.Hour
	// sessionTouchInterval limits how often last_used_at is written.
	sessionTouchInterval = time.Hour
)

// SessionStore persists browser sessions. The cookie holds a random token;
// the database only stores its hash.
type SessionStore struct {
	db *database.DB
}

func NewSessionStore(db *database.DB) *SessionStore {
	return &SessionStore{db: db}
}

// Create starts a session for the user and returns its token.
func (s *SessionStore) Create(ctx context.Context, userID int64, ip, userAgent string) (string, error) {
	token := NewToken()
	now := time.Now().UTC()
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (user_id, token_hash, ip_address, user_agent, created_at, last_used_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $5, $6)`,
		userID, HashToken(token), ip, userAgent, now, now.Add(SessionDuration))
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

// User returns the user owning a valid session token.
func (s *SessionStore) User(ctx context.Context, token string) (*user.User, error) {
	var (
		u        user.User
		lastUsed time.Time
		hash     = HashToken(token)
		now      = time.Now().UTC()
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.password_hash, u.email_verified_at, u.created_at, u.updated_at, s.last_used_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > $2`, hash, now,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.ErrUnauthorized
	}
	if err != nil {
		return nil, fmt.Errorf("session user: %w", err)
	}

	if now.Sub(lastUsed) > sessionTouchInterval {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE sessions SET last_used_at = $1 WHERE token_hash = $2`, now, hash); err != nil {
			return nil, fmt.Errorf("touch session: %w", err)
		}
	}
	return &u, nil
}

func (s *SessionStore) Delete(ctx context.Context, token string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = $1`, HashToken(token)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteAllForUser signs the user out everywhere.
func (s *SessionStore) DeleteAllForUser(ctx context.Context, userID int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	return nil
}

func (s *SessionStore) DeleteExpired(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return res.RowsAffected()
}
