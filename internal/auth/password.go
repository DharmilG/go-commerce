package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	MinPasswordBytes = 8
	MaxPasswordBytes = 72
)

var (
	ErrPasswordTooShort = errors.New("password must be at least 8 bytes")
	ErrPasswordTooLong  = errors.New("password must be at most 72 bytes")
)

// ValidatePassword enforces bcrypt's byte limit instead of silently truncating.
func ValidatePassword(password string) error {
	switch {
	case len(password) < MinPasswordBytes:
		return ErrPasswordTooShort
	case len(password) > MaxPasswordBytes:
		return ErrPasswordTooLong
	default:
		return nil
	}
}

// HashPassword returns a bcrypt hash with the requested cost.
func HashPassword(password string, cost int) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword reports whether password matches hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
