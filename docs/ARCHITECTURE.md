# Backend Architecture

Single vendor · Single product · E-Commerce · Go

---

## Stack

| Concern             | Choice                                            | Notes                                      |
|---------------------|---------------------------------------------------|--------------------------------------------|
| Language            | Go 1.21+                                          |                                            |
| CLI                 | `github.com/spf13/cobra`                          | External — subcommand dispatch             |
| HTTP Server         | `net/http` (stdlib)                               | No framework                               |
| Router              | Manual (stdlib `ServeMux`)                        | Path param extraction written by hand      |
| Middleware          | Handler wrapping pattern (stdlib)                 |                                            |
| JSON                | `encoding/json` (stdlib)                          |                                            |
| Logging             | `log/slog` (stdlib, Go 1.21+)                     |                                            |
| Config              | `os.Getenv` (stdlib)                              | `.env` loaded manually at startup          |
| JWT                 | Manual — `crypto/hmac` + `crypto/sha256` (stdlib) | No jwt library                             |
| Password hashing    | `golang.org/x/crypto/bcrypt`                      | External — no stdlib alternative           |
| Database            | PostgreSQL                                        |                                            |
| DB driver           | `github.com/lib/pq`                               | External — no stdlib PostgreSQL driver     |
| DB interface        | `database/sql` (stdlib)                           | Raw SQL only, no ORM                       |
| Migrations          | Manual SQL runner (stdlib)                        | Execute `.sql` files via `database/sql`    |
| Payment             | Stripe — raw `net/http` calls                     | No Stripe SDK                              |

> **Rule:** Only three external packages are allowed — `github.com/spf13/cobra` (CLI), `lib/pq` (DB driver), and `golang.org/x/crypto/bcrypt` (password hashing). Everything else is stdlib.

---

## Project Structure

```
gogo-backend/
├── main.go                          # entrypoint — calls cmd.Execute()
│
├── internal/
│   ├── auth/
│   │   ├── entity.go                # domain: User struct — pure Go, no json/db tags
│   │   ├── model.go                 # HTTP DTOs: RegisterRequest, LoginRequest, TokenResponse
│   │   ├── token.go                 # TokenService interface — decouples service from JWT impl
│   │   ├── repository.go            # UserRepository + TokenRepository interfaces + postgres impl
│   │   ├── service.go               # business logic — depends on UserRepository + TokenRepository + TokenService interfaces
│   │   ├── handler.go               # HTTP handlers — depends on AuthService interface
│   │   └── middleware.go            # JWT validation — depends on TokenService interface
│   │
│   ├── product/
│   │   ├── entity.go                # domain: Product, Variant structs — pure Go
│   │   ├── model.go                 # HTTP DTOs: UpdateProductRequest, VariantRequest
│   │   ├── repository.go            # ProductRepository interface + postgres implementation
│   │   ├── service.go               # business logic — depends on ProductRepository interface
│   │   └── handler.go               # HTTP handlers — depends on ProductService interface
│   │
│   ├── inventory/
│   │   ├── entity.go                # domain: Inventory struct — pure Go
│   │   ├── model.go                 # HTTP DTOs: AdjustStockRequest
│   │   ├── repository.go            # InventoryRepository interface + postgres implementation
│   │   ├── service.go               # business logic — depends on InventoryRepository interface
│   │   └── handler.go               # HTTP handlers — depends on InventoryService interface
│   │
│   ├── order/
│   │   ├── entity.go                # domain: Order, OrderItem structs — pure Go
│   │   ├── model.go                 # HTTP DTOs: PlaceOrderRequest, OrderStatusUpdateRequest
│   │   ├── uow.go                   # UnitOfWork, Transaction, StockDecrementer interfaces — no *sql.Tx in service
│   │   ├── repository.go            # OrderRepository interface + postgres implementation (no *sql.Tx in interface)
│   │   ├── service.go               # business logic — depends on UnitOfWork + OrderRepository interfaces
│   │   └── handler.go               # HTTP handlers — depends on OrderService interface
│   │
│   ├── payment/
│   │   ├── entity.go                # domain: Payment struct — pure Go
│   │   ├── model.go                 # HTTP DTOs: InitiateResponse
│   │   ├── gateway.go               # PaymentGateway interface — decouples service from Stripe
│   │   ├── repository.go            # PaymentRepository interface + postgres implementation
│   │   ├── service.go               # business logic — defines OrderStatusUpdater interface; depends on PaymentGateway + PaymentRepository + OrderStatusUpdater interfaces
│   │   └── handler.go               # HTTP handlers — depends on PaymentService interface
│   │
│   └── database/
│       ├── postgres.go              # open *sql.DB, ping, expose pool
│       └── uow.go                   # postgres UnitOfWork impl — wraps *sql.Tx, implements order.Transaction
│
├── pkg/
│   ├── apperr/
│   │   └── errors.go                # typed domain errors: AppError, ErrNotFound, ErrUnauthorized, ErrForbidden, ErrInvalidInput, ErrInsufficientStock, ErrConflict
│   ├── cmd/
│   │   ├── root.go                  # cobra root command + Execute()
│   │   ├── serve.go                 # "serve" subcommand — wires and starts HTTP server
│   │   └── migration/
│   │       ├── migration.go         # "migration" parent subcommand
│   │       ├── up.go                # "migration up" subcommand
│   │       └── down.go              # "migration down" subcommand
│   ├── config/
│   │   └── config.go                # load + validate env vars into a Config struct
│   ├── router/
│   │   └── router.go                # register all routes, apply middleware
│   ├── middleware/
│   │   ├── logger.go                # request logger middleware
│   │   ├── recover.go               # panic recovery middleware
│   │   └── cors.go                  # CORS headers middleware
│   ├── jwt/
│   │   └── jwt.go                   # implements auth.TokenService interface — manual HMAC-SHA256
│   └── response/
│       └── response.go              # JSON(w, status, data), Error(w, status, code, msg) — maps *apperr.AppError to HTTP status
│
├── migrations/
│   ├── runner.go                    # reads + executes .sql files in order
│   └── sql/
│       ├── 000001_users.up.sql
│       ├── 000001_users.down.sql
│       ├── 000002_refresh_tokens.up.sql
│       ├── 000002_refresh_tokens.down.sql
│       ├── 000003_product.up.sql
│       ├── 000003_product.down.sql
│       ├── 000004_product_variants.up.sql
│       ├── 000004_product_variants.down.sql
│       ├── 000005_inventory.up.sql
│       ├── 000005_inventory.down.sql
│       ├── 000006_orders.up.sql
│       ├── 000006_orders.down.sql
│       ├── 000007_order_items.up.sql
│       ├── 000007_order_items.down.sql
│       ├── 000008_payments.up.sql
│       └── 000008_payments.down.sql
│
├── .env.example
├── .gitignore
├── docker-compose.yml
├── Dockerfile
├── go.mod
├── go.sum
├── TODO.md
└── ARCHITECTURE.md
```

