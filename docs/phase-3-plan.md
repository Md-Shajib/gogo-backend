# Phase 3 — Inventory Module Implementation Plan

**Goal:** Track stock per variant. Admin can view all inventory and adjust quantities. Inventory decrement is called transactionally from the order module via the Unit of Work pattern — the inventory repository must be callable inside a `*sql.Tx` without exposing `*sql.Tx` to the service layer.

**Estimated time:** ~2h 45m  
**Prerequisites:** Phase 2 complete. Product and variants exist in DB.

---

## Execution Order

Steps must be done in sequence — each step depends on the previous.

---

## Step 1 — Migration: inventory table ⏱ 10m

**File:** `migrations/sql/000005_inventory.up.sql`

```sql
CREATE TABLE inventory (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    variant_id   UUID        NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    quantity     INT         NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    low_stock_at INT         NOT NULL DEFAULT 5,
    updated_at   TIMESTAMPTZ DEFAULT NOW()
);
```

**File:** `migrations/sql/000005_inventory.down.sql`

```sql
DROP TABLE IF EXISTS inventory;
```

**Rules:**
- `CHECK (quantity >= 0)` — DB-level safety net; service also validates but DB is the source of truth
- `ON DELETE CASCADE` — deleting a variant removes its inventory row automatically
- One row per variant — enforced by design; the service never inserts a second row for the same `variant_id`
- `low_stock_at` default is 5 — admin can adjust this via the stock adjustment endpoint

---

## Step 2 — `internal/inventory/entity.go` ⏱ 10m

**Package:** `inventory`  
**Purpose:** Pure domain struct. Zero external imports. No json or db tags.

```
Inventory
  ID         string
  VariantID  string
  Quantity   int
  LowStockAt int
  UpdatedAt  time.Time
```

**Rules:**
- No `json:` or `db:` tags — this is not a DTO
- Import only `"time"` — nothing else

---

## Step 3 — `internal/inventory/model.go` ⏱ 10m

**Package:** `inventory`  
**Purpose:** HTTP request/response DTOs.

```
AdjustStockRequest
  Quantity   int `json:"quantity"`     ← absolute quantity to set (not a delta)
  LowStockAt int `json:"low_stock_at"` ← optional; 0 means "don't update"

InventoryResponse
  ID         string    `json:"id"`
  VariantID  string    `json:"variant_id"`
  Quantity   int       `json:"quantity"`
  LowStockAt int       `json:"low_stock_at"`
  UpdatedAt  time.Time `json:"updated_at"`
```

**Rules:**
- `Quantity` is an absolute value, not a delta — avoids race conditions on concurrent adjustments
- No variant label or SKU in `InventoryResponse` — the client can join with the product data it already has

---

## Step 4 — `internal/inventory/repository.go` — Interfaces ⏱ 15m

**Package:** `inventory`  
**Purpose:** Define what the service and the Unit of Work need from persistence.

### InventoryRepository interface

```
InventoryRepository interface
  GetByVariantID(variantID string) (*Inventory, error)
  GetAll() ([]*Inventory, error)
  SetQuantity(variantID string, quantity int, lowStockAt *int) error
  DecrementStock(variantID string, qty int) error
```

**Key design decision — `DecrementStock`:**

`DecrementStock` is also called from inside a DB transaction (during order placement). The Unit of Work pattern handles this by providing a separate implementation of this method that operates on a `*sql.Tx`. The interface signature is identical — `variantID string, qty int` — no `*sql.Tx` parameter.

The order UoW creates a `StockDecrementer` (defined in `internal/order/uow.go`) that wraps `*sql.Tx`. The inventory module does not know about this — it only exposes `DecrementStock` for use within its own service, and the UoW implementation calls the same SQL but through the transaction.

**Rules:**
- No `*sql.DB` or `*sql.Tx` in the interface — the service never touches DB types
- `SetQuantity` uses an absolute value — matches the HTTP API semantics

---

## Step 5 — `internal/inventory/repository.go` — Implementation ⏱ 30m

