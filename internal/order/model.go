package order

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Address is the shipping destination stored as JSONB with the order.
type Address struct {
	FullName   string `json:"full_name"`
	Line1      string `json:"line1"`
	Line2      string `json:"line2,omitempty"`
	City       string `json:"city"`
	State      string `json:"state,omitempty"`
	PostalCode string `json:"postal_code"`
	Country    string `json:"country"`
}

func (a *Address) Normalize() {
	a.FullName = strings.TrimSpace(a.FullName)
	a.Line1 = strings.TrimSpace(a.Line1)
	a.Line2 = strings.TrimSpace(a.Line2)
	a.City = strings.TrimSpace(a.City)
	a.State = strings.TrimSpace(a.State)
	a.PostalCode = strings.TrimSpace(a.PostalCode)
	a.Country = strings.TrimSpace(a.Country)
}

func (a Address) Validate() error {
	switch {
	case a.FullName == "" || len(a.FullName) > 100:
		return &ValidationError{Field: "shipping_address.full_name", Message: "full_name is required and must be at most 100 characters"}
	case a.Line1 == "" || len(a.Line1) > 200:
		return &ValidationError{Field: "shipping_address.line1", Message: "line1 is required and must be at most 200 characters"}
	case len(a.Line2) > 200:
		return &ValidationError{Field: "shipping_address.line2", Message: "line2 must be at most 200 characters"}
	case a.City == "" || len(a.City) > 100:
		return &ValidationError{Field: "shipping_address.city", Message: "city is required and must be at most 100 characters"}
	case len(a.State) > 100:
		return &ValidationError{Field: "shipping_address.state", Message: "state must be at most 100 characters"}
	case a.PostalCode == "" || len(a.PostalCode) > 30:
		return &ValidationError{Field: "shipping_address.postal_code", Message: "postal_code is required and must be at most 30 characters"}
	case len(a.Country) < 2 || len(a.Country) > 100:
		return &ValidationError{Field: "shipping_address.country", Message: "country is required and must be between 2 and 100 characters"}
	default:
		return nil
	}
}

// ValidationError is a user-correctable request problem.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// Item is a price and name snapshot captured when the order is placed.
type Item struct {
	ID             uuid.UUID `json:"id"`
	ProductID      uuid.UUID `json:"product_id"`
	SKU            string    `json:"sku"`
	Name           string    `json:"name"`
	UnitPriceCents int64     `json:"unit_price_cents"`
	Quantity       int       `json:"quantity"`
	LineTotalCents int64     `json:"line_total_cents"`
}

// Order is the API representation of a placed order.
type Order struct {
	ID              uuid.UUID `json:"id"`
	UserID          uuid.UUID `json:"user_id"`
	Status          string    `json:"status"`
	SubtotalCents   int64     `json:"subtotal_cents"`
	ShippingCents   int64     `json:"shipping_cents"`
	TotalCents      int64     `json:"total_cents"`
	Currency        string    `json:"currency"`
	ShippingAddress Address   `json:"shipping_address"`
	Items           []Item    `json:"items"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// CreateRequest is the body of POST /orders. Items always come from the
// authenticated user's Redis cart.
type CreateRequest struct {
	ShippingAddress Address `json:"shipping_address"`
}

// PlaceResult carries the exact response that must be replayed for retries.
type PlaceResult struct {
	Order    Order
	Replayed bool
	Status   int
	Body     []byte
}

// ListResult is a page of a user's orders.
type ListResult struct {
	Orders []Order
	Total  int
}
