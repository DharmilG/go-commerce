package order

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/DharmilG/go-commerce/internal/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Place(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required", nil)
		return
	}

	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		httpx.Error(w, r, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key header is required", nil)
		return
	}

	var req CreateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteDecodeError(w, r, err)
		return
	}

	result, err := h.service.Place(r.Context(), user.ID, key, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if result.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	w.WriteHeader(result.Status)
	_, _ = w.Write(result.Body)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required", nil)
		return
	}

	limit, err := queryInt(r, "limit", 20)
	if err != nil || limit < 1 || limit > 100 {
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "limit must be between 1 and 100", nil)
		return
	}
	offset, err := queryInt(r, "offset", 0)
	if err != nil || offset < 0 {
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "offset must be zero or greater", nil)
		return
	}

	result, err := h.service.List(r.Context(), user.ID, limit, offset)
	if err != nil {
		h.service.logger.ErrorContext(r.Context(), "list orders failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not load orders", nil)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"data": result.Orders,
		"pagination": map[string]int{
			"limit":  limit,
			"offset": offset,
			"total":  result.Total,
		},
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required", nil)
		return
	}

	orderID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "order id must be a UUID", nil)
		return
	}

	order, err := h.service.Get(r.Context(), user.ID, orderID, user.Role == "admin")
	if errors.Is(err, ErrOrderNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "ORDER_NOT_FOUND", "Order not found", nil)
		return
	}
	if err != nil {
		h.service.logger.ErrorContext(r.Context(), "get order failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not load order", nil)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{"order": order})
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var validationErr *ValidationError
	var stockErr *StockError
	var productErr *ProductUnavailableError

	switch {
	case errors.As(err, &validationErr):
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", validationErr.Message, map[string]string{
			"field": validationErr.Field,
		})
	case errors.Is(err, ErrIdempotencyKeyRequired):
		httpx.Error(w, r, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", err.Error(), nil)
	case errors.Is(err, ErrInvalidIdempotencyKey):
		httpx.Error(w, r, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error(), nil)
	case errors.Is(err, ErrIdempotencyConflict):
		httpx.Error(w, r, http.StatusConflict, "IDEMPOTENCY_CONFLICT", err.Error(), nil)
	case errors.Is(err, ErrCartEmpty):
		httpx.Error(w, r, http.StatusConflict, "CART_EMPTY", "Cannot place an order with an empty cart", nil)
	case errors.As(err, &stockErr):
		httpx.Error(w, r, http.StatusConflict, "INSUFFICIENT_STOCK", "One or more items do not have enough stock", map[string]any{
			"product_id": stockErr.ProductID,
			"requested":  stockErr.Requested,
			"available":  stockErr.Available,
		})
	case errors.As(err, &productErr):
		httpx.Error(w, r, http.StatusConflict, "PRODUCT_UNAVAILABLE", "One or more cart products are no longer available", map[string]any{
			"product_id": productErr.ProductID,
		})
	default:
		h.service.logger.ErrorContext(r.Context(), "place order failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not place order", nil)
	}
}

func queryInt(r *http.Request, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback, nil
	}
	return strconv.Atoi(raw)
}