**Concrete struct:** `inventoryRepository` with field `db *sql.DB`

### Implement GetByVariantID ⏱ 5m

```sql
SELECT id, variant_id, quantity, low_stock_at, updated_at
FROM inventory
WHERE variant_id = $1
```

Return `apperr.ErrNotFound` on `sql.ErrNoRows`.

### Implement GetAll ⏱ 5m

```sql
SELECT id, variant_id, quantity, low_stock_at, updated_at
FROM inventory
ORDER BY variant_id
```

Return an empty slice (not nil) if no rows.

### Implement SetQuantity ⏱ 10m

```sql
INSERT INTO inventory (variant_id, quantity, low_stock_at, updated_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (variant_id) DO UPDATE
  SET quantity = $2, low_stock_at = COALESCE($3, inventory.low_stock_at), updated_at = NOW()
```

Pass `lowStockAt` as `*int` — if nil, the `COALESCE` keeps the existing value.

### Implement DecrementStock ⏱ 10m

```sql
UPDATE inventory
SET quantity = quantity - $2, updated_at = NOW()
WHERE variant_id = $1 AND quantity >= $2
```

Check `rows.RowsAffected()` — if 0, the stock was insufficient; return `apperr.ErrInsufficientStock`.

### Constructor

```go
func NewInventoryRepository(db *sql.DB) InventoryRepository
```

---

## Step 6 — `internal/inventory/service.go` ⏱ 45m

**Package:** `inventory`  
**Purpose:** Business logic. Depends on `InventoryRepository` interface only.

### Part A — Define InventoryService interface ⏱ 5m

```
InventoryService interface
  GetInventory() ([]*Inventory, error)
  AdjustStock(variantID string, req *AdjustStockRequest) (*Inventory, error)
```

### Part B — Concrete struct ⏱ 5m

```
inventoryService
  repo InventoryRepository
```

Constructor:
```go
func NewInventoryService(repo InventoryRepository) InventoryService
```

### Part C — Implement GetInventory ⏱ 5m

Call `repo.GetAll()` and return the slice. No business logic needed.

### Part D — Implement AdjustStock ⏱ 30m

1. Validate: `Quantity` must be `>= 0` — return `apperr.ErrInvalidInput` if negative
2. Validate: `LowStockAt` must be `>= 0` if provided
3. Call `repo.SetQuantity(variantID, req.Quantity, lowStockAtPtr)`
4. Call `repo.GetByVariantID(variantID)` to fetch updated record
5. **Low stock warning:** If returned `Quantity <= LowStockAt`, log a warning using `log/slog`:
   ```go
   slog.Warn("low stock", "variant_id", variantID, "quantity", inv.Quantity, "threshold", inv.LowStockAt)
   ```
6. Return the updated `*Inventory`

**Rules:**
- Never import `database/sql` — only use `InventoryRepository` interface
- Use `log/slog` for the low-stock warning — not `fmt.Println` or `log.Printf`
- The `DecrementStock` method on the repository is NOT called from this service — it is called by the order UoW implementation only

---

## Step 7 — `internal/inventory/handler.go` ⏱ 30m

**Package:** `inventory`  
**Purpose:** Parse HTTP requests, call service, write responses. Zero business logic.

### Handler struct

```
Handler
  svc InventoryService
```

Constructor:
```go
func NewHandler(svc InventoryService) *Handler
```

### GET /api/v1/inventory ⏱ 10m (admin)

1. Call `svc.GetInventory()`
2. Map `[]*Inventory` → `[]InventoryResponse`
3. On error → `response.Error(w, err)`
4. On success → `response.JSON(w, http.StatusOK, inventoryList)`

### PUT /api/v1/inventory/:variant_id ⏱ 15m (admin)

1. Extract `variant_id` path parameter: `r.PathValue("variant_id")`
2. Validate: `variant_id` must not be empty — return `apperr.ErrInvalidInput`
3. Decode JSON body into `AdjustStockRequest`
4. Call `svc.AdjustStock(variantID, &req)`
5. On error → `response.Error(w, err)`
6. On success → `response.JSON(w, http.StatusOK, inventoryResponse)`

