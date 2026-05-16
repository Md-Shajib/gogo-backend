# Phase 4 — Order Module Implementation Plan

**Goal:** Place an order transactionally (validate stock → snapshot price → decrement inventory → insert order + items — all in one DB transaction). Order listing, detail, and status management. Introduce the Unit of Work pattern to keep `*sql.Tx` out of the service layer.

**Estimated time:** ~5h 45m  
**Prerequisites:** Phase 3 complete. Product, variants, and inventory rows exist in DB.

---

## Execution Order

Steps must be done in sequence — each step depends on the previous.

---

## Step 1 — Migration: orders table ⏱ 10m

**File:** `migrations/sql/000006_orders.up.sql`

```sql
CREATE TYPE order_status AS ENUM (
    'pending', 'paid', 'processing', 'shipped', 'delivered', 'cancelled', 'refunded'
);

CREATE TABLE orders (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID         NOT NULL REFERENCES users(id),
    status           order_status NOT NULL DEFAULT 'pending',
    total_amount     NUMERIC(10,2) NOT NULL,
    shipping_name    TEXT         NOT NULL,
    shipping_address TEXT         NOT NULL,
    shipping_phone   TEXT         NOT NULL,
    created_at       TIMESTAMPTZ  DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  DEFAULT NOW()
);
```

**File:** `migrations/sql/000006_orders.down.sql`

```sql
DROP TABLE IF EXISTS orders;
DROP TYPE IF EXISTS order_status;
```

**Rules:**
- Drop table before type — Postgres will reject dropping the type while the table references it
- `status` uses a Postgres enum — valid values enforced at DB level
- `total_amount` is computed by the service and stored — not recomputed on read

---

## Step 2 — Migration: order_items table ⏱ 10m

**File:** `migrations/sql/000007_order_items.up.sql`

```sql
CREATE TABLE order_items (
    id         UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id   UUID           NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    variant_id UUID           NOT NULL REFERENCES product_variants(id),
    quantity   INT            NOT NULL CHECK (quantity > 0),
    unit_price NUMERIC(10,2)  NOT NULL
);
```

**File:** `migrations/sql/000007_order_items.down.sql`

```sql
DROP TABLE IF EXISTS order_items;
```

**Rules:**
- `unit_price` is the price at the moment of purchase — a snapshot, not a reference to current price
- `ON DELETE CASCADE` — deleting an order deletes its items
- `variant_id` is NOT cascaded on variant delete — a deleted variant should not silently remove order history

---

## Step 3 — `internal/order/entity.go` ⏱ 10m

**Package:** `order`  
**Purpose:** Pure domain structs. Zero external imports. No json or db tags.

```
Order
  ID              string
  UserID          string
  Status          string
  TotalAmount     float64
  ShippingName    string
  ShippingAddress string
  ShippingPhone   string
  CreatedAt       time.Time
  UpdatedAt       time.Time
  Items           []OrderItem

OrderItem
  ID        string
  OrderID   string
  VariantID string
  Quantity  int
  UnitPrice float64
```

**Rules:**
- No `json:` or `db:` tags
- Import only `"time"`
- `Items []OrderItem` is populated by the repository — the entity defines the shape

---

## Step 4 — `internal/order/model.go` ⏱ 15m

**Package:** `order`  
**Purpose:** HTTP request/response DTOs.

```
OrderItemRequest
  VariantID string `json:"variant_id"`
  Quantity  int    `json:"quantity"`

PlaceOrderRequest
  Items           []OrderItemRequest `json:"items"`
  ShippingName    string             `json:"shipping_name"`
  ShippingAddress string             `json:"shipping_address"`
  ShippingPhone   string             `json:"shipping_phone"`

OrderStatusUpdateRequest
  Status string `json:"status"`

OrderItemResponse
  ID        string  `json:"id"`
  VariantID string  `json:"variant_id"`
  Quantity  int     `json:"quantity"`
  UnitPrice float64 `json:"unit_price"`

OrderResponse
  ID              string              `json:"id"`
  UserID          string              `json:"user_id"`
  Status          string              `json:"status"`
  TotalAmount     float64             `json:"total_amount"`
  ShippingName    string              `json:"shipping_name"`
  ShippingAddress string              `json:"shipping_address"`
  ShippingPhone   string              `json:"shipping_phone"`
  CreatedAt       time.Time           `json:"created_at"`
  Items           []OrderItemResponse `json:"items"`
```

---

