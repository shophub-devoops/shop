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

// testPool talks to a throwaway Postgres started once for the package
// (Testcontainers — spec 5.2 integration-test infrastructure). The schema is
// the same testdata/schema.sql the operator bootstraps in production.
var testPool *pgxpool.Pool

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
	testPool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("pool: %v", err)
	}

	schema, err := os.ReadFile("testdata/schema.sql")
	if err != nil {
		log.Fatalf("read schema: %v", err)
	}
	if _, err := testPool.Exec(ctx, string(schema)); err != nil {
		log.Fatalf("apply schema: %v", err)
	}

	code := m.Run()

	testPool.Close()
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
	r := buildRouter(testPool)

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
	r := buildRouter(testPool)
	upd := item{Name: "Ghost", Price: "1.00", Stock: 1}
	if w := do(r, http.MethodPut, "/api/items/does-not-exist", upd); w.Code != http.StatusNotFound {
		t.Fatalf("update missing item = %d, want 404", w.Code)
	}
}

func TestCreateOrderRespectsStock(t *testing.T) {
	r := buildRouter(testPool)

	// Seed an item with stock 5.
	seed := item{ID: "ord-item", Name: "Thing", Price: "2.50", Stock: 5}
	if w := do(r, http.MethodPost, "/api/items", seed); w.Code != http.StatusCreated {
		t.Fatalf("seed item = %d", w.Code)
	}

	// Order within stock → 201.
	ok := order{ID: "o1", BuyerWallet: "0xBUY", AmountUSDT: "5.00", ItemID: "ord-item", ItemQuantity: 2}
	if w := do(r, http.MethodPost, "/api/orders", ok); w.Code != http.StatusCreated {
		t.Fatalf("valid order = %d (body: %s)", w.Code, w.Body.String())
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
	w := do(r, http.MethodGet, "/api/orders", nil)
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
