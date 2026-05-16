# Phase 2 — Product Module Implementation Plan

**Goal:** Public GET for the single product with variants. Admin-only PUT to update product info. Admin-only POST/PUT to manage variants. Establish the product module pattern used by inventory, order, and payment.

**Estimated time:** ~3h 45m  
**Prerequisites:** Phase 1 complete. All auth endpoints passing the completion checklist.

---

## Execution Order

Steps must be done in sequence — each step depends on the previous.

---

## Step 1 — Migration: product table ⏱ 10m

**File:** `migrations/sql/000003_product.up.sql`

```sql
CREATE TABLE product (
    id          UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT           NOT NULL,
    description TEXT,
    price       NUMERIC(10,2)  NOT NULL,
    currency    TEXT           NOT NULL DEFAULT 'BDT',
    images      TEXT[]         NOT NULL DEFAULT '{}',
    is_active   BOOLEAN        NOT NULL DEFAULT TRUE,
    updated_at  TIMESTAMPTZ    DEFAULT NOW()
);
```

**File:** `migrations/sql/000003_product.down.sql`

```sql
DROP TABLE IF EXISTS product;
```

**Rules:**
- No `created_at` — the product table holds a single product record that is updated in place
- `images` is a Postgres `TEXT[]` array — stored as array, returned as `[]string` in Go
- `updated_at` is set manually on every UPDATE — not a DB trigger (keep it simple)

---

## Step 2 — Migration: product_variants table ⏱ 10m

**File:** `migrations/sql/000004_product_variants.up.sql`

```sql
CREATE TABLE product_variants (
    id          UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id  UUID           NOT NULL REFERENCES product(id) ON DELETE CASCADE,
    label       TEXT           NOT NULL,
    sku         TEXT           UNIQUE NOT NULL,
    extra_price NUMERIC(10,2)  NOT NULL DEFAULT 0
);
```

**File:** `migrations/sql/000004_product_variants.down.sql`

```sql
DROP TABLE IF EXISTS product_variants;
DROP TABLE IF EXISTS product;
```

**Rules:**
- `ON DELETE CASCADE` — deleting the product deletes all variants automatically
- `sku` is unique across the entire system — enforced at DB level
- `extra_price` is added to `product.price` at display time — the service or handler does this

---

## Step 3 — `internal/product/entity.go` ⏱ 10m

**Package:** `product`  
**Purpose:** Pure domain structs. Zero external imports. No json or db tags.

Define two structs:

```
Product
  ID          string
  Name        string
  Description string
  Price       float64
  Currency    string
  Images      []string
  IsActive    bool
  UpdatedAt   time.Time
  Variants    []Variant    ← populated by repository join or separate query

Variant
  ID         string
  ProductID  string
  Label      string
  SKU        string
  ExtraPrice float64
```

**Rules:**
- No `json:` or `db:` tags — this is not a DTO
- Import only `"time"` — nothing else
- `Variants []Variant` on `Product` — the repository populates this; the entity defines the shape

---

## Step 4 — `internal/product/model.go` ⏱ 15m

**Package:** `product`  
**Purpose:** HTTP request/response DTOs. These are the shapes the API accepts and returns.

Define these structs with `json:` tags:

```
UpdateProductRequest
  Name        *string  `json:"name"`         ← pointer — nil means "not provided, skip"
  Description *string  `json:"description"`
  Price       *float64 `json:"price"`
  Currency    *string  `json:"currency"`
  Images      []string `json:"images"`
  IsActive    *bool    `json:"is_active"`

VariantRequest
  Label      string  `json:"label"`
  SKU        string  `json:"sku"`
  ExtraPrice float64 `json:"extra_price"`

ProductResponse
  ID          string            `json:"id"`
  Name        string            `json:"name"`
  Description string            `json:"description"`
  Price       float64           `json:"price"`
  Currency    string            `json:"currency"`
  Images      []string          `json:"images"`
  IsActive    bool              `json:"is_active"`
  UpdatedAt   time.Time         `json:"updated_at"`
  Variants    []VariantResponse `json:"variants"`

VariantResponse
  ID         string  `json:"id"`
  Label      string  `json:"label"`
  SKU        string  `json:"sku"`
  ExtraPrice float64 `json:"extra_price"`
```

**Rules:**
- `UpdateProductRequest` uses pointer fields — a missing field in JSON decodes to nil, not zero value, preventing accidental overwrites
- `ProductResponse` and `VariantResponse` are separate from entity — never return entity structs directly from handlers

---

## Step 5 — `internal/product/repository.go` — Interfaces ⏱ 15m

**Package:** `product`  
**Purpose:** Define what the service needs from persistence. Interfaces live here; the postgres implementation lives in the same file.

### Part A — Define ProductRepository interface

```
ProductRepository interface
  GetProduct() (*Product, error)
  UpdateProduct(req *UpdateProductRequest) error
  UpsertVariant(productID string, req *VariantRequest) (*Variant, error)
  GetVariantByID(variantID string) (*Variant, error)
```

