# E-Commerce Backend — Task Plan

Single vendor · Single product · Go + net/http + PostgreSQL + Stripe

**Legend:** `[ ]` todo · `[x]` done · ⏱ = estimated time

---

## Phase 0 — Project Setup
> Goal: runnable server with DB connection, config, and routing skeleton

- [ ] ⏱ 10m — Add `cobra`, `lib/pq`, `golang.org/x/crypto/bcrypt` to `go.mod`
- [ ] ⏱ 10m — Create folder structure (`internal/`, `pkg/apperr/`, `pkg/cmd/migration/`, `pkg/config/`, `pkg/router/`, `pkg/middleware/`, `pkg/jwt/`, `pkg/response/`, `pkg/stripe/`, `migrations/sql/`)
- [ ] ⏱ 15m — `main.go` — calls `cmd.Execute()` only
- [ ] ⏱ 20m — `pkg/cmd/root.go` — cobra root command + `Execute()`
- [ ] ⏱ 15m — `pkg/cmd/serve.go` — "serve" subcommand skeleton
- [ ] ⏱ 15m — `pkg/cmd/migration/migration.go` — "migration" parent subcommand
- [ ] ⏱ 10m — `pkg/cmd/migration/up.go` — "migration up" subcommand
- [ ] ⏱ 10m — `pkg/cmd/migration/down.go` — "migration down" subcommand
- [ ] ⏱ 15m — `pkg/config/config.go` — load + validate env vars (DB_URL, JWT_SECRET, PORT, STRIPE_KEY, STRIPE_WEBHOOK_SECRET)
- [ ] ⏱ 15m — `pkg/middleware/logger.go` — request logger using `log/slog`
- [ ] ⏱ 15m — `pkg/middleware/recover.go` — panic recovery middleware
- [ ] ⏱ 15m — `pkg/middleware/cors.go` — CORS headers middleware
- [ ] ⏱ 20m — `internal/database/postgres.go` — open `*sql.DB` via `lib/pq`, ping, set pool limits
- [ ] ⏱ 15m — `pkg/apperr/errors.go` — `AppError` struct + sentinel errors: `ErrNotFound`, `ErrUnauthorized`, `ErrForbidden`, `ErrInvalidInput`, `ErrInsufficientStock`, `ErrConflict`
- [ ] ⏱ 20m — `pkg/response/response.go` — standard JSON envelope; `Error()` maps `*apperr.AppError` to HTTP status in one place
- [ ] ⏱ 20m — `pkg/router/router.go` — `ServeMux` + middleware chain + health check route
- [ ] ⏱ 20m — `pkg/cmd/serve.go` — wire config → logger → db → router → `http.Server` with graceful shutdown
- [ ] ⏱ 20m — `migrations/runner.go` — read + execute `.sql` files, track in `schema_migrations` table
- [ ] ⏱ 15m — `docker-compose.yml` — postgres + app services with env file
- [ ] ⏱ 15m — `Dockerfile` — multi-stage build

**Phase 0 total ⏱ ~4h 45m**

---

## Phase 1 — Auth Module
> Goal: register, login, JWT, refresh, logout

### Migrations
- [ ] ⏱ 10m — `000001_users.up.sql` — users table (id, email, password, role, created_at)
- [ ] ⏱ 5m  — `000001_users.down.sql`
- [ ] ⏱ 10m — `000002_refresh_tokens.up.sql` — refresh_tokens table (id, user_id, token, expires_at)
- [ ] ⏱ 5m  — `000002_refresh_tokens.down.sql`

### Entities
- [ ] ⏱ 10m — `internal/auth/entity.go` — `User` struct (pure domain, no json/db tags)

### Models (HTTP DTOs)
- [ ] ⏱ 15m — `internal/auth/model.go` — `RegisterRequest`, `LoginRequest`, `TokenResponse` (json tags only)

### Repository
- [ ] ⏱ 15m — `internal/auth/repository.go` — define `UserRepository` + `TokenRepository` interfaces
- [ ] ⏱ 15m — `internal/auth/repository.go` — implement `InsertUser()`, `FindUserByEmail()`, `FindUserByID()`
- [ ] ⏱ 15m — `internal/auth/repository.go` — implement `InsertRefreshToken()`, `FindRefreshToken()`, `DeleteRefreshToken()`

### Token Service Interface
- [ ] ⏱ 15m — `internal/auth/token.go` — define `TokenService` interface: `GenerateAccessToken(userID, role string)`, `GenerateRefreshToken()`, `ParseToken(token string) (Claims, error)`

### JWT Implementation (implements TokenService)
- [ ] ⏱ 20m — `pkg/jwt/jwt.go` — implement `auth.TokenService` interface using manual HMAC-SHA256 — no jwt library

