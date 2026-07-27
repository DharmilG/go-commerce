package order

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DharmilG/go-commerce/internal/cart"
	"github.com/DharmilG/go-commerce/internal/config"
)

var (
	ErrIdempotencyKeyRequired = errors.New("Idempotency-Key header is required")
	ErrInvalidIdempotencyKey  = errors.New("Idempotency-Key must be 8-255 visible ASCII characters")
	ErrIdempotencyConflict    = errors.New("Idempotency-Key was already used with a different request")
	ErrCartEmpty              = errors.New("cart is empty")
	ErrProductUnavailable     = errors.New("a cart product is no longer available")
	ErrInsufficientStock      = errors.New("insufficient stock")
	ErrOrderNotFound          = errors.New("order not found")
)

// StockError describes the exact product that could not be purchased.
type StockError struct {
	ProductID uuid.UUID
	Requested int
	Available int
}

func (e *StockError) Error() string {
	return fmt.Sprintf("product %s has %d units available, requested %d", e.ProductID, e.Available, e.Requested)
}

func (e *StockError) Unwrap() error {
	return ErrInsufficientStock
}

// ProductUnavailableError identifies the product that disappeared or became
// inactive between cart and checkout.
type ProductUnavailableError struct {
	ProductID uuid.UUID
}

func (e *ProductUnavailableError) Error() string {
	return "product " + e.ProductID.String() + " is not available"
}

func (e *ProductUnavailableError) Unwrap() error {
	return ErrProductUnavailable
}

// EventPublisher is implemented by the worker pool.
type EventPublisher interface {
	PublishOrderCreated(context.Context, Order)
}

// Service owns checkout and order reads.
type Service struct {
	db     *pgxpool.Pool
	cart   *cart.Service
	cfg    config.Config
	logger *slog.Logger
	events EventPublisher
	now    func() time.Time
}

func NewService(db *pgxpool.Pool, cartService *cart.Service, cfg config.Config, logger *slog.Logger, events EventPublisher) *Service {
	return &Service{
		db:     db,
		cart:   cartService,
		cfg:    cfg,
		logger: logger,
		events: events,
		now:    time.Now,
	}
}

