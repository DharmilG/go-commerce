package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
)

const maxBodyBytes = 1 << 20

var (
	ErrEmptyBody            = errors.New("request body is empty")
	ErrUnsupportedMediaType = errors.New("Content-Type must be application/json")
	ErrBodyTooLarge         = errors.New("request body exceeds 1 MiB")
)

type errorEnvelope struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Details   any    `json:"details,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// JSON writes a JSON response with the supplied status.
func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// Error writes the API's shared error envelope.
func Error(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	JSON(w, status, errorEnvelope{Error: errorDetail{
		Code:      code,
		Message:   message,
		Details:   details,
		RequestID: RequestIDFromContext(r.Context()),
	}})
}

// WriteDecodeError maps JSON decoding failures onto the shared error envelope.
func WriteDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrUnsupportedMediaType):
		Error(w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", ErrUnsupportedMediaType.Error(), nil)
	case errors.Is(err, ErrBodyTooLarge):
		Error(w, r, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", ErrBodyTooLarge.Error(), nil)
	case errors.Is(err, ErrEmptyBody):
		Error(w, r, http.StatusBadRequest, "INVALID_JSON", ErrEmptyBody.Error(), nil)
	default:
		Error(w, r, http.StatusBadRequest, "INVALID_JSON", "Request body must be a single valid JSON object", nil)
	}
}

// DecodeJSON reads a single JSON object, rejects unknown fields, and caps the
// request body size.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		return ErrUnsupportedMediaType
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return ErrUnsupportedMediaType
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return ErrEmptyBody
		}
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return ErrBodyTooLarge
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON object")
	}
	return nil
}
