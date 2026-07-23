package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONAcceptsSingleObject(t *testing.T) {
	body := bytes.NewBufferString(`{"name":"learner","active":true}`)
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	recorder := httptest.NewRecorder()

	var dst struct {
		Name   string `json:"name"`
		Active bool   `json:"active"`
	}
	if err := DecodeJSON(recorder, req, &dst); err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}
	if dst.Name != "learner" || !dst.Active {
		t.Fatalf("decoded body = %+v", dst)
	}
}

func TestDecodeJSONRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantErr     error
	}{
		{name: "missing content type", body: `{}`, wantErr: ErrUnsupportedMediaType},
		{name: "wrong content type", contentType: "text/plain", body: `{}`, wantErr: ErrUnsupportedMediaType},
		{name: "empty body", contentType: "application/json", body: "", wantErr: ErrEmptyBody},
		{name: "unknown field", contentType: "application/json", body: `{"unknown":true}`},
		{name: "trailing object", contentType: "application/json", body: `{} {}`},
		{name: "malformed json", contentType: "application/json", body: `{"name":`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			recorder := httptest.NewRecorder()

			var dst struct {
				Name string `json:"name"`
			}
			err := DecodeJSON(recorder, req, &dst)
			if err == nil {
				t.Fatal("DecodeJSON() accepted an invalid body")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("DecodeJSON() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestDecodeJSONRejectsOversizedBody(t *testing.T) {
	body := `{"payload":"` + strings.Repeat("a", maxBodyBytes+1) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	var dst map[string]any
	err := DecodeJSON(recorder, req, &dst)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("DecodeJSON() error = %v, want ErrBodyTooLarge", err)
	}
}

func TestWriteDecodeErrorMapsStatusAndEnvelope(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "unsupported media type", err: ErrUnsupportedMediaType, status: http.StatusUnsupportedMediaType, code: "UNSUPPORTED_MEDIA_TYPE"},
		{name: "body too large", err: ErrBodyTooLarge, status: http.StatusRequestEntityTooLarge, code: "BODY_TOO_LARGE"},
		{name: "empty body", err: ErrEmptyBody, status: http.StatusBadRequest, code: "INVALID_JSON"},
		{name: "generic decode error", err: errors.New("bad json"), status: http.StatusBadRequest, code: "INVALID_JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req = req.WithContext(WithRequestID(req.Context(), "req-test"))
			recorder := httptest.NewRecorder()
			WriteDecodeError(recorder, req, tt.err)

			if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.status)
			}
			var envelope struct {
				Error struct {
					Code      string `json:"code"`
					RequestID string `json:"request_id"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if envelope.Error.Code != tt.code || envelope.Error.RequestID != "req-test" {
				t.Fatalf("envelope = %+v, want code=%s request_id=req-test", envelope, tt.code)
			}
		})
	}
}

func TestRequestIDMiddlewareRejectsUnsafeInput(t *testing.T) {
	var seen string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "safe-request-id")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	if seen != "safe-request-id" {
		t.Fatalf("request id in context = %q", seen)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "unsafe\r\nheader")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	generated := recorder.Header().Get("X-Request-ID")
	if generated == "" || generated == "unsafe\r\nheader" {
		t.Fatalf("unsafe request id was not replaced: %q", generated)
	}
	if seen != generated {
		t.Fatalf("context request id = %q, response header = %q", seen, generated)
	}
}
