package integration_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DharmilG/go-commerce/internal/cart"
	"github.com/DharmilG/go-commerce/internal/order"
	"github.com/DharmilG/go-commerce/internal/product"
)

type cartResponse struct {
	Cart cart.Cart `json:"cart"`
}

type orderResponse struct {
	Order order.Order `json:"order"`
}

type ordersResponse struct {
	Data       []order.Order `json:"data"`
	Pagination struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Total  int `json:"total"`
	} `json:"pagination"`
}

type productResponse struct {
	Product product.Product `json:"product"`
}

type productsResponse struct {
	Data       []product.Product `json:"data"`
	Pagination struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Total  int `json:"total"`
	} `json:"pagination"`
}

func TestHealthAndReadiness(t *testing.T) {
	status, payload, _ := request(t, http.MethodGet, "/healthz", nil, "", nil)
	assertStatus(t, status, http.StatusOK, payload)
	status, payload, _ = request(t, http.MethodGet, "/readyz", nil, "", nil)
	assertStatus(t, status, http.StatusOK, payload)
}

func TestAuthLifecycle(t *testing.T) {
	reset(t)
	email := "learner-" + uuid.NewString() + "@example.com"

	session := register(t, email)
	if session.AccessToken == "" || session.TokenType != "Bearer" || session.ExpiresIn <= 0 {
		t.Fatalf("invalid registration session: %+v", session)
	}

	status, payload, _ := request(t, http.MethodGet, "/api/v1/auth/me", nil, session.AccessToken, nil)
	assertStatus(t, status, http.StatusOK, payload)

	status, payload, _ = request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"name": "Duplicate", "email": strings.ToUpper(email), "password": "password-123",
	}, "", nil)
	assertStatus(t, status, http.StatusConflict, payload)
	assertErrorCode(t, payload, "EMAIL_TAKEN")

	normalized := login(t, strings.ToUpper(email), "password-123")
	if normalized.AccessToken == "" {
		t.Fatal("login with normalized email returned an empty token")
	}

	status, payload, _ = request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": "wrong-password",
	}, "", nil)
	assertStatus(t, status, http.StatusUnauthorized, payload)
	assertErrorCode(t, payload, "INVALID_CREDENTIALS")

	status, payload, _ = request(t, http.MethodGet, "/api/v1/auth/me", nil, "", nil)
	assertStatus(t, status, http.StatusUnauthorized, payload)

	status, payload, _ = request(t, http.MethodGet, "/api/v1/auth/me", nil, "not-a-token", nil)
	assertStatus(t, status, http.StatusUnauthorized, payload)
}