### Service
- [ ] ⏱ 15m — `internal/auth/service.go` — define `AuthService` interface + concrete struct depending on `UserRepository`, `TokenRepository`, and `TokenService` interfaces
- [ ] ⏱ 20m — `internal/auth/service.go` — `Register()` — validate input, hash password, insert user
- [ ] ⏱ 20m — `internal/auth/service.go` — `Login()` — verify password, issue JWT + refresh token
- [ ] ⏱ 15m — `internal/auth/service.go` — `RefreshToken()` — validate refresh token, issue new JWT
- [ ] ⏱ 10m — `internal/auth/service.go` — `Logout()` — delete refresh token

### Handler
- [ ] ⏱ 15m — `internal/auth/handler.go` — define handler struct depending on `AuthService` interface
- [ ] ⏱ 15m — `internal/auth/handler.go` — `POST /api/v1/auth/register`
- [ ] ⏱ 15m — `internal/auth/handler.go` — `POST /api/v1/auth/login`
- [ ] ⏱ 10m — `internal/auth/handler.go` — `POST /api/v1/auth/refresh`
- [ ] ⏱ 10m — `internal/auth/handler.go` — `POST /api/v1/auth/logout`

### Middleware
- [ ] ⏱ 20m — `internal/auth/middleware.go` — JWT middleware depends on `TokenService` interface (extract + validate token, inject user_id/role into context)
- [ ] ⏱ 10m — `internal/auth/middleware.go` — `AdminOnly()` — role guard middleware

### Routes
- [ ] ⏱ 10m — Register auth routes in `pkg/router/router.go`

**Phase 1 total ⏱ ~5h 30m**

---

## Phase 2 — Product Module
> Goal: public GET and admin-only PUT for the single product and its variants

### Migrations
- [ ] ⏱ 10m — `000003_product.up.sql` — product table (id, name, description, price, currency, images[], is_active, updated_at)
- [ ] ⏱ 5m  — `000003_product.down.sql`
- [ ] ⏱ 10m — `000004_product_variants.up.sql` — product_variants table (id, product_id, label, sku, extra_price)
- [ ] ⏱ 5m  — `000004_product_variants.down.sql`

### Entities
- [ ] ⏱ 10m — `internal/product/entity.go` — `Product`, `Variant` structs (pure domain, no json/db tags)

### Models (HTTP DTOs)
- [ ] ⏱ 15m — `internal/product/model.go` — `UpdateProductRequest`, `VariantRequest` (json tags only)

### Repository
- [ ] ⏱ 15m — `internal/product/repository.go` — define `ProductRepository` interface
- [ ] ⏱ 15m — `internal/product/repository.go` — implement `GetProduct()` — fetch product with variants
- [ ] ⏱ 15m — `internal/product/repository.go` — implement `UpdateProduct()`, `UpsertVariant()`, `GetVariantByID()`

### Service
- [ ] ⏱ 15m — `internal/product/service.go` — define `ProductService` interface + concrete struct depending on `ProductRepository` interface
- [ ] ⏱ 15m — `internal/product/service.go` — `GetProduct()`
- [ ] ⏱ 15m — `internal/product/service.go` — `UpdateProduct()` — validate + update
- [ ] ⏱ 15m — `internal/product/service.go` — `UpsertVariant()`

### Handler
- [ ] ⏱ 10m — `internal/product/handler.go` — define handler struct depending on `ProductService` interface
- [ ] ⏱ 15m — `internal/product/handler.go` — `GET  /api/v1/product` (public)
- [ ] ⏱ 15m — `internal/product/handler.go` — `PUT  /api/v1/product` (admin)
- [ ] ⏱ 15m — `internal/product/handler.go` — `POST /api/v1/product/variants` (admin)
- [ ] ⏱ 10m — `internal/product/handler.go` — `PUT  /api/v1/product/variants/:id` (admin)

### Routes
- [ ] ⏱ 10m — Register product routes

**Phase 2 total ⏱ ~3h 45m**

---

## Phase 3 — Inventory Module
> Goal: stock tracking, admin adjustment, prevent oversell

### Migrations
- [ ] ⏱ 10m — `000005_inventory.up.sql` — inventory table (id, variant_id, quantity CHECK >= 0, low_stock_at, updated_at)
- [ ] ⏱ 5m  — `000005_inventory.down.sql`

### Entities
- [ ] ⏱ 10m — `internal/inventory/entity.go` — `Inventory` struct (pure domain, no json/db tags)

### Models (HTTP DTOs)
- [ ] ⏱ 10m — `internal/inventory/model.go` — `AdjustStockRequest` (json tags only)

