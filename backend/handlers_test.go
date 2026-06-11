package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testStore is a Postgres-backed Store over a throwaway Postgres started once
// for the package (Testcontainers — spec 5.2 integration-test infrastructure).
// testPool is the same connection, kept for raw SQL seeding in tests.
var (
	testStore Store
	testPool  *pgxpool.Pool
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	pg, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("shop"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		log.Fatalf("start postgres: %v", err)
	}

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("conn string: %v", err)
	}
	store, err := newPostgresStore(ctx, dsn)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	testStore = store
	testPool = store.pool

	if err := testStore.EnsureSchema(ctx); err != nil {
		log.Fatalf("ensure schema: %v", err)
	}

	code := m.Run()

	testStore.Close()
	_ = pg.Terminate(ctx)
	os.Exit(code)
}

func do(r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func listItemIDs(t *testing.T, r http.Handler) map[string]item {
	t.Helper()
	w := do(r, http.MethodGet, "/api/items", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list items = %d", w.Code)
	}
	var items []item
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode items: %v", err)
	}
	out := map[string]item{}
	for _, it := range items {
		out[it.ID] = it
	}
	return out
}

func TestItemCRUD(t *testing.T) {
	r := buildRouter(testStore, config{TokenDecimals: 6})

	// Create.
	create := item{ID: "crud-1", Name: "Widget", Price: "9.99", Stock: 5}
	if w := do(r, http.MethodPost, "/api/items", create); w.Code != http.StatusCreated {
		t.Fatalf("create item = %d (body: %s)", w.Code, w.Body.String())
	}

	// List contains it.
	if it, ok := listItemIDs(t, r)["crud-1"]; !ok || it.Name != "Widget" || it.Stock != 5 {
		t.Fatalf("created item missing/wrong in list: %+v", it)
	}

	// Update stock.
	upd := item{Name: "Widget", Price: "9.99", Stock: 12}
	if w := do(r, http.MethodPut, "/api/items/crud-1", upd); w.Code != http.StatusOK {
		t.Fatalf("update item = %d (body: %s)", w.Code, w.Body.String())
	}
	if it := listItemIDs(t, r)["crud-1"]; it.Stock != 12 {
		t.Fatalf("stock after update = %d, want 12", it.Stock)
	}

	// Delete.
	if w := do(r, http.MethodDelete, "/api/items/crud-1", nil); w.Code != http.StatusNoContent {
		t.Fatalf("delete item = %d", w.Code)
	}
	if _, ok := listItemIDs(t, r)["crud-1"]; ok {
		t.Fatal("item still present after delete")
	}
}

func TestUpdateMissingItemReturns404(t *testing.T) {
	r := buildRouter(testStore, config{TokenDecimals: 6})
	upd := item{Name: "Ghost", Price: "1.00", Stock: 1}
	if w := do(r, http.MethodPut, "/api/items/does-not-exist", upd); w.Code != http.StatusNotFound {
		t.Fatalf("update missing item = %d, want 404", w.Code)
	}
}

func TestCreateOrderRespectsStock(t *testing.T) {
	r := buildRouter(testStore, config{TokenDecimals: 6})

	// Seed an item with stock 5.
	seed := item{ID: "ord-item", Name: "Thing", Price: "2.50", Stock: 5}
	if w := do(r, http.MethodPost, "/api/items", seed); w.Code != http.StatusCreated {
		t.Fatalf("seed item = %d", w.Code)
	}

	// Order within stock → 201, and the stock is reserved immediately (5-2=3).
	// The client-sent amount lies ("0.01"); the server must ignore it and
	// compute price × qty from the stored item (2.50 × 2 = 5).
	ok := order{ID: "o1", BuyerWallet: "0xBUY", AmountUSDT: "0.01", ItemID: "ord-item", ItemQuantity: 2}
	w := do(r, http.MethodPost, "/api/orders", ok)
	if w.Code != http.StatusCreated {
		t.Fatalf("valid order = %d (body: %s)", w.Code, w.Body.String())
	}
	var created order
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode order: %v", err)
	}
	if created.AmountUSDT != "5" {
		t.Fatalf("amount = %q, want %q (server-computed price × qty)", created.AmountUSDT, "5")
	}
	if it := listItemIDs(t, r)["ord-item"]; it.Stock != 3 {
		t.Fatalf("stock after order = %d, want 3 (reserved at creation)", it.Stock)
	}

	// Order exceeding stock → 409.
	tooMuch := order{ID: "o2", BuyerWallet: "0xBUY", AmountUSDT: "99.00", ItemID: "ord-item", ItemQuantity: 99}
	if w := do(r, http.MethodPost, "/api/orders", tooMuch); w.Code != http.StatusConflict {
		t.Fatalf("over-stock order = %d, want 409", w.Code)
	}

	// Order for unknown item → 404.
	ghost := order{ID: "o3", BuyerWallet: "0xBUY", AmountUSDT: "1.00", ItemID: "nope", ItemQuantity: 1}
	if w := do(r, http.MethodPost, "/api/orders", ghost); w.Code != http.StatusNotFound {
		t.Fatalf("unknown-item order = %d, want 404", w.Code)
	}

	// Orders list includes the successful one.
	w = do(r, http.MethodGet, "/api/orders", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list orders = %d", w.Code)
	}
	var orders []order
	_ = json.Unmarshal(w.Body.Bytes(), &orders)
	found := false
	for _, o := range orders {
		if o.ID == "o1" {
			found = true
		}
	}
	if !found {
		t.Fatal("created order o1 not in list")
	}
}

