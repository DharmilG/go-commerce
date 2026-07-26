package cart

import (
	"time"

	"github.com/google/uuid"
)

// Item is a cart line enriched with current product data.
type Item struct {
	ProductID      uuid.UUID `json:"product_id"`
	SKU            string    `json:"sku"`
	Name           string    `json:"name"`
	UnitPriceCents int64     `json:"unit_price_cents"`
	Quantity       int       `json:"quantity"`
	LineTotalCents int64     `json:"line_total_cents"`
	Stock          int       `json:"stock"`
	IsActive       bool      `json:"is_active"`
	Available      bool      `json:"available"`
}

// Cart is the API representation of a user's Redis cart.
type Cart struct {
	Items            []Item `json:"items"`
	ItemCount        int    `json:"item_count"`
	SubtotalCents    int64  `json:"subtotal_cents"`
	Currency         string `json:"currency"`
	ExpiresInSeconds int64  `json:"expires_in_seconds"`
}

// SnapshotItem is the minimal data the order service needs from Redis.
type SnapshotItem struct {
	ProductID uuid.UUID
	Quantity  int
}

// Product is the subset of catalog data cart operations need.
type Product struct {
	ID         uuid.UUID
	SKU        string
	Name       string
	PriceCents int64
	Stock      int
	IsActive   bool
}

// CartExpiry is used by tests to reason about the abandoned-cart TTL.
func CartExpiry(ttl time.Duration) int64 {
	if ttl <= 0 {
		return 0
	}
	return int64(ttl.Seconds())
}
