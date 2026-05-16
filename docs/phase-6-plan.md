# Phase 6 — Hardening & Final Wiring Implementation Plan

**Goal:** Make the system production-ready. Harden input validation, add missing middleware (request ID, rate limiting), verify security properties, run a full end-to-end smoke test, and confirm Docker Compose deploys cleanly.

**Estimated time:** ~2h 45m  
**Prerequisites:** Phase 5 complete. All modules implemented and passing their phase checklists.

---

## Execution Order

Most steps in this phase are independent and can be done in any order. The recommended sequence prioritises security fixes first, then observability, then testing.

---

## Step 1 — Harden input validation across all handlers ⏱ 20m

Review every handler in every module. For each endpoint, verify:

### auth handlers
- `RegisterRequest.Email` — must be non-empty, must contain `@`
- `RegisterRequest.Password` — minimum 8 characters, maximum 72 characters (bcrypt truncates at 72)
- `LoginRequest.Email` — non-empty
- `LoginRequest.Password` — non-empty
- `RefreshRequest.RefreshToken` — non-empty
- `LogoutRequest.RefreshToken` — non-empty

### product handlers
- `VariantRequest.Label` — non-empty, max 100 chars
- `VariantRequest.SKU` — non-empty, max 50 chars, no whitespace
- `VariantRequest.ExtraPrice` — `>= 0`
- `UpdateProductRequest` — at least one field must be non-nil

### inventory handlers
- `AdjustStockRequest.Quantity` — `>= 0`
- `variant_id` path param — non-empty, valid UUID format

### order handlers
- `PlaceOrderRequest.Items` — non-empty slice
- Each `OrderItemRequest.VariantID` — non-empty, valid UUID format
- Each `OrderItemRequest.Quantity` — `> 0`
- `PlaceOrderRequest.ShippingName` — non-empty, max 100 chars
- `PlaceOrderRequest.ShippingAddress` — non-empty, max 500 chars
- `PlaceOrderRequest.ShippingPhone` — non-empty, valid phone-like format (at least 7 digits)
- `page` and `limit` query params — default if missing, cap `limit` at 100

### payment handlers
- `order_id` path param — non-empty, valid UUID format
- Webhook — verify `Content-Type: application/json` and non-empty body

**Rule:** All validation returns `apperr.ErrInvalidInput` with a descriptive message using `apperr.New(apperr.ErrInvalidInput, "specific message here")`. Never return generic errors from the handler.

---

## Step 2 — Add Request ID middleware ⏱ 15m

**File:** `pkg/middleware/requestid.go`

```go
func RequestID(next http.Handler) http.Handler
```

Steps:
1. Check if `X-Request-ID` header is already present (from upstream proxy)
2. If not, generate a random UUID-like string using `crypto/rand` — 16 bytes, hex-encoded
3. Set the header on the request: `r.Header.Set("X-Request-ID", requestID)`
4. Set the header on the response: `w.Header().Set("X-Request-ID", requestID)`
5. Store in context for use by the logger: `r = r.WithContext(context.WithValue(r.Context(), requestIDKey, requestID))`
6. Call `next.ServeHTTP(w, r)`

Add a helper:
```go
func RequestIDFromContext(ctx context.Context) string
```

**Update the logger middleware** (`pkg/middleware/logger.go`) to include the request ID in every log line:
```go
slog.Info("request", "method", r.Method, "path", r.URL.Path,
    "status", ww.status, "request_id", RequestIDFromContext(r.Context()))
```

**Update middleware chain in `pkg/router/router.go`:**
```go
handler = middleware.CORS(handler)
handler = middleware.Logger(handler)
handler = middleware.RequestID(handler)   ← add before Logger so logger sees the ID
handler = middleware.Recover(handler)
```

---

## Step 3 — Add rate limiting middleware for auth endpoints ⏱ 20m

**File:** `pkg/middleware/ratelimit.go`

Use a simple in-memory token bucket per IP — no external package.

```go
func RateLimit(limit int, window time.Duration) func(http.Handler) http.Handler
```

Implementation using `sync.Mutex` and a map:

```
type ipBucket struct {
    count    int
    resetAt  time.Time
}

type rateLimiter struct {
    mu      sync.Mutex
    buckets map[string]*ipBucket
    limit   int
    window  time.Duration
}
```