---

## DB Schema

### users
```sql
CREATE TABLE users (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email       TEXT        UNIQUE NOT NULL,
    password    TEXT        NOT NULL,                      -- bcrypt hash
    role        TEXT        NOT NULL DEFAULT 'customer',   -- 'customer' | 'admin'
    created_at  TIMESTAMPTZ DEFAULT NOW()
);
```

### refresh_tokens
```sql
CREATE TABLE refresh_tokens (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token       TEXT        UNIQUE NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);
```

### product
```sql
CREATE TABLE product (
    id          UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT           NOT NULL,
    description TEXT,
    price       NUMERIC(10,2)  NOT NULL,
    currency    TEXT           NOT NULL DEFAULT 'BDT',
    images      TEXT[],                                   -- array of image URLs
    is_active   BOOLEAN        DEFAULT TRUE,
    updated_at  TIMESTAMPTZ    DEFAULT NOW()
);
```

### product_variants
```sql
CREATE TABLE product_variants (
    id          UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id  UUID           NOT NULL REFERENCES product(id) ON DELETE CASCADE,
    label       TEXT           NOT NULL,                  -- e.g. "Size: L"
    sku         TEXT           UNIQUE NOT NULL,
    extra_price NUMERIC(10,2)  NOT NULL DEFAULT 0
);
```

### inventory
```sql
CREATE TABLE inventory (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    variant_id   UUID        NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    quantity     INT         NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    low_stock_at INT         NOT NULL DEFAULT 5,
    updated_at   TIMESTAMPTZ DEFAULT NOW()
);
```

### orders
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

### order_items
```sql
CREATE TABLE order_items (
    id          UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    UUID           NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    variant_id  UUID           NOT NULL REFERENCES product_variants(id),
    quantity    INT            NOT NULL CHECK (quantity > 0),
    unit_price  NUMERIC(10,2)  NOT NULL   -- price snapshot at time of order
);
```

### payments
```sql
CREATE TYPE payment_status AS ENUM (
    'initiated', 'success', 'failed', 'refunded'
);

CREATE TABLE payments (
    id              UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id        UUID           NOT NULL REFERENCES orders(id),
    gateway         TEXT           NOT NULL,              -- 'stripe'
    gateway_ref     TEXT           UNIQUE,                -- Stripe PaymentIntent ID
    amount          NUMERIC(10,2)  NOT NULL,
    currency        TEXT           NOT NULL,
    status          payment_status NOT NULL DEFAULT 'initiated',
    webhook_payload JSONB,                                -- raw webhook stored for audit
    created_at      TIMESTAMPTZ    DEFAULT NOW(),
    updated_at      TIMESTAMPTZ    DEFAULT NOW()
);
```