// Place checks out the user's cart exactly once for a given idempotency key.
//
// The per-user advisory lock serializes duplicate checkouts from the same user.
// Product rows are then locked in UUID order, which prevents deadlocks between
// different users buying overlapping products. Stock is decremented with a
// guarded UPDATE and the idempotency record is committed in the same
// transaction as the order.
func (s *Service) Place(ctx context.Context, userID uuid.UUID, key string, req CreateRequest) (PlaceResult, error) {
	if err := ValidateIdempotencyKey(key); err != nil {
		return PlaceResult{}, err
	}
	req.ShippingAddress.Normalize()
	if err := req.ShippingAddress.Validate(); err != nil {
		return PlaceResult{}, err
	}

	requestHash, err := hashRequest(req)
	if err != nil {
		return PlaceResult{}, err
	}

	// Fast path: a completed retry does not need a database transaction.
	if record, err := s.findIdempotency(ctx, s.db, userID, key); err == nil {
		return s.replay(record, requestHash)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return PlaceResult{}, fmt.Errorf("lookup idempotency record: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PlaceResult{}, fmt.Errorf("begin order transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"checkout:"+userID.String(),
	); err != nil {
		return PlaceResult{}, fmt.Errorf("acquire checkout lock: %w", err)
	}

	// Re-check after taking the lock. A concurrent request may have completed
	// while this request was waiting.
	if record, err := s.findIdempotency(ctx, tx, userID, key); err == nil {
		return s.replay(record, requestHash)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return PlaceResult{}, fmt.Errorf("lookup idempotency record in transaction: %w", err)
	}

	snapshot, err := s.cart.Snapshot(ctx, userID)
	if err != nil {
		return PlaceResult{}, fmt.Errorf("snapshot cart: %w", err)
	}
	if len(snapshot) == 0 {
		return PlaceResult{}, ErrCartEmpty
	}

	ids := make([]string, len(snapshot))
	for i, item := range snapshot {
		ids[i] = item.ProductID.String()
	}

	rows, err := tx.Query(ctx,
		`SELECT id, sku, name, price_cents, stock, is_active
		 FROM products
		 WHERE id = ANY($1::uuid[])
		 ORDER BY id
		 FOR UPDATE`,
		ids,
	)
	if err != nil {
		return PlaceResult{}, fmt.Errorf("lock products: %w", err)
	}

	type lockedProduct struct {
		ID         uuid.UUID
		SKU        string
		Name       string
		PriceCents int64
		Stock      int
		IsActive   bool
	}
	products := make(map[uuid.UUID]lockedProduct, len(snapshot))
	for rows.Next() {
		var (
			product lockedProduct
			rawID   string
		)
		if err := rows.Scan(&rawID, &product.SKU, &product.Name, &product.PriceCents, &product.Stock, &product.IsActive); err != nil {
			rows.Close()
			return PlaceResult{}, fmt.Errorf("scan locked product: %w", err)
		}
		product.ID, err = uuid.Parse(rawID)
		if err != nil {
			rows.Close()
			return PlaceResult{}, fmt.Errorf("parse locked product id: %w", err)
		}
		products[product.ID] = product
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return PlaceResult{}, fmt.Errorf("iterate locked products: %w", err)
	}
	rows.Close()

	orderID := uuid.New()
	order := Order{
		ID:              orderID,
		UserID:          userID,
		Status:          "confirmed",
		Currency:        s.cfg.Currency,
		ShippingAddress: req.ShippingAddress,
		Items:           make([]Item, 0, len(snapshot)),
	}
	for _, wanted := range snapshot {
		product, ok := products[wanted.ProductID]
		if !ok || !product.IsActive {
			return PlaceResult{}, &ProductUnavailableError{ProductID: wanted.ProductID}
		}
		if product.Stock < wanted.Quantity {
			return PlaceResult{}, &StockError{
				ProductID: wanted.ProductID,
				Requested: wanted.Quantity,
				Available: product.Stock,
			}
		}
		if product.PriceCents > 0 && int64(wanted.Quantity) > math.MaxInt64/product.PriceCents {
			return PlaceResult{}, fmt.Errorf("line total overflow for product %s", wanted.ProductID)
		}

		lineTotal := product.PriceCents * int64(wanted.Quantity)
		order.SubtotalCents += lineTotal
		order.Items = append(order.Items, Item{
			ID:             uuid.New(),
			ProductID:      product.ID,
			SKU:            product.SKU,
			Name:           product.Name,
			UnitPriceCents: product.PriceCents,
			Quantity:       wanted.Quantity,
			LineTotalCents: lineTotal,
		})
	}
	order.ShippingCents = 0
	order.TotalCents = order.SubtotalCents

	addressJSON, err := json.Marshal(order.ShippingAddress)
	if err != nil {
		return PlaceResult{}, fmt.Errorf("marshal shipping address: %w", err)
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO orders
		     (id, user_id, status, subtotal_cents, shipping_cents, total_cents, currency, shipping_address)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
		 RETURNING created_at, updated_at`,
		order.ID.String(), order.UserID.String(), order.Status, order.SubtotalCents,
		order.ShippingCents, order.TotalCents, order.Currency, string(addressJSON),
	).Scan(&order.CreatedAt, &order.UpdatedAt)
	if err != nil {
		return PlaceResult{}, fmt.Errorf("insert order: %w", err)
	}

	for _, item := range order.Items {
		if _, err := tx.Exec(ctx,
			`INSERT INTO order_items
			     (id, order_id, product_id, sku, product_name, unit_price_cents, quantity, line_total_cents)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			item.ID.String(), order.ID.String(), item.ProductID.String(), item.SKU, item.Name,
			item.UnitPriceCents, item.Quantity, item.LineTotalCents,
		); err != nil {
			return PlaceResult{}, fmt.Errorf("insert order item: %w", err)
		}

		tag, err := tx.Exec(ctx,
			`UPDATE products
			 SET stock = stock - $1, updated_at = now()
			 WHERE id = $2::uuid AND stock >= $1`,
			item.Quantity, item.ProductID.String(),
		)
		if err != nil {
			return PlaceResult{}, fmt.Errorf("decrement stock: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return PlaceResult{}, &StockError{
				ProductID: item.ProductID,
				Requested: item.Quantity,
			}
		}
	}

	responseBody, err := json.Marshal(struct {
		Order Order `json:"order"`
	}{Order: order})
	if err != nil {
		return PlaceResult{}, fmt.Errorf("marshal order response: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM idempotency_keys WHERE user_id = $1 AND key = $2 AND expires_at <= now()`,
		userID.String(), key,
	); err != nil {
		return PlaceResult{}, fmt.Errorf("remove expired idempotency record: %w", err)
	}

	expiresAt := s.now().Add(s.cfg.IdempotencyTTL)
	if _, err := tx.Exec(ctx,
		`INSERT INTO idempotency_keys
		     (user_id, key, request_hash, order_id, response_status, response_body, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)`,
		userID.String(), key, requestHash, order.ID.String(), 201, string(responseBody), expiresAt,
	); err != nil {
		return PlaceResult{}, fmt.Errorf("store idempotency record: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return PlaceResult{}, fmt.Errorf("commit order: %w", err)
	}

	// The order is committed; the cart cleanup is best-effort. If Redis is
	// temporarily down, an idempotent retry still returns the committed order.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := s.cart.Clear(cleanupCtx, userID); err != nil {
		s.logger.WarnContext(cleanupCtx, "could not clear cart after order", "order_id", order.ID, "error", err)
	}
	if s.events != nil {
		s.events.PublishOrderCreated(cleanupCtx, order)
	}

	return PlaceResult{
		Order:    order,
		Replayed: false,
		Status:   httpStatusCreated,
		Body:     responseBody,
	}, nil
}

const httpStatusCreated = 201

// ValidateIdempotencyKey rejects empty, oversized, and whitespace-bearing keys.
func ValidateIdempotencyKey(key string) error {
	if key == "" {
		return ErrIdempotencyKeyRequired
	}
	if len(key) < 8 || len(key) > 255 {
		return ErrInvalidIdempotencyKey
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 33 || key[i] > 126 {
			return ErrInvalidIdempotencyKey
		}
	}
	return nil
}

func hashRequest(req CreateRequest) (string, error) {
	payload, err := json.Marshal(struct {
		ShippingAddress Address `json:"shipping_address"`
	}{ShippingAddress: req.ShippingAddress})
	if err != nil {
		return "", fmt.Errorf("marshal idempotency payload: %w", err)
	}
	sum := sha256.Sum256(append([]byte("v1:"), payload...))
	return hex.EncodeToString(sum[:]), nil
}

type idempotencyRecord struct {
	RequestHash    string
	OrderID        string
	ResponseStatus int
	ResponseBody   []byte
	ExpiresAt      time.Time
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Service) findIdempotency(ctx context.Context, query queryRower, userID uuid.UUID, key string) (idempotencyRecord, error) {
	var record idempotencyRecord
	err := query.QueryRow(ctx,
		`SELECT request_hash, order_id, response_status, response_body, expires_at
		 FROM idempotency_keys
		 WHERE user_id = $1 AND key = $2 AND expires_at > now()`,
		userID.String(), key,
	).Scan(&record.RequestHash, &record.OrderID, &record.ResponseStatus, &record.ResponseBody, &record.ExpiresAt)
	if err != nil {
		return idempotencyRecord{}, err
	}
	return record, nil
}

func (s *Service) replay(record idempotencyRecord, requestHash string) (PlaceResult, error) {
	if record.RequestHash != requestHash {
		return PlaceResult{}, ErrIdempotencyConflict
	}
	return PlaceResult{
		Replayed: true,
		Status:   record.ResponseStatus,
		Body:     record.ResponseBody,
	}, nil
}

// List returns a page of the user's orders.
func (s *Service) List(ctx context.Context, userID uuid.UUID, limit, offset int) (ListResult, error) {
	var total int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE user_id = $1`, userID.String()).Scan(&total); err != nil {
		return ListResult{}, fmt.Errorf("count orders: %w", err)
	}

	rows, err := s.db.Query(ctx,
		`SELECT id, user_id, status, subtotal_cents, shipping_cents, total_cents, currency,
		        shipping_address, created_at, updated_at
		 FROM orders
		 WHERE user_id = $1
		 ORDER BY created_at DESC, id DESC
		 LIMIT $2 OFFSET $3`,
		userID.String(), limit, offset,
	)
	if err != nil {
		return ListResult{}, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()

	orders := make([]Order, 0, limit)
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return ListResult{}, err
		}
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, fmt.Errorf("iterate orders: %w", err)
	}

	if err := s.attachItems(ctx, orders); err != nil {
		return ListResult{}, err
	}
	return ListResult{Orders: orders, Total: total}, nil
}

// Get returns one order. Admins may read any order; customers only their own.
func (s *Service) Get(ctx context.Context, userID uuid.UUID, orderID uuid.UUID, isAdmin bool) (Order, error) {
	query := `SELECT id, user_id, status, subtotal_cents, shipping_cents, total_cents, currency,
	                 shipping_address, created_at, updated_at
	          FROM orders WHERE id = $1`
	args := []any{orderID.String()}
	if !isAdmin {
		query += " AND user_id = $2"
		args = append(args, userID.String())
	}

	order, err := scanOrder(s.db.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}
	if err != nil {
		return Order{}, err
	}

	orders := []Order{order}
	if err := s.attachItems(ctx, orders); err != nil {
		return Order{}, err
	}
	return orders[0], nil
}

func (s *Service) attachItems(ctx context.Context, orders []Order) error {
	if len(orders) == 0 {
		return nil
	}
	ids := make([]string, len(orders))
	for i, order := range orders {
		ids[i] = order.ID.String()
	}

	rows, err := s.db.Query(ctx,
		`SELECT id, order_id, product_id, sku, product_name, unit_price_cents, quantity, line_total_cents
		 FROM order_items
		 WHERE order_id = ANY($1::uuid[])
		 ORDER BY product_name, id`,
		ids,
	)
	if err != nil {
		return fmt.Errorf("load order items: %w", err)
	}
	defer rows.Close()

	byOrder := make(map[string][]Item, len(orders))
	for rows.Next() {
		var (
			item      Item
			id        string
			orderID   string
			productID string
		)
		if err := rows.Scan(&id, &orderID, &productID, &item.SKU, &item.Name,
			&item.UnitPriceCents, &item.Quantity, &item.LineTotalCents); err != nil {
			return fmt.Errorf("scan order item: %w", err)
		}
		itemID, err := uuid.Parse(id)
		if err != nil {
			return fmt.Errorf("parse order item id: %w", err)
		}
		item.ProductID, err = uuid.Parse(productID)
		if err != nil {
			return fmt.Errorf("parse order item product id: %w", err)
		}
		item.ID = itemID
		byOrder[orderID] = append(byOrder[orderID], item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate order items: %w", err)
	}

	for i := range orders {
		items := byOrder[orders[i].ID.String()]
		if items == nil {
			items = []Item{}
		}
		orders[i].Items = items
	}
	return nil
}

func scanOrder(row pgx.Row) (Order, error) {
	var (
		order       Order
		id          string
		userID      string
		addressJSON []byte
	)
	if err := row.Scan(&id, &userID, &order.Status, &order.SubtotalCents, &order.ShippingCents,
		&order.TotalCents, &order.Currency, &addressJSON, &order.CreatedAt, &order.UpdatedAt); err != nil {
		return Order{}, err
	}
	var err error
	order.ID, err = uuid.Parse(id)
	if err != nil {
		return Order{}, fmt.Errorf("parse order id: %w", err)
	}
	order.UserID, err = uuid.Parse(userID)
	if err != nil {
		return Order{}, fmt.Errorf("parse order user id: %w", err)
	}
	if err := json.Unmarshal(addressJSON, &order.ShippingAddress); err != nil {
		return Order{}, fmt.Errorf("decode shipping address: %w", err)
	}
	order.Items = []Item{}
	return order, nil
}

// CleanupExpired removes idempotency records whose replay window has closed.
func (s *Service) CleanupExpired(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM idempotency_keys WHERE expires_at < now()`)
	if err != nil {
		return 0, fmt.Errorf("cleanup idempotency records: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CleanupLoop is a small janitor for expired idempotency keys.
func (s *Service) CleanupLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if removed, err := s.CleanupExpired(ctx); err != nil {
				s.logger.WarnContext(ctx, "idempotency cleanup failed", "error", err)
			} else if removed > 0 {
				s.logger.InfoContext(ctx, "removed expired idempotency keys", "count", removed)
			}
		}
	}
}