Steps per request:
1. Extract client IP from `r.RemoteAddr` (strip port with `net.SplitHostPort`)
2. Check `X-Forwarded-For` header first — use if present (handles reverse proxy)
3. Lock, look up or create bucket for this IP
4. If `bucket.resetAt` is in the past, reset `count = 0` and `resetAt = time.Now().Add(window)`
5. Increment `bucket.count`
6. If `bucket.count > limit` → write 429 with `{"success":false,"error":{"code":"RATE_LIMITED","message":"too many requests"}}`
7. Unlock, call `next.ServeHTTP(w, r)`

**Apply to auth routes in `pkg/router/router.go`:**
```go
authRateLimit := middleware.RateLimit(10, time.Minute)  // 10 requests/minute per IP

mux.Handle("POST /api/v1/auth/register", authRateLimit(http.HandlerFunc(authHandler.Register)))
mux.Handle("POST /api/v1/auth/login",    authRateLimit(http.HandlerFunc(authHandler.Login)))
```

**Rules:**
- This is a basic in-memory limiter — acceptable for a single-instance server
- A distributed cache (Redis) would be needed for multi-instance deployments — out of scope
- The bucket map grows unboundedly — add a periodic cleanup goroutine or accept this for now

---

## Step 4 — SQL injection audit ⏱ 20m

Review all repository files for injection safety. Check every `db.Query`, `db.QueryRow`, `db.Exec`, `tx.Query`, `tx.QueryRow`, `tx.Exec` call.

**Checklist:**
- All user-supplied values must be `$1`, `$2`, etc. placeholders — never string concatenation
- No `fmt.Sprintf` in SQL strings — flag any found and fix immediately
- Dynamic ORDER BY or column selection — if any, use an allowlist, never interpolate user input
- JSONB column writes — use parameterised query with `[]byte` argument, not inline JSON string

Files to review:
- `internal/auth/repository.go`
- `internal/product/repository.go`
- `internal/inventory/repository.go`
- `internal/order/repository.go`
- `internal/database/uow.go`
- `internal/payment/repository.go`

---

## Step 5 — Security configuration audit ⏱ 15m

### Verify `.gitignore` covers secrets

```
.env
*.env
*.pem
*.key
```

Run `git status` — confirm `.env` is not tracked.

### Verify no hardcoded secrets in source

```bash
grep -r "SECRET\|PASSWORD\|API_KEY\|stripe_" --include="*.go" .
```

Any match should be an env var reference (`os.Getenv(...)`) or a comment, not a value.

### Verify bcrypt usage

- `bcrypt.GenerateFromPassword` must use `bcrypt.DefaultCost` (10) or higher
- `bcrypt.CompareHashAndPassword` — confirm it is used for login, not `==` comparison

### Verify JWT secret minimum length

In `pkg/config/config.go` — add validation:
```go
if len(cfg.JWTSecret) < 32 {
    return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters")
}
```

### Verify CORS settings

In `pkg/middleware/cors.go` — confirm `Access-Control-Allow-Origin` is not `*` for authenticated endpoints in production. For this project's scope, `*` is acceptable but note it should be restricted in production.

---

## Step 6 — Graceful shutdown verification ⏱ 10m

Review `pkg/cmd/serve.go` shutdown logic:

```go
quit := make(chan os.Signal, 1)
signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

select {
case err := <-serverErr:
    return err
case <-quit:
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := srv.Shutdown(ctx); err != nil {
        return err
    }
}
return nil
```

Verify:
- `signal.Notify` listens for both `os.Interrupt` (Ctrl+C) and `syscall.SIGTERM` (Docker stop)
- Shutdown timeout is 10 seconds — enough for in-flight requests to drain
- DB connection is closed after shutdown: `defer db.Close()`

---

## Step 7 — End-to-end smoke test ⏱ 30m

Run the full happy path manually. Use a running instance (`go run . serve` with Docker Compose postgres).

### Sequence