### Repository
- [ ] ⏱ 15m — `internal/inventory/repository.go` — define `InventoryRepository` interface
- [ ] ⏱ 15m — `internal/inventory/repository.go` — implement `GetByVariantID()`, `GetAllInventory()`, `SetQuantity()`
- [ ] ⏱ 15m — `internal/inventory/repository.go` — implement `DecrementStock(variantID string, qty int)` — called via UoW transaction; no `*sql.Tx` in the interface

### Service
- [ ] ⏱ 15m — `internal/inventory/service.go` — define `InventoryService` interface + concrete struct depending on `InventoryRepository` interface
- [ ] ⏱ 15m — `internal/inventory/service.go` — `GetInventory()`
- [ ] ⏱ 15m — `internal/inventory/service.go` — `AdjustStock()` — validate qty, call repo, log if low stock

### Handler
- [ ] ⏱ 10m — `internal/inventory/handler.go` — define handler struct depending on `InventoryService` interface
- [ ] ⏱ 15m — `internal/inventory/handler.go` — `GET /api/v1/inventory` (admin)
- [ ] ⏱ 15m — `internal/inventory/handler.go` — `PUT /api/v1/inventory/:variant_id` (admin)

### Routes
- [ ] ⏱ 10m — Register inventory routes

**Phase 3 total ⏱ ~2h 45m**

---

## Phase 4 — Order Module
> Goal: place order (transactional), status tracking, admin management

### Migrations
- [ ] ⏱ 10m — `000006_orders.up.sql` — order_status enum, orders table
- [ ] ⏱ 5m  — `000006_orders.down.sql`
- [ ] ⏱ 10m — `000007_order_items.up.sql` — order_items table (with unit_price snapshot)
- [ ] ⏱ 5m  — `000007_order_items.down.sql`

### Entities
- [ ] ⏱ 10m — `internal/order/entity.go` — `Order`, `OrderItem` structs (pure domain, no json/db tags)

### Models (HTTP DTOs)
- [ ] ⏱ 15m — `internal/order/model.go` — `PlaceOrderRequest`, `OrderStatusUpdateRequest` (json tags only)

### Unit of Work
- [ ] ⏱ 20m — `internal/order/uow.go` — define `UnitOfWork` interface (`Begin() (Transaction, error)`), `Transaction` interface (`OrderRepository()`, `StockDecrementer()`, `Commit()`, `Rollback()`), `StockDecrementer` interface (`DecrementStock(variantID string, qty int) error`)
- [ ] ⏱ 25m — `internal/database/uow.go` — postgres `UnitOfWork` implementation: `Begin()` opens `*sql.Tx`, returns a `Transaction` impl that wraps it — service layer never sees `*sql.Tx`

### Repository
- [ ] ⏱ 15m — `internal/order/repository.go` — define `OrderRepository` interface (no `*sql.Tx` parameters)
- [ ] ⏱ 15m — `internal/order/repository.go` — implement `InsertOrder()`, `InsertOrderItems()` — UoW passes tx internally
- [ ] ⏱ 15m — `internal/order/repository.go` — implement `GetOrderByID()`, `GetOrdersByUserID()`, `ListAllOrders()`, `UpdateOrderStatus()`

### Service
- [ ] ⏱ 15m — `internal/order/service.go` — define `OrderService` interface + concrete struct depending on `UnitOfWork` + `OrderRepository` interfaces only
- [ ] ⏱ 30m — `internal/order/service.go` — `PlaceOrder()` — `uow.Begin()` → validate stock via `tx.StockDecrementer()` → snapshot price → `tx.OrderRepository().InsertOrder()` + `InsertOrderItems()` → `tx.Commit()`; `defer tx.Rollback()` on any error
- [ ] ⏱ 15m — `internal/order/service.go` — `GetOrder()` — auth check (owner or admin)
- [ ] ⏱ 10m — `internal/order/service.go` — `GetMyOrders()`, `ListAllOrders()`
- [ ] ⏱ 15m — `internal/order/service.go` — `UpdateStatus()` — validate allowed transition, update

### Handler
- [ ] ⏱ 10m — `internal/order/handler.go` — define handler struct depending on `OrderService` interface
- [ ] ⏱ 15m — `internal/order/handler.go` — `POST /api/v1/orders`
- [ ] ⏱ 10m — `internal/order/handler.go` — `GET  /api/v1/orders` (admin)
- [ ] ⏱ 10m — `internal/order/handler.go` — `GET  /api/v1/orders/me` (customer)
- [ ] ⏱ 10m — `internal/order/handler.go` — `GET  /api/v1/orders/:id`
- [ ] ⏱ 10m — `internal/order/handler.go` — `PUT  /api/v1/orders/:id/status` (admin)

### Routes
- [ ] ⏱ 10m — Register order routes

**Phase 4 total ⏱ ~5h 45m**

---

