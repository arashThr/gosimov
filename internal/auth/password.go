package auth

import (
	"fmt"
	"net/mail"

	"github.com/arashthr/skeleton/internal/platform/errs"
	"golang.org/x/crypto/bcrypt"
)

const (
	MinPasswordLength = 8
	maxPasswordLength = 72 // bcrypt ignores anything longer
)

var ErrInvalidCredentials = errs.Public(errs.ErrUnauthorized, "Email address or password is incorrect")

// dummyHash is compared against when the user does not exist, so that
// response times do not reveal which emails are registered.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(b), nil
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func ValidatePassword(password string) error {
	switch {
	case len(password) < MinPasswordLength:
		return errs.Public(errs.ErrInvalid, fmt.Sprintf("Password must be at least %d characters", MinPasswordLength))
	case len(password) > maxPasswordLength:
		return errs.Public(errs.ErrInvalid, fmt.Sprintf("Password must be at most %d characters", maxPasswordLength))
	}
	return nil
}

func ValidateEmail(email string) error {
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > 254 {
		return errs.Public(errs.ErrInvalid, "Please enter a valid email address")
	}
	return nil
}