## Step 5 — `internal/order/uow.go` ⏱ 20m

**Package:** `order`  
**Purpose:** Define the Unit of Work interfaces that keep `*sql.Tx` out of the service layer. The service calls `uow.Begin()` and receives a `Transaction` — it never sees `*sql.Tx`.

```
StockDecrementer interface
  DecrementStock(variantID string, qty int) error

Transaction interface
  OrderRepository() OrderRepository     ← tx-aware order repo
  StockDecrementer() StockDecrementer   ← tx-aware stock decrement
  Commit() error
  Rollback() error

UnitOfWork interface
  Begin() (Transaction, error)
```

**Rules:**
- These are pure interface definitions — no implementation here
- `StockDecrementer` is defined here, not in `internal/inventory` — the order module owns the decrement contract within a transaction
- The concrete implementation lives in `internal/database/uow.go` (Phase 4 Step 6)
- The service depends only on `UnitOfWork` and `OrderRepository` interfaces — never on `*sql.Tx`

---

## Step 6 — `internal/database/uow.go` ⏱ 25m

**Package:** `database`  
**Purpose:** Postgres implementation of `order.UnitOfWork`. Wraps `*sql.Tx` internally. The service never sees `*sql.Tx`.

### postgresUoW struct

```
postgresUoW
  db *sql.DB
```

```go
func NewUnitOfWork(db *sql.DB) order.UnitOfWork
```

### Begin() implementation

```go
func (u *postgresUoW) Begin() (order.Transaction, error) {
    tx, err := u.db.Begin()
    if err != nil { return nil, err }
    return &postgresTx{tx: tx}, nil
}
```

### postgresTx struct

```
postgresTx
  tx *sql.Tx
```

Implements `order.Transaction`:

- `OrderRepository()` — returns a `txOrderRepository{tx: p.tx}` that implements `order.OrderRepository` using `p.tx` for queries
- `StockDecrementer()` — returns a `txStockDecrementer{tx: p.tx}` that implements `order.StockDecrementer`
- `Commit()` — calls `p.tx.Commit()`
- `Rollback()` — calls `p.tx.Rollback()`

### txStockDecrementer

```go
func (s *txStockDecrementer) DecrementStock(variantID string, qty int) error {
    result, err := s.tx.Exec(
        `UPDATE inventory SET quantity = quantity - $2, updated_at = NOW()
         WHERE variant_id = $1 AND quantity >= $2`,
        variantID, qty,
    )
    if err != nil { return err }
    n, _ := result.RowsAffected()
    if n == 0 { return apperr.ErrInsufficientStock }
    return nil
}
```

### txOrderRepository

A struct with field `tx *sql.Tx` that implements `order.OrderRepository` (InsertOrder, InsertOrderItems). Its `tx.Exec` and `tx.QueryRow` calls participate in the same transaction started by `Begin()`.

**Rules:**
- Only this file touches `*sql.Tx` — nowhere else
- This file imports `internal/order` for the interfaces — that is correct (outer importing inner)
- `Rollback()` should always be called via `defer` in the service — even after a successful `Commit()`, `Rollback()` on an already-committed tx is a no-op

---

## Step 7 — `internal/order/repository.go` — Interfaces ⏱ 15m

**Package:** `order`  
**Purpose:** Define what the service needs from persistence.

```
OrderRepository interface
  InsertOrder(order *Order) error
  InsertOrderItems(items []OrderItem) error
  GetOrderByID(orderID string) (*Order, error)
  GetOrdersByUserID(userID string) ([]*Order, error)
  ListAllOrders(page, limit int) ([]*Order, int, error)
  UpdateOrderStatus(orderID, status string) error
```

**Rules:**
- No `*sql.Tx` in method signatures — the UoW provides a tx-aware implementation transparently
- `ListAllOrders` returns items plus the total count for pagination
- `UpdateOrderStatus` is the method that satisfies `payment.OrderStatusUpdater` interface in Phase 5

---

## Step 8 — `internal/order/repository.go` — Implementation ⏱ 45m

**Concrete struct:** `orderRepository` with field `db *sql.DB` (used for non-transactional read operations)

### InsertOrder ⏱ 10m

```sql
INSERT INTO orders (user_id, status, total_amount, shipping_name, shipping_address, shipping_phone)
VALUES ($1, 'pending', $2, $3, $4, $5)
RETURNING id, created_at, updated_at
```

