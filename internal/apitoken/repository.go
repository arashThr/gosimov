// Package apitoken lets users create personal API tokens and authenticates
// API requests carrying "Authorization: Bearer <token>".
package apitoken

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arashthr/skeleton/internal/auth"
	"github.com/arashthr/skeleton/internal/platform/database"
	"github.com/arashthr/skeleton/internal/platform/errs"
	"github.com/arashthr/skeleton/internal/user"
)

const (
	// Prefix makes tokens recognizable, e.g. by secret scanners.
	Prefix       = "skt_"
	MaxPerUser   = 10
	maxNameLen   = 60
	touchEvery   = 5 * time.Minute
	defaultLabel = "API token"
)

type Token struct {
	ID         int64
	UserID     int64
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

type Repository struct {
	db *database.DB
}

func NewRepository(db *database.DB) *Repository {
	return &Repository{db: db}
}

// Create issues a token and returns the raw value, which is shown to the
// user once. Only its hash is stored.
func (r *Repository) Create(ctx context.Context, userID int64, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultLabel
	}
	if len(name) > maxNameLen {
		return "", errs.Public(errs.ErrInvalid, fmt.Sprintf("Token name must be at most %d characters", maxNameLen))
	}

	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_tokens WHERE user_id = $1`, userID).Scan(&count); err != nil {
		return "", fmt.Errorf("count api tokens: %w", err)
	}
	if count >= MaxPerUser {
		return "", errs.Public(errs.ErrConflict, fmt.Sprintf("You can have at most %d tokens. Revoke one first.", MaxPerUser))
	}

	raw := Prefix + auth.NewToken()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO api_tokens (user_id, name, token_hash, created_at)
		VALUES ($1, $2, $3, $4)`, userID, name, auth.HashToken(raw), time.Now().UTC())
	if err != nil {
		return "", fmt.Errorf("create api token: %w", err)
	}
	return raw, nil
}

func (r *Repository) List(ctx context.Context, userID int64) ([]Token, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, name, created_at, last_used_at
		FROM api_tokens WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	defer rows.Close()

	var tokens []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, fmt.Errorf("scan api token: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func (r *Repository) Delete(ctx context.Context, userID, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("delete api token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errs.ErrNotFound
	}
	return nil
}

// User returns the owner of a raw token.
func (r *Repository) User(ctx context.Context, raw string) (*user.User, error) {
	var (
		u        user.User
		tokenID  int64
		lastUsed *time.Time
		now      = time.Now().UTC()
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.password_hash, u.email_verified_at, u.created_at, u.updated_at, t.id, t.last_used_at
		FROM api_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = $1`, auth.HashToken(raw),
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt, &tokenID, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.ErrUnauthorized
	}
	if err != nil {
		return nil, fmt.Errorf("api token user: %w", err)
	}

	if lastUsed == nil || now.Sub(*lastUsed) > touchEvery {
		if _, err := r.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = $1 WHERE id = $2`, now, tokenID); err != nil {
			return nil, fmt.Errorf("touch api token: %w", err)
		}
	}
	return &u, nil
}