---

## Routes (Endpoints)

Base path: `/api/v1`

### Auth
| Method | Path                    | Access  | Description                        |
|--------|-------------------------|---------|------------------------------------|
| POST   | `/auth/register`        | Public  | Register new customer account      |
| POST   | `/auth/login`           | Public  | Login, returns JWT + refresh token |
| POST   | `/auth/refresh`         | Public  | Issue new JWT via refresh token    |
| POST   | `/auth/logout`          | Auth    | Invalidate refresh token           |

### Product
| Method | Path                        | Access  | Description                        |
|--------|-----------------------------|---------|------------------------------------|
| GET    | `/product`                  | Public  | Get product details with variants  |
| PUT    | `/product`                  | Admin   | Update product info                |
| POST   | `/product/variants`         | Admin   | Add a new variant                  |
| PUT    | `/product/variants/:id`     | Admin   | Update an existing variant         |

### Inventory
| Method | Path                        | Access  | Description                        |
|--------|-----------------------------|---------|------------------------------------|
| GET    | `/inventory`                | Admin   | List all variants with stock       |
| PUT    | `/inventory/:variant_id`    | Admin   | Set stock quantity for a variant   |

### Orders
| Method | Path                        | Access        | Description                        |
|--------|-----------------------------|---------------|------------------------------------|
| POST   | `/orders`                   | Auth          | Place a new order                  |
| GET    | `/orders`                   | Admin         | List all orders (paginated)        |
| GET    | `/orders/me`                | Auth          | Get current user's order history   |
| GET    | `/orders/:id`               | Auth / Admin  | Get order detail                   |
| PUT    | `/orders/:id/status`        | Admin         | Update order status                |

### Payments
| Method | Path                            | Access  | Description                             |
|--------|---------------------------------|---------|-----------------------------------------|
| POST   | `/payments/initiate/:order_id`  | Auth    | Create Stripe PaymentIntent             |
| POST   | `/payments/webhook`             | Public* | Stripe webhook callback                 |
| GET    | `/payments/:order_id`           | Auth    | Get payment status for an order         |
| POST   | `/payments/refund/:order_id`    | Admin   | Issue refund via Stripe                 |

> `*` Webhook is public but verified by Stripe signature on every request.

---

## Clean Architecture Layer Map

```
┌────────────────────────────────────────────────────────┐
│              Frameworks & Drivers                      │
│  net/http · PostgreSQL · Stripe API                    │
│  pkg/stripe/client.go  ← implements payment.Gateway   │
│  internal/database/uow.go ← implements order.UnitOfWork│
│  internal/database/postgres.go                         │
│  pkg/jwt/jwt.go  ← implements auth.TokenService        │
└──────────────────────────┬─────────────────────────────┘
                           │ implements
┌──────────────────────────▼─────────────────────────────┐
│              Interface Adapters                        │
│  handler.go  ·  repository.go  ·  middleware.go        │
│  (adapts HTTP ↔ domain · adapts DB ↔ domain)          │
└──────────────────────────┬─────────────────────────────┘
                           │ implements interfaces defined by
┌──────────────────────────▼─────────────────────────────┐
│              Use Cases / Services                      │
│  service.go — business logic                           │
│  repository.go — defines Repository interface          │
│  token.go    — defines TokenService interface          │
│  gateway.go  — defines PaymentGateway interface        │
│  uow.go      — defines UnitOfWork + Transaction ifaces │
└──────────────────────────┬─────────────────────────────┘
                           │ uses
┌──────────────────────────▼─────────────────────────────┐
│              Entities                                  │
│  entity.go in each module — pure Go structs            │
│  pkg/apperr/errors.go — typed domain errors            │
│  (zero dependencies — innermost layer)                 │
└────────────────────────────────────────────────────────┘
```

**Dependency rule:** every arrow points inward only. Nothing in an inner layer imports from an outer layer. Entities and domain errors have zero imports. Services import only entities and their own interfaces. Handlers and repositories are outer — they import service interfaces and implement them.

---

---

## Rules

### Architecture
- **Handler → Service → Repository** — strict layering. Handlers only parse and respond. Services own all business logic. Repositories own all SQL.
- No business logic in handlers. No SQL in services.
- All DB access goes through `database/sql` with parameterised queries only — never string-concatenated SQL.

