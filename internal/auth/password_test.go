package auth

import (
	"strings"
	"testing"
)

func TestValidatePasswordEnforcesByteBounds(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "minimum length", password: strings.Repeat("a", MinPasswordBytes)},
		{name: "maximum length", password: strings.Repeat("a", MaxPasswordBytes)},
		{name: "too short", password: strings.Repeat("a", MinPasswordBytes-1), wantErr: true},
		{name: "too long", password: strings.Repeat("a", MaxPasswordBytes+1), wantErr: true},
		{name: "multi-byte characters count as bytes", password: strings.Repeat("é", 37), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidatePassword() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHashPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery", 4)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if !CheckPassword(hash, "correct-horse-battery") {
		t.Fatal("CheckPassword() rejected the original password")
	}
	if CheckPassword(hash, "wrong-password") {
		t.Fatal("CheckPassword() accepted the wrong password")
	}
	if strings.Contains(hash, "correct-horse-battery") {
		t.Fatal("bcrypt hash contains the plaintext password")
	}
}