**Rules:**
- No `*sql.DB` or `*sql.Tx` in the interface — service layer never knows about DB types
- `UpsertVariant` handles both insert and update — service calls one method regardless

---

## Step 6 — `internal/product/repository.go` — Implementation ⏱ 30m

**Concrete struct:** `productRepository` with field `db *sql.DB`

### Implement GetProduct ⏱ 10m

```sql
SELECT id, name, description, price, currency, images, is_active, updated_at
FROM product
LIMIT 1
```

Then fetch variants:

```sql
SELECT id, product_id, label, sku, extra_price
FROM product_variants
WHERE product_id = $1
```

Return `apperr.ErrNotFound` if no product row exists.

### Implement UpdateProduct ⏱ 10m

Build the UPDATE only for non-nil fields from `UpdateProductRequest`. Update `updated_at = NOW()` on every call.

```sql
UPDATE product SET name = $1, description = $2, ..., updated_at = NOW()
WHERE id = $1
```

Use a conditional approach — only include a column in SET if the pointer is non-nil.

### Implement UpsertVariant ⏱ 10m

```sql
INSERT INTO product_variants (product_id, label, sku, extra_price)
VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO UPDATE SET label = $2, sku = $3, extra_price = $4
RETURNING id, product_id, label, sku, extra_price
```

For update path (variantID provided): `UPDATE product_variants SET label = $1, sku = $2, extra_price = $3 WHERE id = $4 RETURNING ...`

### Implement GetVariantByID ⏱ 5m

```sql
SELECT id, product_id, label, sku, extra_price
FROM product_variants
WHERE id = $1
```

Return `apperr.ErrNotFound` on `sql.ErrNoRows`.

### Constructor

```go
func NewProductRepository(db *sql.DB) ProductRepository
```

---

## Step 7 — `internal/product/service.go` ⏱ 45m

**Package:** `product`  
**Purpose:** Business logic. Depends on `ProductRepository` interface only.

### Part A — Define ProductService interface ⏱ 5m

```
ProductService interface
  GetProduct() (*Product, error)
  UpdateProduct(req *UpdateProductRequest) error
  AddVariant(productID string, req *VariantRequest) (*Variant, error)
  UpdateVariant(variantID string, req *VariantRequest) (*Variant, error)
```

### Part B — Concrete struct ⏱ 5m

```
productService
  repo ProductRepository
```

Constructor:
```go
func NewProductService(repo ProductRepository) ProductService
```

### Part C — Implement GetProduct ⏱ 5m

Call `repo.GetProduct()` and return the result. No business logic — straightforward read.

### Part D — Implement UpdateProduct ⏱ 10m

1. At least one non-nil field must be present in the request — return `apperr.ErrInvalidInput` if everything is nil
2. If `Price` is set and value is `<= 0` — return `apperr.ErrInvalidInput`
3. Call `repo.UpdateProduct(req)`

### Part E — Implement AddVariant ⏱ 10m

1. Validate: `Label` and `SKU` must not be empty — return `apperr.ErrInvalidInput`
2. Validate: `ExtraPrice` must be `>= 0`
3. Call `repo.UpsertVariant(productID, req)` with insert semantics (no conflict on ID)

### Part F — Implement UpdateVariant ⏱ 10m

1. Call `repo.GetVariantByID(variantID)` — return `apperr.ErrNotFound` if missing
2. Validate: `Label` and `SKU` must not be empty — return `apperr.ErrInvalidInput`
3. Call `repo.UpsertVariant(variantID, req)` with update semantics (WHERE id = variantID)

**Rules:**
- Never import `database/sql` — only use `ProductRepository` interface
- Never expose domain `*Product` struct directly to the HTTP layer — let the handler map to `ProductResponse`

---

## Step 8 — `internal/product/handler.go` ⏱ 55m

**Package:** `product`  
**Purpose:** Parse HTTP requests, call service, write responses. Zero business logic.

### Handler struct

```
Handler
  svc ProductService
```

Constructor:
```go
func NewHandler(svc ProductService) *Handler
```

### GET /api/v1/product ⏱ 10m (public)

1. Call `svc.GetProduct()`
2. Map `*Product` → `ProductResponse` (copy fields; flatten variants to `[]VariantResponse`)
3. On error → `response.Error(w, err)`
4. On success → `response.JSON(w, http.StatusOK, productResponse)`

### PUT /api/v1/product ⏱ 10m (admin)

1. Decode JSON body into `UpdateProductRequest`
2. Call `svc.UpdateProduct(&req)`
3. On error → `response.Error(w, err)`
4. On success → `response.JSON(w, http.StatusOK, map[string]string{"message": "product updated"})`

### POST /api/v1/product/variants ⏱ 15m (admin)

1. Decode JSON body into `VariantRequest`
2. Get productID — fetch the product first to get its ID, or use a hard-coded path since there's only one product
3. Call `svc.AddVariant(productID, &req)`
4. On error → `response.Error(w, err)`
5. On success → `response.JSON(w, http.StatusCreated, variantResponse)`

