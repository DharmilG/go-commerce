package product

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/DharmilG/go-commerce/internal/httpx"
)

type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
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

	filter := ListFilter{
		Search:   strings.TrimSpace(r.URL.Query().Get("search")),
		Category: strings.TrimSpace(r.URL.Query().Get("category")),
		Limit:    limit,
		Offset:   offset,
	}

	products, total, err := h.store.List(r.Context(), filter)
	if err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not load products", nil)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"data": products,
		"pagination": map[string]int{
			"limit":  limit,
			"offset": offset,
			"total":  total,
		},
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "product id must be a UUID", nil)
		return
	}

	product, err := h.store.Get(r.Context(), id, false)
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "PRODUCT_NOT_FOUND", "Product not found", nil)
		return
	}
	if err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not load product", nil)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{"product": product})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var input CreateInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteDecodeError(w, r, err)
		return
	}
	input.Normalize()
	if err := input.Validate(); err != nil {
		h.writeValidationError(w, r, err)
		return
	}

	product, err := h.store.Create(r.Context(), input)
	if errors.Is(err, ErrSKUExists) {
		httpx.Error(w, r, http.StatusConflict, "SKU_TAKEN", "A product with this SKU already exists", nil)
		return
	}
	if err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not create product", nil)
		return
	}

	httpx.JSON(w, http.StatusCreated, map[string]any{"product": product})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "product id must be a UUID", nil)
		return
	}

	var input UpdateInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteDecodeError(w, r, err)
		return
	}
	input.Normalize()
	if err := input.Validate(); err != nil {
		h.writeValidationError(w, r, err)
		return
	}

	product, err := h.store.Update(r.Context(), id, input)
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "PRODUCT_NOT_FOUND", "Product not found", nil)
		return
	}
	if errors.Is(err, ErrSKUExists) {
		httpx.Error(w, r, http.StatusConflict, "SKU_TAKEN", "A product with this SKU already exists", nil)
		return
	}
	if err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not update product", nil)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{"product": product})
}

func (h *Handler) writeValidationError(w http.ResponseWriter, r *http.Request, err error) {
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", validationErr.Message, map[string]string{
			"field": validationErr.Field,
		})
		return
	}
	httpx.Error(w, r, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), nil)
}

func queryInt(r *http.Request, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback, nil
	}
	return strconv.Atoi(raw)
}