func TestCatalogVisibilityAndAdminAuthorization(t *testing.T) {
	reset(t)
	activeID := insertProduct(t, "ACTIVE-1", 1000, 5)
	inactiveID := insertProduct(t, "INACTIVE-1", 1000, 5)
	if _, err := suite.db.Exec(context.Background(), `UPDATE products SET is_active = false WHERE id = $1`, inactiveID.String()); err != nil {
		t.Fatalf("deactivate product: %v", err)
	}

	status, payload, _ := request(t, http.MethodGet, "/api/v1/products?limit=100", nil, "", nil)
	assertStatus(t, status, http.StatusOK, payload)
	list := decode[productsResponse](t, payload)
	if list.Pagination.Total != 1 || len(list.Data) != 1 || list.Data[0].ID != activeID {
		t.Fatalf("public list leaked inactive products: %+v", list)
	}

	customer := register(t, "")
	status, payload, _ = request(t, http.MethodPost, "/api/v1/admin/products", map[string]any{
		"sku": "DENIED-1", "name": "Denied", "price_cents": 100, "stock": 1,
	}, customer.AccessToken, nil)
	assertStatus(t, status, http.StatusForbidden, payload)

	status, payload, _ = request(t, http.MethodPost, "/api/v1/admin/products", map[string]any{
		"sku": "ADMIN-1", "name": "Admin Created", "price_cents": 2500, "stock": 4, "category": "Test",
	}, suite.adminToken, nil)
	assertStatus(t, status, http.StatusCreated, payload)
	created := decode[productResponse](t, payload)
	if created.Product.ID == uuid.Nil || created.Product.PriceCents != 2500 {
		t.Fatalf("unexpected created product: %+v", created.Product)
	}

	status, payload, _ = request(t, http.MethodPost, "/api/v1/admin/products", map[string]any{
		"sku": "ADMIN-1", "name": "Duplicate SKU", "price_cents": 100, "stock": 1,
	}, suite.adminToken, nil)
	assertStatus(t, status, http.StatusConflict, payload)
	assertErrorCode(t, payload, "SKU_TAKEN")

	status, payload, _ = request(t, http.MethodPatch, "/api/v1/admin/products/"+created.Product.ID.String(), map[string]any{
		"price_cents": 3000, "stock": 9,
	}, suite.adminToken, nil)
	assertStatus(t, status, http.StatusOK, payload)
	updated := decode[productResponse](t, payload)
	if updated.Product.PriceCents != 3000 || updated.Product.Stock != 9 {
		t.Fatalf("admin update did not apply: %+v", updated.Product)
	}

	status, payload, _ = request(t, http.MethodPost, "/api/v1/admin/products", map[string]any{
		"sku": "BAD-PRICE", "name": "Bad Price", "price_cents": -1, "stock": 1,
	}, suite.adminToken, nil)
	assertStatus(t, status, http.StatusBadRequest, payload)
	assertErrorCode(t, payload, "VALIDATION_ERROR")
}

func TestCartTTLRefreshesOnEveryMutation(t *testing.T) {
	reset(t)
	session := register(t, "")
	productID := insertProduct(t, "TTL-1", 1000, 10)

	status, payload, _ := request(t, http.MethodPost, "/api/v1/cart/items", map[string]any{
		"product_id": productID, "quantity": 1,
	}, session.AccessToken, nil)
	assertStatus(t, status, http.StatusOK, payload)
	first := decode[cartResponse](t, payload).Cart
	if first.ExpiresInSeconds < 500 {
		t.Fatalf("initial cart TTL = %d, want close to 600", first.ExpiresInSeconds)
	}

	time.Sleep(2100 * time.Millisecond)

	status, payload, _ = request(t, http.MethodGet, "/api/v1/cart", nil, session.AccessToken, nil)
	assertStatus(t, status, http.StatusOK, payload)
	afterGet := decode[cartResponse](t, payload).Cart
	if afterGet.ExpiresInSeconds >= first.ExpiresInSeconds {
		t.Fatalf("GET refreshed the TTL: first=%d after=%d", first.ExpiresInSeconds, afterGet.ExpiresInSeconds)
	}

	status, payload, _ = request(t, http.MethodPut, "/api/v1/cart/items/"+productID.String(), map[string]int{
		"quantity": 2,
	}, session.AccessToken, nil)
	assertStatus(t, status, http.StatusOK, payload)
	afterSet := decode[cartResponse](t, payload).Cart
	if afterSet.ExpiresInSeconds < 590 {
		t.Fatalf("mutation did not refresh TTL: %d", afterSet.ExpiresInSeconds)
	}
	if afterSet.ItemCount != 2 {
		t.Fatalf("item count = %d, want 2", afterSet.ItemCount)
	}

	status, payload, _ = request(t, http.MethodDelete, "/api/v1/cart/items/"+productID.String(), nil, session.AccessToken, nil)
	assertStatus(t, status, http.StatusOK, payload)
	afterDelete := decode[cartResponse](t, payload).Cart
	if len(afterDelete.Items) != 0 || afterDelete.ItemCount != 0 {
		t.Fatalf("cart was not emptied: %+v", afterDelete)
	}

	key := suite.cfg.RedisPrefix + "cart:" + session.User.ID.String()
	exists, err := suite.redis.Exists(context.Background(), key).Result()
	if err != nil {
		t.Fatalf("check redis key: %v", err)
	}
	if exists != 0 {
		t.Fatalf("cart key still exists after removing the last item")
	}
}

