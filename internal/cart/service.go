package cart

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/DharmilG/go-commerce/internal/config"
	"github.com/DharmilG/go-commerce/internal/product"
)

var (
	ErrEmptyCart           = errors.New("cart is empty")
	ErrProductUnavailable  = errors.New("product is not available")
	ErrInsufficientProduct = errors.New("product does not have enough stock")
)

// Service combines Redis quantities with current PostgreSQL product data.
type Service struct {
	store    *Store
	products *product.Store
	cfg      config.Config
}

func NewService(store *Store, products *product.Store, cfg config.Config) *Service {
	return &Service{store: store, products: products, cfg: cfg}
}

func (s *Service) Get(ctx context.Context, userID uuid.UUID) (Cart, error) {
	quantities, ttl, err := s.store.Get(ctx, userID)
	if err != nil {
		return Cart{}, err
	}

	ids := make([]uuid.UUID, 0, len(quantities))
	for raw := range quantities {
		id, err := uuid.Parse(raw)
		if err != nil {
			return Cart{}, fmt.Errorf("cart contains invalid product id %q", raw)
		}
		ids = append(ids, id)
	}

	products, err := s.products.GetMany(ctx, ids)
	if err != nil {
		return Cart{}, err
	}
	byID := make(map[uuid.UUID]product.Product, len(products))
	for _, item := range products {
		byID[item.ID] = item
	}

	cart := Cart{
		Items:            make([]Item, 0, len(quantities)),
		Currency:         s.cfg.Currency,
		ExpiresInSeconds: CartExpiry(ttl),
	}
	for rawID, quantity := range quantities {
		productID, err := uuid.Parse(rawID)
		if err != nil {
			return Cart{}, fmt.Errorf("cart contains invalid product id %q", rawID)
		}
		cart.ItemCount += quantity
		productData, ok := byID[productID]
		if !ok {
			cart.Items = append(cart.Items, Item{
				ProductID: productID,
				Quantity:  quantity,
				Available: false,
			})
			continue
		}

		lineTotal := productData.PriceCents * int64(quantity)
		available := productData.IsActive && productData.Stock >= quantity
		item := Item{
			ProductID:      productID,
			SKU:            productData.SKU,
			Name:           productData.Name,
			UnitPriceCents: productData.PriceCents,
			Quantity:       quantity,
			LineTotalCents: lineTotal,
			Stock:          productData.Stock,
			IsActive:       productData.IsActive,
			Available:      available,
		}
		if available {
			cart.SubtotalCents += lineTotal
		}
		cart.Items = append(cart.Items, item)
	}

	sort.Slice(cart.Items, func(i, j int) bool {
		if cart.Items[i].Name == cart.Items[j].Name {
			return cart.Items[i].ProductID.String() < cart.Items[j].ProductID.String()
		}
		return cart.Items[i].Name < cart.Items[j].Name
	})

	return cart, nil
}

func (s *Service) Add(ctx context.Context, userID, productID uuid.UUID, quantity int) (Cart, error) {
	if quantity < 1 || quantity > s.cfg.MaxItemQuantity {
		return Cart{}, ErrQuantityLimit
	}

	productData, err := s.products.Get(ctx, productID, false)
	if errors.Is(err, product.ErrNotFound) {
		return Cart{}, ErrProductUnavailable
	}
	if err != nil {
		return Cart{}, err
	}
	if productData.Stock < quantity {
		return Cart{}, ErrInsufficientProduct
	}

	maxQuantity := productData.Stock
	if maxQuantity > s.cfg.MaxItemQuantity {
		maxQuantity = s.cfg.MaxItemQuantity
	}
	if err := s.store.Add(ctx, userID, productID, quantity, maxQuantity); err != nil {
		return Cart{}, err
	}
	return s.Get(ctx, userID)
}

func (s *Service) Set(ctx context.Context, userID, productID uuid.UUID, quantity int) (Cart, error) {
	if quantity < 0 || quantity > s.cfg.MaxItemQuantity {
		return Cart{}, ErrQuantityLimit
	}
	if quantity == 0 {
		if err := s.store.Remove(ctx, userID, productID); err != nil {
			return Cart{}, err
		}
		return s.Get(ctx, userID)
	}

	productData, err := s.products.Get(ctx, productID, false)
	if errors.Is(err, product.ErrNotFound) {
		return Cart{}, ErrProductUnavailable
	}
	if err != nil {
		return Cart{}, err
	}
	if productData.Stock < quantity {
		return Cart{}, ErrInsufficientProduct
	}
	if err := s.store.Set(ctx, userID, productID, quantity, productData.Stock); err != nil {
		return Cart{}, err
	}
	return s.Get(ctx, userID)
}

func (s *Service) Remove(ctx context.Context, userID, productID uuid.UUID) (Cart, error) {
	if err := s.store.Remove(ctx, userID, productID); err != nil {
		return Cart{}, err
	}
	return s.Get(ctx, userID)
}

func (s *Service) Clear(ctx context.Context, userID uuid.UUID) error {
	return s.store.Clear(ctx, userID)
}

// Snapshot returns a deterministic cart view for checkout. It deliberately does
// not trust product prices or stock; the order service re-reads those inside a
// database transaction.
func (s *Service) Snapshot(ctx context.Context, userID uuid.UUID) ([]SnapshotItem, error) {
	quantities, _, err := s.store.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(quantities) == 0 {
		return []SnapshotItem{}, nil
	}

	items := make([]SnapshotItem, 0, len(quantities))
	for raw, quantity := range quantities {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("cart contains invalid product id %q", raw)
		}
		items = append(items, SnapshotItem{ProductID: id, Quantity: quantity})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ProductID.String() < items[j].ProductID.String()
	})
	return items, nil
}
