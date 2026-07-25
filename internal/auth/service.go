package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrEmailTaken         = errors.New("email is already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidEmail       = errors.New("email address is not valid")
	ErrInvalidName        = errors.New("name must be between 2 and 100 characters")
	ErrUserNotFound       = errors.New("user not found")
)

// ValidationError is returned for user-correctable input problems.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// Service owns registration, login, and token validation.
type Service struct {
	db         *pgxpool.Pool
	tokens     *TokenManager
	bcryptCost int
	dummyHash  []byte
	logger     *slog.Logger
	now        func() time.Time
}

func NewService(db *pgxpool.Pool, tokens *TokenManager, bcryptCost int, logger *slog.Logger) *Service {
	// Comparing against a real hash keeps unknown-email logins from being
	// noticeably faster than wrong-password logins.
	dummyHash, err := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcryptCost)
	if err != nil {
		dummyHash = []byte("$2a$12$C6UzMDM.H6dfI/f/IKcEe.7pJ5L4Zg2rG6pW2Xx9n0V4v5Xq0P2eG")
	}

	return &Service{
		db:         db,
		tokens:     tokens,
		bcryptCost: bcryptCost,
		dummyHash:  dummyHash,
		logger:     logger,
		now:        time.Now,
	}
}

// Register creates a customer account and returns a session.
func (s *Service) Register(ctx context.Context, name, email, password string) (Session, error) {
	name = strings.TrimSpace(name)
	email = NormalizeEmail(email)

	if utf8.RuneCountInString(name) < 2 || utf8.RuneCountInString(name) > 100 {
		return Session{}, ErrInvalidName
	}
	if err := validateEmail(email); err != nil {
		return Session{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return Session{}, err
	}

	hash, err := HashPassword(password, s.bcryptCost)
	if err != nil {
		return Session{}, err
	}

	user := User{
		ID:        uuid.New(),
		Email:     email,
		Name:      name,
		Role:      "customer",
		CreatedAt: s.now().UTC(),
	}

	err = s.db.QueryRow(ctx,
		`INSERT INTO users (id, email, password_hash, name, role)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING created_at`,
		user.ID.String(), user.Email, hash, user.Name, user.Role,
	).Scan(&user.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Session{}, ErrEmailTaken
		}
		return Session{}, fmt.Errorf("insert user: %w", err)
	}

	return s.session(user)
}

// Login verifies credentials and returns a session.
func (s *Service) Login(ctx context.Context, email, password string) (Session, error) {
	email = NormalizeEmail(email)

	var (
		user         User
		passwordHash string
	)
	err := s.db.QueryRow(ctx,
		`SELECT id, email, password_hash, name, role, created_at
		 FROM users
		 WHERE lower(email) = lower($1)`,
		email,
	).Scan(&user.ID, &user.Email, &passwordHash, &user.Name, &user.Role, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, fmt.Errorf("lookup user: %w", err)
	}

	if !CheckPassword(passwordHash, password) {
		return Session{}, ErrInvalidCredentials
	}

	return s.session(user)
}

// GetByID returns the current database state for an authenticated user.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (User, error) {
	var user User
	err := s.db.QueryRow(ctx,
		`SELECT id, email, name, role, created_at FROM users WHERE id = $1`,
		id.String(),
	).Scan(&user.ID, &user.Email, &user.Name, &user.Role, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("lookup user by id: %w", err)
	}
	return user, nil
}

func (s *Service) session(user User) (Session, error) {
	token, _, err := s.tokens.Issue(user)
	if err != nil {
		return Session{}, err
	}
	return Session{
		User:        user,
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int64(s.tokens.TTL().Seconds()),
	}, nil
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateEmail(email string) error {
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || parsed.Name != "" || !strings.Contains(email, "@") {
		return ErrInvalidEmail
	}
	return nil
}