// TestConfirmIsIdempotent guards the multi-replica payment sweep: stock is
// reserved once at order creation, and repeated confirms don't touch it again.
func TestConfirmIsIdempotent(t *testing.T) {
	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO items (id, name, price_usdt, stock) VALUES ('idem-item','Idem','3'::numeric,5)`); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	tx := "0xtx"
	if _, err := testStore.CreateOrder(ctx, order{
		ID: "idem-order", BuyerWallet: "0xBUY", TxHash: &tx,
		ItemID: "idem-item", ItemQuantity: 2,
	}); err != nil {
		t.Fatalf("create order: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := testStore.ConfirmOrder(ctx, "idem-order"); err != nil {
			t.Fatalf("confirm #%d: %v", i, err)
		}
	}

	var stock int
	var status string
	if err := testPool.QueryRow(ctx, `SELECT stock FROM items WHERE id='idem-item'`).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT status FROM orders WHERE id='idem-order'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if stock != 3 {
		t.Errorf("stock = %d, want 3 (reserved once at creation)", stock)
	}
	if status != "confirmed" {
		t.Errorf("status = %q, want confirmed", status)
	}
}

// TestFailOrderRestoresStock guards the reservation release: failing a pending
// order restores its stock exactly once, even when swept by multiple replicas.
func TestFailOrderRestoresStock(t *testing.T) {
	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO items (id, name, price_usdt, stock) VALUES ('fail-item','Fail','3'::numeric,5)`); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := testStore.CreateOrder(ctx, order{
		ID: "fail-order", BuyerWallet: "0xBUY",
		ItemID: "fail-item", ItemQuantity: 2,
	}); err != nil {
		t.Fatalf("create order: %v", err)
	}

	var stock int
	if err := testPool.QueryRow(ctx, `SELECT stock FROM items WHERE id='fail-item'`).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if stock != 3 {
		t.Fatalf("stock after order = %d, want 3", stock)
	}

	for i := 0; i < 3; i++ {
		if err := testStore.FailOrder(ctx, "fail-order", "fail-item", 2); err != nil {
			t.Fatalf("fail #%d: %v", i, err)
		}
	}

	var status string
	if err := testPool.QueryRow(ctx, `SELECT stock FROM items WHERE id='fail-item'`).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT status FROM orders WHERE id='fail-order'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if stock != 5 {
		t.Errorf("stock = %d, want 5 (restored exactly once)", stock)
	}
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}
}

// TestAdminAuth verifies the admin gate end-to-end: writes and order listing
// require a token from /api/auth/login, while catalogue reads stay public.
func TestAdminAuth(t *testing.T) {
	r := buildRouter(testStore, config{TokenDecimals: 6, AdminPassword: "s3cret-pass"})

	// Public reads work without a token.
	if w := do(r, http.MethodGet, "/api/items", nil); w.Code != http.StatusOK {
		t.Fatalf("public item list = %d, want 200", w.Code)
	}

	// Writes and order listing are rejected without a token.
	it := item{ID: "auth-1", Name: "Locked", Price: "1.00", Stock: 1}
	if w := do(r, http.MethodPost, "/api/items", it); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create = %d, want 401", w.Code)
	}
	if w := do(r, http.MethodGet, "/api/orders", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated order list = %d, want 401", w.Code)
	}

	// Wrong password → 401.
	if w := do(r, http.MethodPost, "/api/auth/login", gin.H{"password": "wrong"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-password login = %d, want 401", w.Code)
	}

	// Correct password → token that unlocks admin endpoints.
	w := do(r, http.MethodPost, "/api/auth/login", gin.H{"password": "s3cret-pass"})
	if w.Code != http.StatusOK {
		t.Fatalf("login = %d (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Token == "" {
		t.Fatalf("login response %q: %v", w.Body.String(), err)
	}

	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(it)
	req := httptest.NewRequest(http.MethodPost, "/api/items", &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+resp.Token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("authenticated create = %d (body: %s)", rec.Code, rec.Body.String())
	}
}