### PUT /api/v1/product/variants/:id ⏱ 15m (admin)

1. Extract `id` path parameter from URL: `r.PathValue("id")`
2. Decode JSON body into `VariantRequest`
3. Call `svc.UpdateVariant(id, &req)`
4. On error → `response.Error(w, err)`
5. On success → `response.JSON(w, http.StatusOK, variantResponse)`

### Private decode helper ⏱ 5m

Reuse the same `decodeJSON` pattern from auth handler:
```go
func decodeJSON(r *http.Request, dst interface{}) error
```

**Rules:**
- Path param extraction uses `r.PathValue("id")` — Go 1.22 stdlib, no framework needed
- Never call the service before decoding and validating the request body
- Map entity → DTO in the handler — never return raw entity structs

---

## Step 9 — Register routes in `pkg/router/router.go` ⏱ 10m

Update `router.New()` signature to accept product dependencies:

```go
func New(
    authHandler     *auth.Handler,
    authMiddleware  func(http.Handler) http.Handler,
    adminOnly       func(http.Handler) http.Handler,
    productHandler  *product.Handler,
) http.Handler
```

Register product routes:

```
GET  /api/v1/product                → productHandler.Get         (public)
PUT  /api/v1/product                → productHandler.Update      (admin: jwt + adminOnly)
POST /api/v1/product/variants       → productHandler.AddVariant  (admin: jwt + adminOnly)
PUT  /api/v1/product/variants/{id}  → productHandler.UpdateVariant (admin: jwt + adminOnly)
```

Apply middleware inline per route:
```go
mux.Handle("PUT /api/v1/product", adminOnly(authMiddleware(http.HandlerFunc(productHandler.Update))))
```

**Rule:** Use Go 1.22 method-based routing with `{id}` wildcard syntax for path parameters.

---

## Step 10 — Wire product in `pkg/cmd/serve.go` ⏱ 15m

Add product wiring after auth wiring:

```
1. productRepo    := product.NewProductRepository(db)
2. productSvc     := product.NewProductService(productRepo)
3. productHandler := product.NewHandler(productSvc)
4. Pass productHandler to router.New(...)
```

**Rule:** All wiring in `serve.go` only. No `New()` calls in handlers or services.

---

## Step 11 — Seed initial product and run migrations ⏱ 15m

```bash
# apply migrations
go run . migration up

# seed one product row (there's only one product in the system)
psql $DB_URL -c "
INSERT INTO product (name, description, price, currency)
VALUES ('My Product', 'The one and only product', 999.00, 'BDT');
"

# start server
go run . serve
```

Test with curl:

```bash
# get product (public)
curl localhost:8080/api/v1/product

# update product (admin — use admin JWT)
curl -X PUT localhost:8080/api/v1/product \
  -H "Authorization: Bearer <admin_token>" \
  -H "Content-Type: application/json" \
  -d '{"name":"Updated Name","price":1099.00}'

# add variant (admin)
curl -X POST localhost:8080/api/v1/product/variants \
  -H "Authorization: Bearer <admin_token>" \
  -H "Content-Type: application/json" \
  -d '{"label":"Size: L","sku":"PROD-L","extra_price":50.00}'

# update variant (admin)
curl -X PUT localhost:8080/api/v1/product/variants/<variant_id> \
  -H "Authorization: Bearer <admin_token>" \
  -H "Content-Type: application/json" \
  -d '{"label":"Size: XL","sku":"PROD-XL","extra_price":75.00}'
```

---

## Completion Checklist

```
[ ] go run . migration up → 000003_product and 000004_product_variants applied
[ ] go build ./... → zero errors
[ ] GET /api/v1/product → 200 with product and variants (no auth required)
[ ] GET /api/v1/product with no product in DB → 404 NOT_FOUND
[ ] PUT /api/v1/product with no token → 401 UNAUTHORIZED
[ ] PUT /api/v1/product with customer token → 403 FORBIDDEN
[ ] PUT /api/v1/product with admin token → 200, product updated
[ ] PUT /api/v1/product with empty body → 400 INVALID_INPUT
[ ] POST /api/v1/product/variants with admin token → 201 with new variant
[ ] POST /api/v1/product/variants with duplicate SKU → 409 CONFLICT (from DB unique constraint)
[ ] PUT /api/v1/product/variants/:id with admin token → 200, variant updated
[ ] PUT /api/v1/product/variants/nonexistent-id → 404 NOT_FOUND
```

**Only move to Phase 3 when all boxes are checked.**

---

## Dependency Graph

```
migrations/sql/000003_product
migrations/sql/000004_product_variants
    └── internal/product/entity.go          (time only)
    └── internal/product/model.go           (time, json)
            └── internal/product/repository.go  (entity, model, apperr, database/sql)
                    └── internal/product/service.go   (repository interface, apperr)
                            └── internal/product/handler.go    (ProductService, model, response)
                                    └── pkg/router/router.go  (product.Handler + auth deps)
                                    └── pkg/cmd/serve.go      (all of the above)
```