func TestCheckoutDecrementsStockAndClearsCart(t *testing.T) {
	reset(t)
	session := register(t, "")
	productID := insertProduct(t, "BUY-1", 1299, 5)
	addToCart(t, session.AccessToken, productID, 2)

	status, payload, headers := placeOrder(t, session.AccessToken, "checkout-key-"+uuid.NewString(), nil, nil)
	assertStatus(t, status, http.StatusCreated, payload)
	if headers.Get("Idempotency-Replayed") != "" {
		t.Fatalf("first order was marked as replayed")
	}
	placed := decode[orderResponse](t, payload).Order
	if placed.TotalCents != 2598 || len(placed.Items) != 1 || placed.Items[0].Quantity != 2 {
		t.Fatalf("unexpected order: %+v", placed)
	}
	if placed.ShippingAddress.City != "Ahmedabad" {
		t.Fatalf("shipping address was not persisted: %+v", placed.ShippingAddress)
	}

	var stock int
	if err := suite.db.QueryRow(context.Background(), `SELECT stock FROM products WHERE id = $1`, productID.String()).Scan(&stock); err != nil {
		t.Fatalf("read stock: %v", err)
	}
	if stock != 3 {
		t.Fatalf("stock = %d, want 3", stock)
	}

	status, payload, _ = request(t, http.MethodGet, "/api/v1/cart", nil, session.AccessToken, nil)
	assertStatus(t, status, http.StatusOK, payload)
	current := decode[cartResponse](t, payload).Cart
	if len(current.Items) != 0 {
		t.Fatalf("cart should be empty after checkout: %+v", current)
	}

	status, payload, _ = request(t, http.MethodGet, "/api/v1/orders", nil, session.AccessToken, nil)
	assertStatus(t, status, http.StatusOK, payload)
	orders := decode[ordersResponse](t, payload)
	if orders.Pagination.Total != 1 || len(orders.Data) != 1 {
		t.Fatalf("order not listed: %+v", orders)
	}
}

func TestIdempotencyReplayAndConflict(t *testing.T) {
	reset(t)
	session := register(t, "")
	productID := insertProduct(t, "IDEM-1", 1000, 5)
	addToCart(t, session.AccessToken, productID, 2)
	key := "idem-key-" + uuid.NewString()

	status, firstPayload, _ := placeOrder(t, session.AccessToken, key, nil, nil)
	assertStatus(t, status, http.StatusCreated, firstPayload)
	first := decode[orderResponse](t, firstPayload).Order

	status, replayPayload, replayHeaders := placeOrder(t, session.AccessToken, key, nil, nil)
	assertStatus(t, status, http.StatusCreated, replayPayload)
	if replayHeaders.Get("Idempotency-Replayed") != "true" {
		t.Fatalf("missing replay header: %v", replayHeaders)
	}
	replayed := decode[orderResponse](t, replayPayload).Order
	if replayed.ID != first.ID {
		t.Fatalf("replayed order id = %s, want %s", replayed.ID, first.ID)
	}

	changedAddress := defaultAddress()
	changedAddress["line1"] = "2 Different Street"
	status, payload, _ := placeOrder(t, session.AccessToken, key, changedAddress, nil)
	assertStatus(t, status, http.StatusConflict, payload)
	assertErrorCode(t, payload, "IDEMPOTENCY_CONFLICT")

	var stock, orderCount int
	if err := suite.db.QueryRow(context.Background(), `SELECT stock FROM products WHERE id = $1`, productID.String()).Scan(&stock); err != nil {
		t.Fatalf("read stock: %v", err)
	}
	if err := suite.db.QueryRow(context.Background(), `SELECT count(*) FROM orders`).Scan(&orderCount); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if stock != 3 || orderCount != 1 {
		t.Fatalf("replay changed state: stock=%d orders=%d", stock, orderCount)
	}

	status, payload, _ = placeOrder(t, session.AccessToken, "short", nil, nil)
	assertStatus(t, status, http.StatusBadRequest, payload)
	assertErrorCode(t, payload, "INVALID_IDEMPOTENCY_KEY")

	status, payload, _ = placeOrder(t, session.AccessToken, "", nil, nil)
	assertStatus(t, status, http.StatusBadRequest, payload)
	assertErrorCode(t, payload, "IDEMPOTENCY_KEY_REQUIRED")
}

