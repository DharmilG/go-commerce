package cart

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var (
	ErrCartFull        = errors.New("cart already contains the maximum number of distinct items")
	ErrQuantityLimit   = errors.New("requested quantity exceeds the allowed maximum")
	ErrInvalidQuantity = errors.New("quantity must be positive")
)

// Store owns the Redis hash representation of a cart.
type Store struct {
	client   *redis.Client
	prefix   string
	ttl      time.Duration
	maxItems int
	maxQty   int
}

func NewStore(client *redis.Client, prefix string, ttl time.Duration, maxItems, maxQty int) *Store {
	return &Store{
		client:   client,
		prefix:   prefix,
		ttl:      ttl,
		maxItems: maxItems,
		maxQty:   maxQty,
	}
}

func (s *Store) key(userID uuid.UUID) string {
	return s.prefix + "cart:" + userID.String()
}

// Get returns quantity-by-product and the remaining TTL.
func (s *Store) Get(ctx context.Context, userID uuid.UUID) (map[string]int, time.Duration, error) {
	key := s.key(userID)
	values, err := s.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("read cart: %w", err)
	}

	items := make(map[string]int, len(values))
	for productID, raw := range values {
		quantity, err := strconv.Atoi(raw)
		if err != nil || quantity <= 0 {
			return nil, 0, fmt.Errorf("cart contains invalid quantity for %s", productID)
		}
		items[productID] = quantity
	}

	ttl, err := s.client.TTL(ctx, key).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("read cart ttl: %w", err)
	}
	if ttl < 0 {
		ttl = 0
	}
	return items, ttl, nil
}

// Add increments a product quantity and refreshes the TTL atomically.
func (s *Store) Add(ctx context.Context, userID, productID uuid.UUID, delta, maxQuantity int) error {
	if delta <= 0 {
		return ErrInvalidQuantity
	}
	if maxQuantity < 1 {
		maxQuantity = 1
	}
	if maxQuantity > s.maxQty {
		maxQuantity = s.maxQty
	}

	result, err := addScript.Run(ctx, s.client, []string{s.key(userID)},
		productID.String(), delta, maxQuantity, s.maxItems, int64(s.ttl.Seconds()),
	).Slice()
	if err != nil {
		return fmt.Errorf("add cart item: %w", err)
	}
	return cartScriptError(result)
}

// Set stores an absolute quantity. A zero quantity removes the item.
func (s *Store) Set(ctx context.Context, userID, productID uuid.UUID, quantity, maxQuantity int) error {
	if quantity < 0 {
		return ErrInvalidQuantity
	}
	if maxQuantity < 1 {
		maxQuantity = 1
	}
	if maxQuantity > s.maxQty {
		maxQuantity = s.maxQty
	}

	result, err := setScript.Run(ctx, s.client, []string{s.key(userID)},
		productID.String(), quantity, maxQuantity, s.maxItems, int64(s.ttl.Seconds()),
	).Slice()
	if err != nil {
		return fmt.Errorf("set cart item: %w", err)
	}
	return cartScriptError(result)
}

// Remove deletes one product and refreshes or removes the cart TTL.
func (s *Store) Remove(ctx context.Context, userID, productID uuid.UUID) error {
	_, err := removeScript.Run(ctx, s.client, []string{s.key(userID)},
		productID.String(), int64(s.ttl.Seconds()),
	).Result()
	if err != nil {
		return fmt.Errorf("remove cart item: %w", err)
	}
	return nil
}

// Clear deletes the entire cart.
func (s *Store) Clear(ctx context.Context, userID uuid.UUID) error {
	if err := s.client.Del(ctx, s.key(userID)).Err(); err != nil {
		return fmt.Errorf("clear cart: %w", err)
	}
	return nil
}

func cartScriptError(result []any) error {
	if len(result) == 0 {
		return errors.New("cart script returned no status")
	}
	status, ok := asInt64(result[0])
	if !ok {
		return errors.New("cart script returned a non-integer status")
	}
	switch status {
	case -1:
		return ErrQuantityLimit
	case -2:
		return ErrCartFull
	case -3:
		return ErrInvalidQuantity
	default:
		return nil
	}
}

func asInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	default:
		return 0, false
	}
}

var addScript = redis.NewScript(`
local key = KEYS[1]
local field = ARGV[1]
local delta = tonumber(ARGV[2])
local maxQuantity = tonumber(ARGV[3])
local maxItems = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])

local current = tonumber(redis.call('HGET', key, field) or '0')
local exists = redis.call('HEXISTS', key, field)
if exists == 0 and redis.call('HLEN', key) >= maxItems then
  return {-2, current}
end

local nextQuantity = current + delta
if nextQuantity < 1 then
  return {-3, current}
end
if nextQuantity > maxQuantity then
  return {-1, current}
end

redis.call('HSET', key, field, nextQuantity)
redis.call('EXPIRE', key, ttl)
return {nextQuantity, current}
`)

var setScript = redis.NewScript(`
local key = KEYS[1]
local field = ARGV[1]
local quantity = tonumber(ARGV[2])
local maxQuantity = tonumber(ARGV[3])
local maxItems = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])

if quantity < 0 then
  return {-3, 0}
end
if quantity == 0 then
  redis.call('HDEL', key, field)
  if redis.call('HLEN', key) == 0 then
    redis.call('DEL', key)
  else
    redis.call('EXPIRE', key, ttl)
  end
  return {0, 0}
end

local exists = redis.call('HEXISTS', key, field)
if exists == 0 and redis.call('HLEN', key) >= maxItems then
  return {-2, 0}
end
if quantity > maxQuantity then
  return {-1, 0}
end

redis.call('HSET', key, field, quantity)
redis.call('EXPIRE', key, ttl)
return {quantity, 0}
`)

var removeScript = redis.NewScript(`
local key = KEYS[1]
local field = ARGV[1]
local ttl = tonumber(ARGV[2])

redis.call('HDEL', key, field)
if redis.call('HLEN', key) == 0 then
  redis.call('DEL', key)
else
  redis.call('EXPIRE', key, ttl)
end
return 1
`)