### Clean Architecture
- **Entity vs DTO separation** — `entity.go` holds pure domain structs (no `json:` or `db:` tags). `model.go` holds HTTP request/response DTOs. Domain shape is never dictated by HTTP concerns.
- **Repository interfaces** — every `service.go` defines the repository interface it needs. The concrete postgres implementation in `repository.go` satisfies that interface. Services never import `database/sql` directly.
- **Payment gateway abstraction** — `internal/payment/gateway.go` defines the `PaymentGateway` interface. `pkg/stripe/client.go` implements it. The payment service never knows it is Stripe — swapping providers requires zero service changes.
- **Service interfaces** — every `handler.go` depends on a service interface, not the concrete struct. Keeps handlers independently testable.
- **Token service abstraction** — `internal/auth/token.go` defines the `TokenService` interface (`GenerateAccessToken`, `GenerateRefreshToken`, `ParseToken`). `pkg/jwt/jwt.go` implements it. The auth service and middleware never import `pkg/jwt` directly — they depend only on the interface.
- **Unit of Work — no `*sql.Tx` in the service layer** — `internal/order/uow.go` defines `UnitOfWork`, `Transaction`, and `StockDecrementer` interfaces. The order service calls `uow.Begin()` and operates on the `Transaction` interface — it never sees `*sql.Tx`. The postgres implementation in `internal/database/uow.go` wraps `*sql.Tx` internally.
- **Typed domain errors** — `pkg/apperr/errors.go` defines `AppError` and sentinel errors (`ErrNotFound`, `ErrUnauthorized`, `ErrForbidden`, `ErrInvalidInput`, `ErrInsufficientStock`, `ErrConflict`). Services return `*apperr.AppError`. The response layer maps error codes to HTTP status codes in one place — no scattered `http.StatusXxx` calls in business logic.
- **Cross-module communication via interface** — modules never import each other directly. When the payment service needs to update an order status after a successful webhook, it defines its own `OrderStatusUpdater` interface in `internal/payment/service.go`. The order repository's existing `UpdateOrderStatus()` satisfies it. Wiring happens at the composition root (`pkg/cmd/serve.go`) — zero cross-module coupling in the inner layer.

### External Packages
- Only three external packages allowed: `github.com/spf13/cobra` (CLI), `lib/pq` (DB driver), `golang.org/x/crypto/bcrypt` (password hashing).
- Everything else — routing, middleware, JWT, migrations, Stripe calls — is implemented manually using stdlib.

### Auth & Security
- Passwords stored as bcrypt hash only. Plain text never logged or stored.
- JWT uses HMAC-SHA256, signed with `JWT_SECRET` from env. Access token TTL: 15 minutes. Refresh token TTL: 7 days.
- Refresh tokens stored in DB. Logout deletes the token row (no blacklist needed).
- Stripe webhook requests verified via HMAC signature before any processing.
- No secrets, keys, or credentials in source code. All via `.env` (never committed).

### Response Format
Every response uses a standard JSON envelope:
```json
// success
{ "success": true,  "data": { ... }, "error": null }

// failure
{ "success": false, "data": null,   "error": { "code": "INSUFFICIENT_STOCK", "message": "Not enough stock." } }
```

### Order & Inventory
- Order placement runs inside a single DB transaction: validate stock → snapshot price → decrement inventory → insert order + items. Any failure rolls back the entire transaction.
- `inventory.quantity` has a `CHECK (quantity >= 0)` constraint at DB level. Never goes negative.
- `order_items.unit_price` stores the price at the moment of purchase — not a foreign key to current price.
- Log a warning when `inventory.quantity` drops below `low_stock_at`.

### Allowed Order Status Transitions
```
pending → paid → processing → shipped → delivered
pending → cancelled
paid    → refunded
```
Any other transition is rejected with a 400 error.

### Payment
- Stripe PaymentIntent is created server-side. `client_secret` is returned to the frontend.
- Frontend completes payment using Stripe.js (no card data touches the server).
- Order status is updated to `paid` only after a verified Stripe webhook event — never on client callback alone.
- Raw webhook payload is stored in `payments.webhook_payload` (JSONB) for audit.

### Migrations
- Migration files are numbered sequentially: `000001_`, `000002_`, etc.
- Each migration has a `.up.sql` and `.down.sql`.
- The migration runner tracks applied migrations in a `schema_migrations` table.
- Never edit an already-applied migration — add a new one instead.

### Code Style
- No comments unless the reason is non-obvious.
- No ORM — raw SQL with named parameters via `database/sql`.
- Validate all input at the handler boundary. Trust nothing from the request.
- Return domain errors from services; map them to HTTP status codes in a single error handler.