func TestConcurrentCheckoutDoesNotOversell(t *testing.T) {
	reset(t)
	productID := insertProduct(t, "RACE-1", 500, 1)
	const buyers = 8

	tokens := make([]string, 0, buyers)
	for i := 0; i < buyers; i++ {
		session := register(t, "")
		addToCart(t, session.AccessToken, productID, 1)
		tokens = append(tokens, session.AccessToken)
	}

	type outcome struct {
		status int
		body   []byte
		err    error
	}
	start := make(chan struct{})
	results := make(chan outcome, buyers)
	for _, token := range tokens {
		token := token
		go func() {
			<-start
			status, body, _, err := rawRequest(http.MethodPost, "/api/v1/orders", map[string]any{
				"shipping_address": defaultAddress(),
			}, token, map[string]string{"Idempotency-Key": "race-" + uuid.NewString()})
			results <- outcome{status: status, body: body, err: err}
		}()
	}
	close(start)

	successes := 0
	for i := 0; i < buyers; i++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent checkout error: %v", result.err)
		}
		switch result.status {
		case http.StatusCreated:
			successes++
		case http.StatusConflict:
			assertErrorCode(t, result.body, "INSUFFICIENT_STOCK")
		default:
			t.Fatalf("unexpected concurrent checkout status %d: %s", result.status, result.body)
		}
	}
	if successes != 1 {
		t.Fatalf("successful checkouts = %d, want 1", successes)
	}

	var stock, orderCount int
	if err := suite.db.QueryRow(context.Background(), `SELECT stock FROM products WHERE id = $1`, productID.String()).Scan(&stock); err != nil {
		t.Fatalf("read stock: %v", err)
	}
	if err := suite.db.QueryRow(context.Background(), `SELECT count(*) FROM orders`).Scan(&orderCount); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if stock != 0 || orderCount != 1 {
		t.Fatalf("oversell detected: stock=%d orders=%d", stock, orderCount)
	}
}

func TestConcurrentSameIdempotencyKeyCreatesOneOrder(t *testing.T) {
	reset(t)
	session := register(t, "")
	productID := insertProduct(t, "IDEM-RACE-1", 700, 5)
	addToCart(t, session.AccessToken, productID, 2)
	key := "idem-race-" + uuid.NewString()
	const attempts = 10

	type outcome struct {
		status int
		body   []byte
		err    error
	}
	start := make(chan struct{})
	results := make(chan outcome, attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			<-start
			status, body, _, err := rawRequest(http.MethodPost, "/api/v1/orders", map[string]any{
				"shipping_address": defaultAddress(),
			}, session.AccessToken, map[string]string{"Idempotency-Key": key})
			results <- outcome{status: status, body: body, err: err}
		}()
	}
	close(start)

	orderIDs := map[uuid.UUID]struct{}{}
	for i := 0; i < attempts; i++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent idempotent checkout error: %v", result.err)
		}
		assertStatus(t, result.status, http.StatusCreated, result.body)
		placed := decode[orderResponse](t, result.body).Order
		orderIDs[placed.ID] = struct{}{}
	}
	if len(orderIDs) != 1 {
		t.Fatalf("distinct order ids = %d, want 1", len(orderIDs))
	}

	var stock, orderCount int
	if err := suite.db.QueryRow(context.Background(), `SELECT stock FROM products WHERE id = $1`, productID.String()).Scan(&stock); err != nil {
		t.Fatalf("read stock: %v", err)
	}
	if err := suite.db.QueryRow(context.Background(), `SELECT count(*) FROM orders`).Scan(&orderCount); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if stock != 3 || orderCount != 1 {
		t.Fatalf("idempotent concurrency changed state: stock=%d orders=%d", stock, orderCount)
	}
}

