package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/arashthr/skeleton/internal/platform/database"
	"github.com/arashthr/skeleton/internal/platform/errs"
)

var ErrEmailTaken = errs.Public(errs.ErrConflict, "That email address is already registered")

type Repository struct {
	db *database.DB
}

func NewRepository(db *database.DB) *Repository {
	return &Repository{db: db}
}

const userColumns = `id, email, password_hash, email_verified_at, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// Create inserts a new user. passwordHash may be nil. When verified is true,
// the email is marked as verified right away (magic link and OAuth signups).
func (r *Repository) Create(ctx context.Context, email string, passwordHash *string, verified bool) (*User, error) {
	now := time.Now().UTC()
	var verifiedAt *time.Time
	if verified {
		verifiedAt = &now
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, email_verified_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $4)
		RETURNING `+userColumns,
		NormalizeEmail(email), passwordHash, verifiedAt, now)
	u, err := scanUser(row)
	if database.IsUniqueViolation(err) {
		return nil, ErrEmailTaken
	}
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

func (r *Repository) GetByID(ctx context.Context, id int64) (*User, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("get user %d: %w", id, err)
	}
	return u, nil
}

func (r *Repository) GetByEmail(ctx context.Context, email string) (*User, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1`, NormalizeEmail(email))
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return u, nil
}

func (r *Repository) UpdatePassword(ctx context.Context, id int64, passwordHash string) error {
	return r.exec(ctx, "update password",
		`UPDATE users SET password_hash = $1, updated_at = $2 WHERE id = $3`,
		passwordHash, time.Now().UTC(), id)
}

func (r *Repository) MarkEmailVerified(ctx context.Context, id int64) error {
	now := time.Now().UTC()
	return r.exec(ctx, "mark email verified",
		`UPDATE users SET email_verified_at = $1, updated_at = $1 WHERE id = $2 AND email_verified_at IS NULL`,
		now, id)
}

// Delete removes the user. Sessions, tokens and identities are removed by
// ON DELETE CASCADE.
func (r *Repository) Delete(ctx context.Context, id int64) error {
	return r.exec(ctx, "delete user", `DELETE FROM users WHERE id = $1`, id)
}

func (r *Repository) Count(ctx context.Context) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// GetByIdentity finds the user linked to a provider account.
func (r *Repository) GetByIdentity(ctx context.Context, provider, providerUserID string) (*User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.password_hash, u.email_verified_at, u.created_at, u.updated_at
		FROM users u
		JOIN user_identities i ON i.user_id = u.id
		WHERE i.provider = $1 AND i.provider_user_id = $2`, provider, providerUserID)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("get user by %s identity: %w", provider, err)
	}
	return u, nil
}

// LinkIdentity attaches a provider account to a user.
func (r *Repository) LinkIdentity(ctx context.Context, userID int64, provider, providerUserID, email string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO user_identities (user_id, provider, provider_user_id, email, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		userID, provider, providerUserID, NormalizeEmail(email), time.Now().UTC())
	if database.IsUniqueViolation(err) {
		return errs.Public(errs.ErrConflict, "This account is already linked to another user")
	}
	if err != nil {
		return fmt.Errorf("link %s identity: %w", provider, err)
	}
	return nil
}

// CreateWithIdentity creates a verified, password-less user linked to a
// provider account, atomically.
func (r *Repository) CreateWithIdentity(ctx context.Context, email, provider, providerUserID string) (*User, error) {
	var u *User
	err := r.db.InTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC()
		var err error
		u, err = scanUser(tx.QueryRowContext(ctx, `
			INSERT INTO users (email, email_verified_at, created_at, updated_at)
			VALUES ($1, $2, $2, $2)
			RETURNING `+userColumns, NormalizeEmail(email), now))
		if database.IsUniqueViolation(err) {
			return ErrEmailTaken
		}
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO user_identities (user_id, provider, provider_user_id, email, created_at)
			VALUES ($1, $2, $3, $4, $5)`, u.ID, provider, providerUserID, NormalizeEmail(email), now)
		if err != nil {
			return fmt.Errorf("link %s identity: %w", provider, err)
		}
		return nil
	})
	return u, err
}

// UnlinkIdentity removes a provider account from a user.
func (r *Repository) UnlinkIdentity(ctx context.Context, userID int64, provider string) error {
	return r.exec(ctx, "unlink identity",
		`DELETE FROM user_identities WHERE user_id = $1 AND provider = $2`, userID, provider)
}

func (r *Repository) ListIdentities(ctx context.Context, userID int64) ([]Identity, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, provider, provider_user_id, email, created_at
		FROM user_identities WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}
	defer rows.Close()

	var out []Identity
	for rows.Next() {
		var i Identity
		if err := rows.Scan(&i.ID, &i.UserID, &i.Provider, &i.ProviderUserID, &i.Email, &i.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan identity: %w", err)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (r *Repository) exec(ctx context.Context, op, query string, args ...any) error {
	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}