Populate `order.ID`, `order.CreatedAt`, `order.UpdatedAt` from RETURNING clause.

### InsertOrderItems ⏱ 10m

For each item in the slice, execute:
```sql
INSERT INTO order_items (order_id, variant_id, quantity, unit_price)
VALUES ($1, $2, $3, $4)
RETURNING id
```

### GetOrderByID ⏱ 10m

```sql
SELECT id, user_id, status, total_amount, shipping_name, shipping_address, shipping_phone,
       created_at, updated_at
FROM orders WHERE id = $1
```

Then fetch items:
```sql
SELECT id, order_id, variant_id, quantity, unit_price
FROM order_items WHERE order_id = $1
```

Return `apperr.ErrNotFound` on `sql.ErrNoRows`.

### GetOrdersByUserID ⏱ 5m

```sql
SELECT id, user_id, status, total_amount, shipping_name, shipping_address, shipping_phone,
       created_at, updated_at
FROM orders WHERE user_id = $1
ORDER BY created_at DESC
```

### ListAllOrders ⏱ 5m

```sql
SELECT id, user_id, status, total_amount, shipping_name, shipping_address, shipping_phone,
       created_at, updated_at
FROM orders ORDER BY created_at DESC LIMIT $1 OFFSET $2
```

Count:
```sql
SELECT COUNT(*) FROM orders
```

### UpdateOrderStatus ⏱ 5m

```sql
UPDATE orders SET status = $2, updated_at = NOW() WHERE id = $1
```

Return `apperr.ErrNotFound` if rows affected is 0.

### Constructor

```go
func NewOrderRepository(db *sql.DB) OrderRepository
```

---

## Step 9 — `internal/order/service.go` ⏱ 1h 25m

**Package:** `order`  
**Purpose:** Business logic. Depends on `UnitOfWork` and `OrderRepository` interfaces only. Never sees `*sql.Tx` or `*sql.DB`.

### Part A — Define OrderService interface ⏱ 5m

```
OrderService interface
  PlaceOrder(userID string, req *PlaceOrderRequest) (*Order, error)
  GetOrder(requestingUserID, requestingRole, orderID string) (*Order, error)
  GetMyOrders(userID string) ([]*Order, error)
  ListAllOrders(page, limit int) ([]*Order, int, error)
  UpdateStatus(orderID, newStatus string) error
```

### Part B — Concrete struct ⏱ 5m

```
orderService
  uow        UnitOfWork
  orderRepo  OrderRepository
```

Constructor:
```go
func NewOrderService(uow UnitOfWork, orderRepo OrderRepository) OrderService
```

### Part C — Implement PlaceOrder ⏱ 30m

This is the most important method — it runs entirely inside one DB transaction.

```
1. Validate request:
   - Items must not be empty → apperr.ErrInvalidInput
   - ShippingName, ShippingAddress, ShippingPhone must not be empty → apperr.ErrInvalidInput
   - Each item Quantity must be > 0 → apperr.ErrInvalidInput

2. Begin transaction: tx, err := uow.Begin()
3. defer tx.Rollback()   ← always — no-op after Commit

4. For each item in req.Items:
   a. Decrement stock via tx.StockDecrementer().DecrementStock(item.VariantID, item.Quantity)
      → if ErrInsufficientStock, return immediately (Rollback fires via defer)

5. Fetch product to get current price:
   (The service needs the product price — this requires a ProductRepository or the price
    must be passed in the request. ARCHITECTURE decision: fetch from DB via a separate
    query using the product repo. The order service must accept a ProductRepository.)

   Revised struct:
   orderService
     uow         UnitOfWork
     orderRepo   OrderRepository
     productRepo ProductRepository

   ← Define a minimal ProductRepository interface in this file (not importing product package):
   type ProductRepository interface {
       GetVariantPrice(variantID string) (base float64, extra float64, err error)
   }

   b. For each item: unitPrice = base + extra (variant's extra_price)
   c. totalAmount += unitPrice * float64(item.Quantity)

6. Build Order and OrderItems:
   order := &Order{
     UserID: userID, TotalAmount: totalAmount,
     ShippingName: ..., ShippingAddress: ..., ShippingPhone: ...,
   }
   items := []OrderItem{ {VariantID:..., Quantity:..., UnitPrice:...}, ... }

7. tx.OrderRepository().InsertOrder(&order)
8. for each item, set item.OrderID = order.ID
9. tx.OrderRepository().InsertOrderItems(items)
10. tx.Commit()
11. Return &order
```