## Phase 5 — Payment Module
> Goal: Stripe PaymentIntent flow, webhook handling, refund

### Migrations
- [ ] ⏱ 10m — `000008_payments.up.sql` — payment_status enum, payments table
- [ ] ⏱ 5m  — `000008_payments.down.sql`

### Entities
- [ ] ⏱ 10m — `internal/payment/entity.go` — `Payment` struct (pure domain, no json/db tags)

### Models (HTTP DTOs)
- [ ] ⏱ 10m — `internal/payment/model.go` — `InitiateResponse` (json tags only)

### Payment Gateway Interface
- [ ] ⏱ 20m — `internal/payment/gateway.go` — define `PaymentGateway` interface: `CreatePaymentIntent()`, `VerifyWebhookSignature()`, `CreateRefund()`

### Stripe Client (implements PaymentGateway)
- [ ] ⏱ 25m — `pkg/stripe/client.go` — raw `net/http` Stripe client implementing `PaymentGateway` interface — no Stripe SDK

### Repository
- [ ] ⏱ 15m — `internal/payment/repository.go` — define `PaymentRepository` interface
- [ ] ⏱ 15m — `internal/payment/repository.go` — implement `InsertPayment()`, `GetPaymentByOrderID()`, `UpdatePaymentStatus()`, `SaveWebhookPayload()`

### Service
- [ ] ⏱ 15m — `internal/payment/service.go` — define `OrderStatusUpdater` interface: `UpdateOrderStatus(orderID string, status string) error` — satisfied by order repository, wired in `serve.go`
- [ ] ⏱ 15m — `internal/payment/service.go` — define `PaymentService` interface + concrete struct depending on `PaymentGateway`, `PaymentRepository`, and `OrderStatusUpdater` interfaces
- [ ] ⏱ 25m — `internal/payment/service.go` — `InitiatePayment()` — create PaymentIntent via gateway, insert payments row, return client_secret
- [ ] ⏱ 30m — `internal/payment/service.go` — `HandleWebhook()` — verify signature via gateway, parse event, update payment status via `PaymentRepository`, update order status via `OrderStatusUpdater`
- [ ] ⏱ 10m — `internal/payment/service.go` — `GetPaymentStatus()`
- [ ] ⏱ 20m — `internal/payment/service.go` — `RefundPayment()` — call gateway refund, update DB

### Handler
- [ ] ⏱ 10m — `internal/payment/handler.go` — define handler struct depending on `PaymentService` interface
- [ ] ⏱ 15m — `internal/payment/handler.go` — `POST /api/v1/payments/initiate/:order_id`
- [ ] ⏱ 20m — `internal/payment/handler.go` — `POST /api/v1/payments/webhook` (raw body, no JSON middleware)
- [ ] ⏱ 10m — `internal/payment/handler.go` — `GET  /api/v1/payments/:order_id`
- [ ] ⏱ 15m — `internal/payment/handler.go` — `POST /api/v1/payments/refund/:order_id` (admin)

### Routes
- [ ] ⏱ 10m — Register payment routes

**Phase 5 total ⏱ ~4h 45m**

---

## Phase 6 — Hardening & Final Wiring
> Goal: production-ready, tested, deployable

- [ ] ⏱ 20m — Input validation — manual validation in handlers (no external validator package)
- [ ] ⏱ 20m — Global error handler — map domain errors to HTTP status codes in one place
- [ ] ⏱ 15m — Request ID middleware — inject `X-Request-ID` into every request for tracing
- [ ] ⏱ 20m — Rate limiting middleware — `POST /auth/login` and `POST /auth/register`
- [ ] ⏱ 15m — Graceful shutdown — listen for SIGINT/SIGTERM, drain connections
- [ ] ⏱ 30m — End-to-end smoke test — register → login → get product → place order → initiate payment → simulate webhook
- [ ] ⏱ 20m — Review all SQL queries for injection safety (parameterised only)
- [ ] ⏱ 15m — Confirm `.env` is in `.gitignore`, no secrets in code
- [ ] ⏱ 20m — Final Docker Compose test — bring up full stack, run smoke test

**Phase 6 total ⏱ ~2h 45m**

---

## Time Summary

| Phase | Description         | Estimate   |
|-------|---------------------|------------|
| 0     | Project Setup       | ~4h 45m    |
| 1     | Auth                | ~5h 30m    |
| 2     | Product             | ~3h 45m    |
| 3     | Inventory           | ~2h 45m    |
| 4     | Order               | ~5h 45m    |
| 5     | Payment             | ~4h 45m    |
| 6     | Hardening & Testing | ~2h 45m    |
| **Total** |                 | **~30h 15m** |

> Estimates assume no major blockers. Stripe webhook testing adds variability — budget an extra 30–60m for that.
