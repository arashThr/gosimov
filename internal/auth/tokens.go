package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/arashthr/skeleton/internal/platform/database"
	"github.com/arashthr/skeleton/internal/platform/errs"
	"github.com/arashthr/skeleton/internal/user"
)

// Purpose tells what a one-time email token can be used for.
type Purpose string

const (
	PurposeMagicLink         Purpose = "magic_link"
	PurposeEmailVerification Purpose = "email_verification"
	PurposePasswordReset     Purpose = "password_reset"
)

var tokenTTL = map[Purpose]time.Duration{
	PurposeMagicLink:         15 * time.Minute,
	PurposeEmailVerification: 72 * time.Hour,
	PurposePasswordReset:     time.Hour,
}

var ErrInvalidToken = errs.Public(errs.ErrInvalid, "This link is invalid or has expired")

// NewToken returns a random URL-safe token with 256 bits of entropy.
func NewToken() string {
	return base64.RawURLEncoding.EncodeToString(randomBytes(32))
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b) // never returns an error, see crypto/rand docs
	return b
}

// HashToken returns the value stored in the database for a token. Only
// hashes are stored, so a leaked database does not leak usable tokens.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// TokenStore persists single-use tokens sent by email.
type TokenStore struct {
	db *database.DB
}

func NewTokenStore(db *database.DB) *TokenStore {
	return &TokenStore{db: db}
}

// Create issues a token for email. The raw token is returned; only its hash is stored.
func (s *TokenStore) Create(ctx context.Context, email string, purpose Purpose) (string, error) {
	token := NewToken()
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_tokens (email, purpose, token_hash, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)`,
		user.NormalizeEmail(email), purpose, HashToken(token), now, now.Add(tokenTTL[purpose]))
	if err != nil {
		return "", fmt.Errorf("create %s token: %w", purpose, err)
	}
	return token, nil
}

// Consume deletes the token and returns the email it was issued for. It fails
// with ErrInvalidToken if the token is unknown, expired or meant for another purpose.
func (s *TokenStore) Consume(ctx context.Context, token string, purpose Purpose) (string, error) {
	var (
		email     string
		expiresAt time.Time
	)
	err := s.db.QueryRowContext(ctx, `
		DELETE FROM auth_tokens WHERE token_hash = $1 AND purpose = $2
		RETURNING email, expires_at`,
		HashToken(token), purpose).Scan(&email, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidToken
	}
	if err != nil {
		return "", fmt.Errorf("consume %s token: %w", purpose, err)
	}
	if time.Now().After(expiresAt) {
		return "", ErrInvalidToken
	}
	return email, nil
}

// DeleteExpired removes expired tokens.
func (s *TokenStore) DeleteExpired(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM auth_tokens WHERE expires_at < $1`, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("delete expired tokens: %w", err)
	}
	return res.RowsAffected()
}