**ProductRepository in order package:**

Define this minimal interface in `internal/order/service.go` (not importing `internal/product`):

```go
type ProductRepository interface {
    GetVariantPrice(variantID string) (basePrice, extraPrice float64, err error)
}
```

The product repository in Phase 2 must implement `GetVariantPrice` — add it to `internal/product/repository.go` when wiring.

### Part D — Implement GetOrder ⏱ 10m

1. Fetch order via `orderRepo.GetOrderByID(orderID)`
2. If not found → return `apperr.ErrNotFound`
3. Authorization check:
   - If `requestingRole == "admin"` → allowed
   - If `order.UserID == requestingUserID` → allowed
   - Otherwise → return `apperr.ErrForbidden`
4. Return order

### Part E — Implement GetMyOrders ⏱ 5m

Call `orderRepo.GetOrdersByUserID(userID)`. Return the slice.

### Part F — Implement ListAllOrders ⏱ 5m

Validate `page >= 1`, `limit >= 1` and `limit <= 100`. Call `orderRepo.ListAllOrders(page, limit)`.

### Part G — Implement UpdateStatus ⏱ 10m

Validate allowed transitions. Fetch the current order first to know its current status.

Allowed transitions map:
```
"pending"    → ["paid", "cancelled"]
"paid"       → ["processing", "refunded"]
"processing" → ["shipped"]
"shipped"    → ["delivered"]
```

Any other transition → return `apperr.ErrInvalidInput` with message `"invalid status transition"`

Call `orderRepo.UpdateOrderStatus(orderID, newStatus)`.

**Rules:**
- Never import `database/sql` — only `UnitOfWork` and `OrderRepository` interfaces
- `defer tx.Rollback()` must be the first deferred call after `uow.Begin()` — ensures cleanup on any error path
- Price is snapshotted at order time — never recalculate from live price after the transaction

---

## Step 10 — `internal/order/handler.go` ⏱ 55m

**Package:** `order`  
**Purpose:** Parse HTTP requests, call service, write responses. Zero business logic.

### Handler struct

```
Handler
  svc OrderService
```

Constructor:
```go
func NewHandler(svc OrderService) *Handler
```

### POST /api/v1/orders ⏱ 15m (auth required)

1. Get `userID` from context: `auth.UserIDFromContext(r.Context())`
2. Decode JSON body into `PlaceOrderRequest`
3. Call `svc.PlaceOrder(userID, &req)`
4. On error → `response.Error(w, err)`
5. On success → `response.JSON(w, http.StatusCreated, orderResponse)`

### GET /api/v1/orders ⏱ 10m (admin)

1. Parse query params: `page` and `limit` (default: page=1, limit=20)
2. Call `svc.ListAllOrders(page, limit)` → returns `([]*Order, total int, error)`
3. On error → `response.Error(w, err)`
4. On success → `response.Paginated(w, http.StatusOK, orders, meta)`

### GET /api/v1/orders/me ⏱ 10m (auth required)

1. Get `userID` from context
2. Call `svc.GetMyOrders(userID)`
3. On success → `response.JSON(w, http.StatusOK, orderList)`

### GET /api/v1/orders/:id ⏱ 10m (auth or admin)

1. Extract `id` path param: `r.PathValue("id")`
2. Get `userID` and `role` from context
3. Call `svc.GetOrder(userID, role, id)`
4. On error → `response.Error(w, err)`
5. On success → `response.JSON(w, http.StatusOK, orderResponse)`

### PUT /api/v1/orders/:id/status ⏱ 10m (admin)

1. Extract `id` path param
2. Decode JSON body into `OrderStatusUpdateRequest`
3. Call `svc.UpdateStatus(id, req.Status)`
4. On error → `response.Error(w, err)`
5. On success → `response.JSON(w, http.StatusOK, map[string]string{"message": "status updated"})`

---

## Step 11 — Register routes in `pkg/router/router.go` ⏱ 10m

Update `router.New()` to accept order handler:

```go
func New(
    authHandler       *auth.Handler,
    authMiddleware    func(http.Handler) http.Handler,
    adminOnly         func(http.Handler) http.Handler,
    productHandler    *product.Handler,
    inventoryHandler  *inventory.Handler,
    orderHandler      *order.Handler,
) http.Handler
```

Register order routes:

```
POST /api/v1/orders              → orderHandler.PlaceOrder      (jwt)
GET  /api/v1/orders              → orderHandler.ListAll         (jwt + adminOnly)
GET  /api/v1/orders/me           → orderHandler.GetMyOrders     (jwt)
GET  /api/v1/orders/{id}         → orderHandler.GetOrder        (jwt)
PUT  /api/v1/orders/{id}/status  → orderHandler.UpdateStatus    (jwt + adminOnly)
```

**Important:** Register `GET /api/v1/orders/me` BEFORE `GET /api/v1/orders/{id}` — ServeMux matches more specific paths first.

---

## Step 12 — Wire order in `pkg/cmd/serve.go` ⏱ 15m

Add order wiring:

```
1. uow           := database.NewUnitOfWork(db)
2. orderRepo     := order.NewOrderRepository(db)
3. productRepo   ← reuse productRepo from Phase 2 wiring (must implement order.ProductRepository)
4. orderSvc      := order.NewOrderService(uow, orderRepo, productRepo)
5. orderHandler  := order.NewHandler(orderSvc)
6. Pass orderHandler to router.New(...)
```

**Note on productRepo:** The `product.productRepository` already has a `db *sql.DB`. Add a `GetVariantPrice` method to it to satisfy `order.ProductRepository`. Wire it in serve.go by passing the same `productRepo` variable to both `product.NewProductService` and `order.NewOrderService`.

---

## Step 13 — Test ⏱ 15m

```bash
go run . migration up
go run . serve
```

```bash
# place an order (customer token)
curl -X POST localhost:8080/api/v1/orders \
  -H "Authorization: Bearer <customer_token>" \
  -H "Content-Type: application/json" \
  -d '{
    "items": [{"variant_id":"<variant_id>","quantity":2}],
    "shipping_name": "John Doe",
    "shipping_address": "123 Main St",
    "shipping_phone": "01700000000"
  }'

# get my orders
curl -H "Authorization: Bearer <customer_token>" localhost:8080/api/v1/orders/me

# get single order (customer)
curl -H "Authorization: Bearer <customer_token>" localhost:8080/api/v1/orders/<order_id>

# list all orders (admin)
curl -H "Authorization: Bearer <admin_token>" "localhost:8080/api/v1/orders?page=1&limit=10"

# update status (admin)
curl -X PUT localhost:8080/api/v1/orders/<order_id>/status \
  -H "Authorization: Bearer <admin_token>" \
  -H "Content-Type: application/json" \
  -d '{"status":"processing"}'
```

---

## Completion Checklist

```
[ ] go run . migration up → 000006_orders and 000007_order_items applied
[ ] go build ./... → zero errors
[ ] POST /api/v1/orders → 201, order created, inventory decremented
[ ] POST /api/v1/orders with quantity exceeding stock → 400 INSUFFICIENT_STOCK
[ ] POST /api/v1/orders with empty items → 400 INVALID_INPUT
[ ] POST /api/v1/orders with missing shipping info → 400 INVALID_INPUT
[ ] GET /api/v1/orders/me → 200 with customer's orders only
[ ] GET /api/v1/orders with admin token → 200 with paginated list
[ ] GET /api/v1/orders/:id with owner customer token → 200
[ ] GET /api/v1/orders/:id with different customer token → 403 FORBIDDEN
[ ] GET /api/v1/orders/:id with admin token → 200
[ ] PUT /api/v1/orders/:id/status → 200 (valid transition)
[ ] PUT /api/v1/orders/:id/status with invalid transition (e.g. pending→delivered) → 400 INVALID_INPUT
[ ] Inventory quantity in DB matches expected value after order placement
[ ] Two concurrent orders racing on low stock: only one succeeds (DB CHECK constraint fires)
```

**Only move to Phase 5 when all boxes are checked.**

---

## Dependency Graph

```
migrations/sql/000006_orders
migrations/sql/000007_order_items
    └── internal/order/entity.go        (time only)
    └── internal/order/model.go         (time, json)
    └── internal/order/uow.go           (interfaces only — no imports)
            └── internal/database/uow.go    (order.UnitOfWork impl, *sql.Tx, apperr)
            └── internal/order/repository.go (entity, apperr, database/sql)
                    └── internal/order/service.go
                          (UnitOfWork, OrderRepository, ProductRepository interfaces, apperr)
                            └── internal/order/handler.go
                                  (OrderService, model, response, auth context helpers)
                                    └── pkg/router/router.go
                                    └── pkg/cmd/serve.go
```
