package user_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arashthr/skeleton/internal/platform/database/dbtest"
	"github.com/arashthr/skeleton/internal/platform/errs"
	"github.com/arashthr/skeleton/internal/user"
)

func TestRepository(t *testing.T) {
	ctx := context.Background()
	repo := user.NewRepository(dbtest.New(t))

	hash := "hash"
	u, err := repo.Create(ctx, "  Alice@Example.com ", &hash, false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.Email != "alice@example.com" || u.EmailVerified() || !u.HasPassword() {
		t.Fatalf("unexpected user: %+v", u)
	}

	if _, err := repo.Create(ctx, "alice@example.com", nil, false); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("duplicate email: got %v, want ErrConflict", err)
	}

	got, err := repo.GetByEmail(ctx, "ALICE@example.com")
	if err != nil || got.ID != u.ID {
		t.Fatalf("get by email: %v %+v", err, got)
	}
	if !got.CreatedAt.Equal(u.CreatedAt) {
		t.Fatalf("created_at round trip: got %v, want %v", got.CreatedAt, u.CreatedAt)
	}

	if _, err := repo.GetByID(ctx, 9999); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("missing user: got %v, want ErrNotFound", err)
	}

	if err := repo.MarkEmailVerified(ctx, u.ID); err != nil {
		t.Fatalf("mark verified: %v", err)
	}
	if got, _ := repo.GetByID(ctx, u.ID); !got.EmailVerified() {
		t.Fatal("email should be verified")
	}

	if err := repo.LinkIdentity(ctx, u.ID, "github", "42", "alice@example.com"); err != nil {
		t.Fatalf("link identity: %v", err)
	}
	if got, err := repo.GetByIdentity(ctx, "github", "42"); err != nil || got.ID != u.ID {
		t.Fatalf("get by identity: %v %+v", err, got)
	}

	other, err := repo.CreateWithIdentity(ctx, "bob@example.com", "google", "g-1")
	if err != nil {
		t.Fatalf("create with identity: %v", err)
	}
	if !other.EmailVerified() || other.HasPassword() {
		t.Fatalf("oauth user should be verified and password-less: %+v", other)
	}
	if err := repo.LinkIdentity(ctx, u.ID, "google", "g-1", "x@example.com"); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("identity linked twice: got %v, want ErrConflict", err)
	}

	if n, err := repo.Count(ctx); err != nil || n != 2 {
		t.Fatalf("count: %d %v", n, err)
	}

	// Deleting a user cascades to identities.
	if err := repo.Delete(ctx, u.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetByIdentity(ctx, "github", "42"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("identity should be gone: %v", err)
	}
}