func TestPartialCheckoutFailureRollsBackStock(t *testing.T) {
	reset(t)
	session := register(t, "")
	firstID := insertProduct(t, "ROLLBACK-A", 1000, 5)
	secondID := insertProduct(t, "ROLLBACK-B", 2000, 2)
	addToCart(t, session.AccessToken, firstID, 2)
	addToCart(t, session.AccessToken, secondID, 2)

	if _, err := suite.db.Exec(context.Background(), `UPDATE products SET stock = 1 WHERE id = $1`, secondID.String()); err != nil {
		t.Fatalf("reduce second product stock: %v", err)
	}

	status, payload, _ := placeOrder(t, session.AccessToken, "rollback-key-"+uuid.NewString(), nil, nil)
	assertStatus(t, status, http.StatusConflict, payload)
	assertErrorCode(t, payload, "INSUFFICIENT_STOCK")

	var firstStock, secondStock, orderCount int
	if err := suite.db.QueryRow(context.Background(), `SELECT stock FROM products WHERE id = $1`, firstID.String()).Scan(&firstStock); err != nil {
		t.Fatalf("read first stock: %v", err)
	}
	if err := suite.db.QueryRow(context.Background(), `SELECT stock FROM products WHERE id = $1`, secondID.String()).Scan(&secondStock); err != nil {
		t.Fatalf("read second stock: %v", err)
	}
	if err := suite.db.QueryRow(context.Background(), `SELECT count(*) FROM orders`).Scan(&orderCount); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if firstStock != 5 || secondStock != 1 || orderCount != 0 {
		t.Fatalf("partial order was not rolled back: first=%d second=%d orders=%d", firstStock, secondStock, orderCount)
	}
}

func TestUnavailableProductRejectsCheckout(t *testing.T) {
	reset(t)
	session := register(t, "")
	productID := insertProduct(t, "UNAVAILABLE-1", 900, 3)
	addToCart(t, session.AccessToken, productID, 1)

	if _, err := suite.db.Exec(context.Background(), `UPDATE products SET is_active = false WHERE id = $1`, productID.String()); err != nil {
		t.Fatalf("deactivate product: %v", err)
	}

	status, payload, _ := placeOrder(t, session.AccessToken, "unavailable-"+uuid.NewString(), nil, nil)
	assertStatus(t, status, http.StatusConflict, payload)
	assertErrorCode(t, payload, "PRODUCT_UNAVAILABLE")

	var orderCount int
	if err := suite.db.QueryRow(context.Background(), `SELECT count(*) FROM orders`).Scan(&orderCount); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if orderCount != 0 {
		t.Fatalf("order was created for an inactive product")
	}
}

func TestServerRecalculatesPriceAtCheckout(t *testing.T) {
	reset(t)
	session := register(t, "")
	productID := insertProduct(t, "PRICE-1", 100, 5)
	addToCart(t, session.AccessToken, productID, 2)

	if _, err := suite.db.Exec(context.Background(), `UPDATE products SET price_cents = 150 WHERE id = $1`, productID.String()); err != nil {
		t.Fatalf("update price: %v", err)
	}

	status, payload, _ := placeOrder(t, session.AccessToken, "price-key-"+uuid.NewString(), nil, nil)
	assertStatus(t, status, http.StatusCreated, payload)
	placed := decode[orderResponse](t, payload).Order
	if placed.TotalCents != 300 || placed.Items[0].UnitPriceCents != 150 {
		t.Fatalf("server did not recalculate price: %+v", placed)
	}
}