### Private decode helper ⏱ 5m

```go
func decodeJSON(r *http.Request, dst interface{}) error
```

Same pattern as auth and product handlers.

---

## Step 8 — Register routes in `pkg/router/router.go` ⏱ 10m

Update `router.New()` signature to accept inventory handler:

```go
func New(
    authHandler       *auth.Handler,
    authMiddleware    func(http.Handler) http.Handler,
    adminOnly         func(http.Handler) http.Handler,
    productHandler    *product.Handler,
    inventoryHandler  *inventory.Handler,
) http.Handler
```

Register inventory routes (all admin):

```
GET /api/v1/inventory                  → inventoryHandler.List         (jwt + adminOnly)
PUT /api/v1/inventory/{variant_id}     → inventoryHandler.AdjustStock  (jwt + adminOnly)
```

---

## Step 9 — Wire inventory in `pkg/cmd/serve.go` ⏱ 10m

Add inventory wiring after product wiring:

```
1. inventoryRepo    := inventory.NewInventoryRepository(db)
2. inventorySvc     := inventory.NewInventoryService(inventoryRepo)
3. inventoryHandler := inventory.NewHandler(inventorySvc)
4. Pass inventoryHandler to router.New(...)
```

**Rule:** All wiring in `serve.go` only.

---

## Step 10 — Seed inventory rows and test ⏱ 15m

After seeding variants in Phase 2, seed inventory:

```bash
go run . migration up

# seed inventory for each variant
psql $DB_URL -c "
INSERT INTO inventory (variant_id, quantity, low_stock_at)
SELECT id, 100, 5 FROM product_variants;
"

go run . serve
```

Test with curl:

```bash
# list all inventory (admin)
curl -H "Authorization: Bearer <admin_token>" localhost:8080/api/v1/inventory

# adjust stock (admin)
curl -X PUT localhost:8080/api/v1/inventory/<variant_id> \
  -H "Authorization: Bearer <admin_token>" \
  -H "Content-Type: application/json" \
  -d '{"quantity":50,"low_stock_at":10}'

# try with customer token (should fail)
curl -X PUT localhost:8080/api/v1/inventory/<variant_id> \
  -H "Authorization: Bearer <customer_token>" \
  -H "Content-Type: application/json" \
  -d '{"quantity":50}'
```

---

## Completion Checklist

```
[ ] go run . migration up → 000005_inventory applied
[ ] go build ./... → zero errors
[ ] GET /api/v1/inventory with no token → 401 UNAUTHORIZED
[ ] GET /api/v1/inventory with customer token → 403 FORBIDDEN
[ ] GET /api/v1/inventory with admin token → 200 with inventory list
[ ] PUT /api/v1/inventory/:variant_id with admin token → 200, quantity updated
[ ] PUT /api/v1/inventory/:variant_id with quantity -1 → 400 INVALID_INPUT
[ ] PUT /api/v1/inventory/nonexistent-id → 404 NOT_FOUND (or 200 with insert via upsert)
[ ] slog.Warn appears in server logs when quantity is set below low_stock_at
[ ] CHECK constraint fires when quantity would go below 0 (test via direct SQL)
```

**Only move to Phase 4 when all boxes are checked.**

---

## Dependency Graph

```
migrations/sql/000005_inventory
    └── internal/inventory/entity.go          (time only)
    └── internal/inventory/model.go           (time, json)
            └── internal/inventory/repository.go  (entity, model, apperr, database/sql)
                    └── internal/inventory/service.go   (repository interface, apperr, log/slog)
                            └── internal/inventory/handler.go    (InventoryService, model, response)
                                    └── pkg/router/router.go  (inventory.Handler + previous deps)
                                    └── pkg/cmd/serve.go      (all of the above)

Note: inventory.DecrementStock SQL pattern is reused by internal/database/uow.go
      in Phase 4 — the interface stays clean, only the transaction wrapping changes.
```
