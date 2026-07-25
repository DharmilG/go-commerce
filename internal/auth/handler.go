package auth

import (
	"errors"
	"net/http"

	"github.com/DharmilG/go-commerce/internal/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type registerRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteDecodeError(w, r, err)
		return
	}

	session, err := h.service.Register(r.Context(), req.Name, req.Email, req.Password)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusCreated, session)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteDecodeError(w, r, err)
		return
	}

	session, err := h.service.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, session)
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required", nil)
		return
	}

	current, err := h.service.GetByID(r.Context(), user.ID)
	if errors.Is(err, ErrUserNotFound) {
		httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required", nil)
		return
	}
	if err != nil {
		h.service.logger.ErrorContext(r.Context(), "load current user failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not load account", nil)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{"user": current})
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var validationErr *ValidationError
	switch {
	case errors.As(err, &validationErr):
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", validationErr.Message, map[string]string{
			"field": validationErr.Field,
		})
	case errors.Is(err, ErrInvalidEmail), errors.Is(err, ErrInvalidName),
		errors.Is(err, ErrPasswordTooShort), errors.Is(err, ErrPasswordTooLong):
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), nil)
	case errors.Is(err, ErrEmailTaken):
		httpx.Error(w, r, http.StatusConflict, "EMAIL_TAKEN", "An account with this email already exists", nil)
	case errors.Is(err, ErrInvalidCredentials):
		httpx.Error(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Email or password is incorrect", nil)
	default:
		h.service.logger.ErrorContext(r.Context(), "auth request failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred", nil)
	}
}