```bash
BASE="localhost:8080/api/v1"

# 1. Register customer
curl -s -X POST $BASE/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"customer@example.com","password":"password123"}' | jq .

# 2. Login as customer — save access and refresh tokens
LOGIN=$(curl -s -X POST $BASE/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"customer@example.com","password":"password123"}')
CUST_TOKEN=$(echo $LOGIN | jq -r .data.access_token)
REFRESH_TOKEN=$(echo $LOGIN | jq -r .data.refresh_token)

# 3. Get product (public)
curl -s $BASE/product | jq .

# 4. Get inventory (admin — register/login an admin first)
curl -s -H "Authorization: Bearer <admin_token>" $BASE/inventory | jq .

# 5. Place order
ORDER=$(curl -s -X POST $BASE/orders \
  -H "Authorization: Bearer $CUST_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "items":[{"variant_id":"<variant_id>","quantity":1}],
    "shipping_name":"Test User",
    "shipping_address":"123 Test Street",
    "shipping_phone":"01700000001"
  }')
ORDER_ID=$(echo $ORDER | jq -r .data.id)
echo "Order: $ORDER_ID"

# 6. Initiate payment
PAYMENT=$(curl -s -X POST $BASE/payments/initiate/$ORDER_ID \
  -H "Authorization: Bearer $CUST_TOKEN")
echo "ClientSecret: $(echo $PAYMENT | jq -r .data.client_secret)"

# 7. Simulate webhook (Stripe CLI)
# stripe trigger payment_intent.succeeded --add payment_intent:metadata.order_id=$ORDER_ID

# 8. Check payment status
curl -s -H "Authorization: Bearer $CUST_TOKEN" $BASE/payments/$ORDER_ID | jq .

# 9. Check order status (should be "paid")
curl -s -H "Authorization: Bearer $CUST_TOKEN" $BASE/orders/$ORDER_ID | jq .

# 10. Admin: update status to processing
curl -s -X PUT $BASE/orders/$ORDER_ID/status \
  -H "Authorization: Bearer <admin_token>" \
  -H "Content-Type: application/json" \
  -d '{"status":"processing"}' | jq .

# 11. Refresh token
curl -s -X POST $BASE/auth/refresh \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"$REFRESH_TOKEN\"}" | jq .

# 12. Logout
curl -s -X POST $BASE/auth/logout \
  -H "Authorization: Bearer $CUST_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"$REFRESH_TOKEN\"}" | jq .
```

Verify every response follows the standard envelope: `{"success": true, "data": {...}, "error": null}`

---

## Step 8 — Docker Compose full-stack test ⏱ 20m

```bash
# build and start everything
docker compose up --build -d

# wait for postgres healthcheck
docker compose ps   # check app is "running", not "restarting"

# run migrations via docker exec
docker compose exec app /app/app migration up

# check health
curl localhost:8080/health

# run the smoke test sequence from Step 7 against localhost:8080

# stop and clean up
docker compose down -v
```

Verify:
- `docker compose ps` shows app as healthy after startup
- Migration runs successfully inside the container
- All smoke test steps pass against the containerised stack

---

## Completion Checklist

```
[ ] All handlers validate required fields and return 400 INVALID_INPUT on bad input
[ ] Password max 72 chars validated (bcrypt truncation protection)
[ ] X-Request-ID header present on every response
[ ] Logger middleware includes request_id in every log line
[ ] POST /api/v1/auth/login: 11th request in 1 minute from same IP → 429
[ ] POST /api/v1/auth/register: rate limited same as login
[ ] grep -r "fmt.Sprintf" internal/ → zero SQL string interpolations
[ ] .env not tracked by git (git status shows .env as untracked)
[ ] No hardcoded secrets found (grep passes)
[ ] JWT_SECRET < 32 chars → server fails to start with clear error
[ ] Ctrl+C during active request: request completes before server exits
[ ] docker compose up --build → app starts and /health returns 200
[ ] docker compose exec app /app/app migration up → runs cleanly
[ ] Full smoke test sequence passes end-to-end
[ ] go build ./... → zero errors, zero warnings
```

---

## Final Architecture Verification

Before declaring the project complete, verify Clean Architecture compliance one final time:

```
[ ] entity.go files: zero json/db tags, only time import
[ ] service.go files: zero database/sql imports
[ ] handler.go files: zero database/sql imports, zero business logic
[ ] No cross-module imports (internal/order does not import internal/product, etc.)
[ ] internal/payment/service.go: no import of internal/order
[ ] pkg/jwt/jwt.go: imports internal/auth, nothing from database
[ ] internal/database/uow.go: only file containing *sql.Tx
[ ] All *apperr.AppError values mapped to HTTP status in response.Error() — nowhere else
```

**The project is complete when this checklist passes.**
