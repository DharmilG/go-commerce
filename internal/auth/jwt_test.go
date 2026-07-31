package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func testTokenManager() *TokenManager {
	return NewTokenManager(strings.Repeat("s", 32), "go-commerce-test", "go-commerce-api-test", time.Hour)
}

func testUser() User {
	return User{
		ID:    uuid.New(),
		Email: "learner@example.com",
		Name:  "Test Learner",
		Role:  "customer",
	}
}

func TestTokenManagerIssueAndParse(t *testing.T) {
	manager := testTokenManager()
	user := testUser()

	token, expiresAt, err := manager.Issue(user)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if token == "" || expiresAt.IsZero() {
		t.Fatal("Issue() returned an empty token or expiry")
	}

	claims, err := manager.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if claims.Subject != user.ID.String() || claims.Email != user.Email || claims.Role != user.Role {
		t.Fatalf("claims = %+v, want user identity %+v", claims, user)
	}
	if claims.Issuer != "go-commerce-test" || claims.Audience[0] != "go-commerce-api-test" {
		t.Fatalf("claims issuer/audience = %q/%v", claims.Issuer, claims.Audience)
	}
}

func TestTokenManagerRejectsWrongSecret(t *testing.T) {
	token, _, err := testTokenManager().Issue(testUser())
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	other := NewTokenManager(strings.Repeat("x", 32), "go-commerce-test", "go-commerce-api-test", time.Hour)
	if _, err := other.Parse(token); err == nil {
		t.Fatal("Parse() accepted a token signed with a different secret")
	}
}

func TestTokenManagerRejectsExpiredToken(t *testing.T) {
	manager := NewTokenManager(strings.Repeat("s", 32), "go-commerce-test", "go-commerce-api-test", -time.Minute)
	token, _, err := manager.Issue(testUser())
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if _, err := manager.Parse(token); err == nil {
		t.Fatal("Parse() accepted an expired token")
	}
}

func TestTokenManagerRejectsWrongAudience(t *testing.T) {
	token, _, err := testTokenManager().Issue(testUser())
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	other := NewTokenManager(strings.Repeat("s", 32), "go-commerce-test", "other-api", time.Hour)
	if _, err := other.Parse(token); err == nil {
		t.Fatal("Parse() accepted a token for another audience")
	}
}

func TestTokenManagerRejectsTamperedToken(t *testing.T) {
	manager := testTokenManager()
	token, _, err := manager.Issue(testUser())
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("issued token has %d parts, want 3", len(parts))
	}
	// Change a decoded signature byte. Flipping the final base64 character can
	// leave the decoded signature unchanged because those bits are padding.
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if len(signature) == 0 {
		t.Fatal("issued token has an empty signature")
	}
	signature[0] ^= 0x01
	parts[2] = base64.RawURLEncoding.EncodeToString(signature)

	if _, err := manager.Parse(strings.Join(parts, ".")); err == nil {
		t.Fatal("Parse() accepted a token with a modified signature")
	}
}

func TestTokenManagerRejectsAlgNone(t *testing.T) {
	user := testUser()
	claims := Claims{
		Role:  user.Role,
		Email: user.Email,
		Name:  user.Name,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "go-commerce-test",
			Subject:   user.ID.String(),
			Audience:  jwt.ClaimStrings{"go-commerce-api-test"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign alg=none token: %v", err)
	}
	if _, err := testTokenManager().Parse(token); err == nil {
		t.Fatal("Parse() accepted an alg=none token")
	}
}

func TestTokenManagerRejectsNonUUIDSubject(t *testing.T) {
	claims := Claims{
		Role:  "customer",
		Email: "learner@example.com",
		Name:  "Test Learner",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "go-commerce-test",
			Subject:   "not-a-uuid",
			Audience:  jwt.ClaimStrings{"go-commerce-api-test"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(strings.Repeat("s", 32)))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	if _, err := testTokenManager().Parse(token); err == nil {
		t.Fatal("Parse() accepted a non-UUID subject")
	}
}