func TestOrderOwnershipAndAdminRead(t *testing.T) {
	reset(t)
	owner := register(t, "")
	other := register(t, "")
	productID := insertProduct(t, "OWNER-1", 1000, 5)
	addToCart(t, owner.AccessToken, productID, 1)

	status, payload, _ := placeOrder(t, owner.AccessToken, "owner-key-"+uuid.NewString(), nil, nil)
	assertStatus(t, status, http.StatusCreated, payload)
	placed := decode[orderResponse](t, payload).Order

	status, payload, _ = request(t, http.MethodGet, "/api/v1/orders/"+placed.ID.String(), nil, other.AccessToken, nil)
	assertStatus(t, status, http.StatusNotFound, payload)
	assertErrorCode(t, payload, "ORDER_NOT_FOUND")

	status, payload, _ = request(t, http.MethodGet, "/api/v1/orders/"+placed.ID.String(), nil, suite.adminToken, nil)
	assertStatus(t, status, http.StatusOK, payload)
	adminView := decode[orderResponse](t, payload).Order
	if adminView.ID != placed.ID {
		t.Fatalf("admin read wrong order: %+v", adminView)
	}
}

func TestAddressValidationAndEmptyCart(t *testing.T) {
	reset(t)
	session := register(t, "")

	status, payload, _ := placeOrder(t, session.AccessToken, "empty-key-"+uuid.NewString(), nil, nil)
	assertStatus(t, status, http.StatusConflict, payload)
	assertErrorCode(t, payload, "CART_EMPTY")

	productID := insertProduct(t, "ADDR-1", 100, 5)
	addToCart(t, session.AccessToken, productID, 1)
	invalid := defaultAddress()
	delete(invalid, "city")

	status, payload, _ = placeOrder(t, session.AccessToken, "addr-key-"+uuid.NewString(), invalid, nil)
	assertStatus(t, status, http.StatusBadRequest, payload)
	assertErrorCode(t, payload, "VALIDATION_ERROR")
}

func TestReplayDoesNotDependOnCartStillExisting(t *testing.T) {
	reset(t)
	session := register(t, "")
	productID := insertProduct(t, "REPLAY-1", 1000, 5)
	addToCart(t, session.AccessToken, productID, 1)
	key := "replay-key-" + uuid.NewString()

	status, payload, _ := placeOrder(t, session.AccessToken, key, nil, nil)
	assertStatus(t, status, http.StatusCreated, payload)
	first := decode[orderResponse](t, payload).Order

	status, payload, headers := placeOrder(t, session.AccessToken, key, nil, nil)
	assertStatus(t, status, http.StatusCreated, payload)
	if headers.Get("Idempotency-Replayed") != "true" {
		t.Fatal("replay header missing after cart was cleared")
	}
	second := decode[orderResponse](t, payload).Order
	if first.ID != second.ID {
		t.Fatalf("replay after cart clear returned a different order")
	}
}

func TestOrderIsDurableBeforeNotification(t *testing.T) {
	reset(t)
	session := register(t, "")
	productID := insertProduct(t, "WORKER-1", 500, 3)
	addToCart(t, session.AccessToken, productID, 1)

	status, payload, _ := placeOrder(t, session.AccessToken, "worker-key-"+uuid.NewString(), nil, nil)
	assertStatus(t, status, http.StatusCreated, payload)
	placed := decode[orderResponse](t, payload).Order

	// The logging notifier is asynchronous. The order must be durable even if
	// the worker has not run yet, so assert the committed row immediately.
	var count int
	if err := suite.db.QueryRow(context.Background(), `SELECT count(*) FROM orders WHERE id = $1`, placed.ID.String()).Scan(&count); err != nil {
		t.Fatalf("check committed order: %v", err)
	}
	if count != 1 {
		t.Fatalf("committed order count = %d, want 1", count)
	}
}
