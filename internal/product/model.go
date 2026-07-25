package product

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrNotFound  = errors.New("product not found")
	ErrSKUExists = errors.New("a product with this SKU already exists")
)

// ValidationError describes an invalid product field.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// Product is the public catalog representation.
type Product struct {
	ID          uuid.UUID `json:"id"`
	SKU         string    `json:"sku"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	PriceCents  int64     `json:"price_cents"`
	Stock       int       `json:"stock"`
	Category    string    `json:"category"`
	ImageURL    string    `json:"image_url"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateInput is the admin payload for a new product.
type CreateInput struct {
	SKU         string `json:"sku"`
	Name        string `json:"name"`
	Description string `json:"description"`
	PriceCents  int64  `json:"price_cents"`
	Stock       int    `json:"stock"`
	Category    string `json:"category"`
	ImageURL    string `json:"image_url"`
	IsActive    *bool  `json:"is_active"`
}

// UpdateInput uses pointers so omitted fields remain unchanged.
type UpdateInput struct {
	SKU         *string `json:"sku"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	PriceCents  *int64  `json:"price_cents"`
	Stock       *int    `json:"stock"`
	Category    *string `json:"category"`
	ImageURL    *string `json:"image_url"`
	IsActive    *bool   `json:"is_active"`
}

func (in *CreateInput) Normalize() {
	in.SKU = strings.TrimSpace(in.SKU)
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Category = strings.TrimSpace(in.Category)
	in.ImageURL = strings.TrimSpace(in.ImageURL)
}

func (in *CreateInput) Validate() error {
	if in.SKU == "" || utf8.RuneCountInString(in.SKU) > 64 {
		return &ValidationError{Field: "sku", Message: "sku is required and must be at most 64 characters"}
	}
	if utf8.RuneCountInString(in.Name) < 2 || utf8.RuneCountInString(in.Name) > 200 {
		return &ValidationError{Field: "name", Message: "name must be between 2 and 200 characters"}
	}
	if in.PriceCents < 0 {
		return &ValidationError{Field: "price_cents", Message: "price_cents cannot be negative"}
	}
	if in.Stock < 0 {
		return &ValidationError{Field: "stock", Message: "stock cannot be negative"}
	}
	if utf8.RuneCountInString(in.Category) > 100 {
		return &ValidationError{Field: "category", Message: "category must be at most 100 characters"}
	}
	return nil
}

func (in *UpdateInput) Normalize() {
	if in.SKU != nil {
		value := strings.TrimSpace(*in.SKU)
		in.SKU = &value
	}
	if in.Name != nil {
		value := strings.TrimSpace(*in.Name)
		in.Name = &value
	}
	if in.Description != nil {
		value := strings.TrimSpace(*in.Description)
		in.Description = &value
	}
	if in.Category != nil {
		value := strings.TrimSpace(*in.Category)
		in.Category = &value
	}
	if in.ImageURL != nil {
		value := strings.TrimSpace(*in.ImageURL)
		in.ImageURL = &value
	}
}

func (in *UpdateInput) Validate() error {
	if in.SKU == nil && in.Name == nil && in.Description == nil && in.PriceCents == nil &&
		in.Stock == nil && in.Category == nil && in.ImageURL == nil && in.IsActive == nil {
		return &ValidationError{Field: "body", Message: "at least one field must be provided"}
	}
	if in.SKU != nil && (*in.SKU == "" || utf8.RuneCountInString(*in.SKU) > 64) {
		return &ValidationError{Field: "sku", Message: "sku must be between 1 and 64 characters"}
	}
	if in.Name != nil && (utf8.RuneCountInString(*in.Name) < 2 || utf8.RuneCountInString(*in.Name) > 200) {
		return &ValidationError{Field: "name", Message: "name must be between 2 and 200 characters"}
	}
	if in.PriceCents != nil && *in.PriceCents < 0 {
		return &ValidationError{Field: "price_cents", Message: "price_cents cannot be negative"}
	}
	if in.Stock != nil && *in.Stock < 0 {
		return &ValidationError{Field: "stock", Message: "stock cannot be negative"}
	}
	if in.Category != nil && utf8.RuneCountInString(*in.Category) > 100 {
		return &ValidationError{Field: "category", Message: "category must be at most 100 characters"}
	}
	return nil
}
