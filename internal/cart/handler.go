package cart

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/DharmilG/go-commerce/internal/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required", nil)
		return
	}

	cart, err := h.service.Get(r.Context(), user.ID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"cart": cart})
}

type addRequest struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

func (h *Handler) Add(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required", nil)
		return
	}

	var req addRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteDecodeError(w, r, err)
		return
	}
	productID, err := uuid.Parse(req.ProductID)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "product_id must be a UUID", nil)
		return
	}
	if req.Quantity < 1 {
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "quantity must be at least 1", nil)
		return
	}

	cart, err := h.service.Add(r.Context(), user.ID, productID, req.Quantity)
	if err != nil {
		if errors.Is(err, ErrInsufficientProduct) {
			httpx.Error(w, r, http.StatusConflict, "INSUFFICIENT_STOCK", "Not enough stock for this product", nil)
			return
		}
		h.writeError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"cart": cart})
}

type setRequest struct {
	Quantity int `json:"quantity"`
}

func (h *Handler) Set(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required", nil)
		return
	}

	productID, err := uuid.Parse(r.PathValue("productID"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "product id must be a UUID", nil)
		return
	}

	var req setRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteDecodeError(w, r, err)
		return
	}
	if req.Quantity < 0 {
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "quantity cannot be negative", nil)
		return
	}

	cart, err := h.service.Set(r.Context(), user.ID, productID, req.Quantity)
	if err != nil {
		if errors.Is(err, ErrInsufficientProduct) {
			httpx.Error(w, r, http.StatusConflict, "INSUFFICIENT_STOCK", "Not enough stock for this product", nil)
			return
		}
		h.writeError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"cart": cart})
}

func (h *Handler) Remove(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required", nil)
		return
	}

	productID, err := uuid.Parse(r.PathValue("productID"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "product id must be a UUID", nil)
		return
	}

	cart, err := h.service.Remove(r.Context(), user.ID, productID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"cart": cart})
}

func (h *Handler) Clear(w http.ResponseWriter, r *http.Request) {
	user, ok := httpx.UserFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required", nil)
		return
	}

	if err := h.service.Clear(r.Context(), user.ID); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrProductUnavailable):
		httpx.Error(w, r, http.StatusNotFound, "PRODUCT_NOT_FOUND", "Product is not available", nil)
	case errors.Is(err, ErrQuantityLimit):
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), nil)
	case errors.Is(err, ErrCartFull):
		httpx.Error(w, r, http.StatusConflict, "CART_FULL", err.Error(), nil)
	case errors.Is(err, ErrInvalidQuantity):
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), nil)
	default:
		httpx.Error(w, r, http.StatusServiceUnavailable, "CART_UNAVAILABLE", "The cart service is temporarily unavailable", nil)
	}
}
