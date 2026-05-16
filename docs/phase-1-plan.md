# Phase 1 — Auth Module Implementation Plan

**Goal:** Register, login, JWT-based authentication, token refresh, logout. Establish the auth pattern (entity → repository interface → service interface → handler) that every subsequent phase follows.

**Estimated time:** ~5h 30m  
**Prerequisites:** Phase 0 complete. Server starts, `/health` returns 200.

---

## Execution Order

Steps must be done in sequence — each step depends on the previous.

---

## Step 1 — Migration: users table ⏱ 10m

**File:** `migrations/sql/000001_users.up.sql`

```sql
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE users (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT        UNIQUE NOT NULL,
    password   TEXT        NOT NULL,
    role       TEXT        NOT NULL DEFAULT 'customer',
    created_at TIMESTAMPTZ DEFAULT NOW()
);
```

**File:** `migrations/sql/000001_users.down.sql`

```sql
DROP TABLE IF EXISTS users;
```

**Rules:**
- `pgcrypto` extension is required for `gen_random_uuid()` — always create it first
- `role` values are `'customer'` and `'admin'` only — enforced in service layer, not DB constraint (keep it simple)
- `password` stores bcrypt hash only — never plain text

---

## Step 2 — Migration: refresh_tokens table ⏱ 10m

**File:** `migrations/sql/000002_refresh_tokens.up.sql`

```sql
CREATE TABLE refresh_tokens (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token      TEXT        UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
```

**File:** `migrations/sql/000002_refresh_tokens.down.sql`

```sql
DROP TABLE IF EXISTS refresh_tokens;
```

**Rules:**
- `ON DELETE CASCADE` — when a user is deleted, all their refresh tokens are deleted automatically
- `token` is unique — one row per issued refresh token
- `expires_at` is checked in the service layer — not a DB-level expiry

---

## Step 3 — `internal/auth/entity.go` ⏱ 10m

**Package:** `auth`  
**Purpose:** Pure domain struct. Zero imports. No json or db tags.

Define one struct:

```
User
  ID        string
  Email     string
  Password  string    ← bcrypt hash
  Role      string    ← "customer" | "admin"
  CreatedAt time.Time
```

**Rules:**
- No `json:` tags — this is not a DTO
- No `db:` tags — repository handles mapping
- Import only `"time"` — nothing else

---

## Step 4 — `internal/auth/model.go` ⏱ 15m

**Package:** `auth`  
**Purpose:** HTTP request/response DTOs. These are the shapes the API accepts and returns.

Define these structs with `json:` tags:

```
RegisterRequest
  Email    string  `json:"email"`
  Password string  `json:"password"`

LoginRequest
  Email    string  `json:"email"`
  Password string  `json:"password"`

TokenResponse
  AccessToken  string `json:"access_token"`
  RefreshToken string `json:"refresh_token"`

RefreshRequest
  RefreshToken string `json:"refresh_token"`

LogoutRequest
  RefreshToken string `json:"refresh_token"`
```

**Rules:**
- No domain logic here — plain structs only
- No `User` fields in DTOs — never expose password hash to the client

---

## Step 5 — `internal/auth/token.go` ⏱ 15m

**Package:** `auth`  
**Purpose:** Define the `TokenService` interface and `Claims` struct. The service and middleware depend on this interface — never on `pkg/jwt` directly.

```
Claims
  UserID string
  Role   string

TokenService interface
  GenerateAccessToken(userID, role string) (string, error)
  GenerateRefreshToken() (string, error)
  ParseToken(token string) (*Claims, error)
```

**Rules:**
- `Claims` must be defined here (inner layer) — `pkg/jwt` uses this type, not the other way around
- No imports from `pkg/jwt` — this is the interface, not the implementation
- Import only `"time"` if needed for expiry — nothing external

---

## Step 6 — `internal/auth/repository.go` ⏱ 40m

**Package:** `auth`  
**Purpose:** Define repository interfaces the service needs, then implement them with postgres.

### Part A — Define interfaces ⏱ 10m

```
UserRepository interface
  InsertUser(user *User) error
  FindUserByEmail(email string) (*User, error)
  FindUserByID(id string) (*User, error)

TokenRepository interface
  InsertRefreshToken(userID, token string, expiresAt time.Time) error
  FindRefreshToken(token string) (userID string, expiresAt time.Time, err error)
  DeleteRefreshToken(token string) error
```

### Part B — Implement UserRepository ⏱ 15m

Concrete struct: `userRepository` with field `db *sql.DB`

- `InsertUser` — `INSERT INTO users (email, password, role) VALUES ($1, $2, $3) RETURNING id, created_at`
- `FindUserByEmail` — `SELECT id, email, password, role, created_at FROM users WHERE email = $1`
- `FindUserByID` — `SELECT id, email, password, role, created_at FROM users WHERE id = $1`

Return `apperr.ErrNotFound` when `sql.ErrNoRows` is returned.

### Part C — Implement TokenRepository ⏱ 15m

Concrete struct: `tokenRepository` with field `db *sql.DB`

- `InsertRefreshToken` — `INSERT INTO refresh_tokens (user_id, token, expires_at) VALUES ($1, $2, $3)`
- `FindRefreshToken` — `SELECT user_id, expires_at FROM refresh_tokens WHERE token = $1`
- `DeleteRefreshToken` — `DELETE FROM refresh_tokens WHERE token = $1`

### Constructor functions ⏱ 5m

```go
func NewUserRepository(db *sql.DB) UserRepository
func NewTokenRepository(db *sql.DB) TokenRepository
```

Return the interface — not the concrete type.

**Rules:**
- All queries use `$1`, `$2` placeholders — never string concatenation
- Import `database/sql` and `apperr` only — no business logic
- `sql.ErrNoRows` → return `apperr.ErrNotFound` — never expose raw DB errors

---

## Step 7 — `pkg/jwt/jwt.go` ⏱ 20m

**Package:** `jwt`  
**Purpose:** Implement `auth.TokenService` interface using manual HMAC-SHA256. No external JWT library.

**Concrete struct:**

```
jwtService
  secret     []byte
  accessTTL  time.Duration   ← 15 minutes
  refreshTTL time.Duration   ← 7 days
```

**Constructor:**

```go
func New(secret string) auth.TokenService
```

**Token format** — manual JWT: `base64(header).base64(payload).base64(signature)`

- Header: `{"alg":"HS256","typ":"JWT"}`
- Payload: `{"user_id":"...","role":"...","exp":unix_timestamp}`
- Signature: `HMAC-SHA256(header.payload, secret)`

**Implement:**

- `GenerateAccessToken(userID, role string)` — builds and signs a 15-min token
- `GenerateRefreshToken()` — generates a random 32-byte hex string (use `crypto/rand`) — not a JWT, just a secure random token
- `ParseToken(token string)` — splits on `.`, verifies signature, checks `exp`, returns `*auth.Claims`

**Rules:**
- Use `crypto/rand` for refresh token — not `math/rand`
- Use `crypto/hmac` + `crypto/sha256` + `encoding/base64` — all stdlib
- `ParseToken` must return `apperr.ErrUnauthorized` on invalid or expired token
- This package imports `internal/auth` (for `auth.TokenService` and `auth.Claims`) — that is correct, outer layer importing inner layer

---

## Step 8 — `internal/auth/service.go` ⏱ 1h

**Package:** `auth`  
**Purpose:** Business logic. Define `AuthService` interface then implement it. Depends on repository interfaces and `TokenService` interface only.

### Part A — Define AuthService interface ⏱ 10m

```
AuthService interface
  Register(req *RegisterRequest) (*User, error)
  Login(req *LoginRequest) (*TokenResponse, error)
  RefreshToken(refreshToken string) (*TokenResponse, error)
  Logout(refreshToken string) error
```

### Part B — Concrete struct ⏱ 5m

```
authService
  userRepo  UserRepository
  tokenRepo TokenRepository
  tokenSvc  TokenService
```

Constructor:
```go
func NewAuthService(userRepo UserRepository, tokenRepo TokenRepository, tokenSvc TokenService) AuthService
```

### Part C — Implement Register ⏱ 10m

1. Validate: email not empty, password minimum 8 characters — return `apperr.ErrInvalidInput` if fails
2. Check if email already exists via `userRepo.FindUserByEmail` — if found return `apperr.ErrConflict`
3. Hash password with `bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)`
4. Call `userRepo.InsertUser`
5. Return the created `*User`

### Part D — Implement Login ⏱ 10m

1. Find user by email — if not found return `apperr.ErrUnauthorized` (do not reveal whether email exists)
2. Compare password: `bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password))` — if fails return `apperr.ErrUnauthorized`
3. Generate access token via `tokenSvc.GenerateAccessToken(user.ID, user.Role)`
4. Generate refresh token via `tokenSvc.GenerateRefreshToken()`
5. Store refresh token: `tokenRepo.InsertRefreshToken(user.ID, refreshToken, time.Now().Add(7*24*time.Hour))`
6. Return `*TokenResponse`

### Part E — Implement RefreshToken ⏱ 10m

1. Find refresh token in DB — if not found return `apperr.ErrUnauthorized`
2. Check `expiresAt` — if past return `apperr.ErrUnauthorized` and delete the token
3. Generate new access token via `tokenSvc.GenerateAccessToken(userID, role)` — need to fetch user for role
4. Return `*TokenResponse` with new access token and same refresh token

### Part F — Implement Logout ⏱ 5m

1. Delete refresh token from DB via `tokenRepo.DeleteRefreshToken`
2. Return nil — even if token does not exist, return nil (idempotent)

**Rules:**
- Never import `pkg/jwt` — only use `TokenService` interface
- Never import `database/sql` — only use repository interfaces
- Login failure must return `ErrUnauthorized` for both wrong email and wrong password — never reveal which one failed
- Import `golang.org/x/crypto/bcrypt` here — this is the only service that hashes passwords

---

## Step 9 — `internal/auth/handler.go` ⏱ 50m

**Package:** `auth`  
**Purpose:** Parse HTTP requests, call service, write responses. Zero business logic.

### Handler struct

```
Handler
  svc AuthService
```

Constructor:
```go
func NewHandler(svc AuthService) *Handler
```

### POST /api/v1/auth/register ⏱ 10m

1. Decode JSON body into `RegisterRequest`
2. Call `svc.Register(&req)`
3. On error → `response.Error(w, err)`
4. On success → `response.JSON(w, http.StatusCreated, user)`

### POST /api/v1/auth/login ⏱ 10m

1. Decode JSON body into `LoginRequest`
2. Call `svc.Login(&req)`
3. On error → `response.Error(w, err)`
4. On success → `response.JSON(w, http.StatusOK, tokenResponse)`

### POST /api/v1/auth/refresh ⏱ 10m

1. Decode JSON body into `RefreshRequest`
2. Call `svc.RefreshToken(req.RefreshToken)`
3. On error → `response.Error(w, err)`
4. On success → `response.JSON(w, http.StatusOK, tokenResponse)`

### POST /api/v1/auth/logout ⏱ 10m

1. Decode JSON body into `LogoutRequest`
2. Call `svc.Logout(req.RefreshToken)`
3. On error → `response.Error(w, err)`
4. On success → `response.JSON(w, http.StatusOK, map[string]string{"message": "logged out"})`

### JSON decode helper ⏱ 10m

Write a private helper used by all handlers:
```go
func decodeJSON(r *http.Request, dst interface{}) error
```
Returns `apperr.ErrInvalidInput` if body is empty or malformed.

**Rules:**
- No business logic — only parse, call service, respond
- Always check decode error before calling service
- Never access `r.Body` directly after calling the decode helper

---

## Step 10 — `internal/auth/middleware.go` ⏱ 30m

**Package:** `auth`  
**Purpose:** JWT validation middleware and admin role guard. Both depend on `TokenService` interface only.

### Part A — JWT Middleware ⏱ 20m

```go
func JWTMiddleware(tokenSvc TokenService) func(http.Handler) http.Handler
```

Steps inside:
1. Read `Authorization` header — if missing return `apperr.ErrUnauthorized`
2. Strip `Bearer ` prefix — if format wrong return `apperr.ErrUnauthorized`
3. Call `tokenSvc.ParseToken(token)` — if error return `apperr.ErrUnauthorized`
4. Store `claims.UserID` and `claims.Role` in request context using unexported context keys
5. Call `next.ServeHTTP(w, r.WithContext(ctx))`

Define two unexported context key types to avoid collisions:
```go
type contextKey string
const (
    contextKeyUserID contextKey = "user_id"
    contextKeyRole   contextKey = "role"
)
```

Provide two exported helper functions for handlers to read context values:
```go
func UserIDFromContext(ctx context.Context) string
func RoleFromContext(ctx context.Context) string
```

### Part B — AdminOnly Middleware ⏱ 10m

```go
func AdminOnly(next http.Handler) http.Handler
```

1. Read role from context via `RoleFromContext`
2. If role is not `"admin"` → return `apperr.ErrForbidden`
3. Otherwise call `next.ServeHTTP(w, r)`

**Rule:** `AdminOnly` must always be used after `JWTMiddleware` in the chain — it assumes context values are already set.

---

## Step 11 — Register routes in `pkg/router/router.go` ⏱ 15m

Update `router.New()` to accept auth dependencies:

```go
func New(authHandler *auth.Handler, authMiddleware func(http.Handler) http.Handler) http.Handler
```

Register auth routes:

```
POST /api/v1/auth/register  → authHandler.Register   (public)
POST /api/v1/auth/login     → authHandler.Login       (public)
POST /api/v1/auth/refresh   → authHandler.Refresh     (public)
POST /api/v1/auth/logout    → authHandler.Logout      (JWT required)
```

Apply `JWTMiddleware` only to `/logout` route.

**Rule:** Use Go 1.22 method-based routing: `"POST /api/v1/auth/register"`.

---

## Step 12 — Wire auth in `pkg/cmd/serve.go` ⏱ 20m

Update `runServe` to construct and inject auth dependencies:

```
1. config.Load()
2. database.Open()
3. jwtSvc  := jwt.New(cfg.JWTSecret)
4. userRepo := auth.NewUserRepository(db)
5. tokenRepo := auth.NewTokenRepository(db)
6. authSvc  := auth.NewAuthService(userRepo, tokenRepo, jwtSvc)
7. authHandler := auth.NewHandler(authSvc)
8. jwtMiddleware := auth.JWTMiddleware(jwtSvc)
9. router.New(authHandler, jwtMiddleware)
```

**Rule:** All wiring happens in `serve.go` only — no `New()` calls inside handlers or services.

---

## Step 13 — Run migrations and test ⏱ 15m

```bash
# apply migrations
go run . migration up

# verify schema_migrations table
psql $DB_URL -c "SELECT * FROM schema_migrations;"

# start server
go run . serve
```

Test each endpoint with curl or Postman:

```bash
# register
curl -X POST localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"password123"}'

# login
curl -X POST localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"password123"}'

# refresh (use refresh_token from login response)
curl -X POST localhost:8080/api/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"..."}'

# logout
curl -X POST localhost:8080/api/v1/auth/logout \
  -H "Authorization: Bearer <access_token>" \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"..."}'
```

---

## Completion Checklist

```
[ ] go run . migration up → 000001_users and 000002_refresh_tokens applied
[ ] go build ./... → zero errors
[ ] POST /auth/register → 201 with user data
[ ] POST /auth/register with same email → 409 CONFLICT
[ ] POST /auth/register with short password → 400 INVALID_INPUT
[ ] POST /auth/login with correct credentials → 200 with access + refresh token
[ ] POST /auth/login with wrong password → 401 UNAUTHORIZED
[ ] POST /auth/refresh with valid token → 200 with new access token
[ ] POST /auth/refresh with expired token → 401 UNAUTHORIZED
[ ] POST /auth/logout with valid token → 200
[ ] GET /health with no token → 200 (public route unaffected)
[ ] Any protected route with no token → 401 UNAUTHORIZED
[ ] Any protected route with invalid token → 401 UNAUTHORIZED
```

**Only move to Phase 2 when all boxes are checked.**

---

## Dependency Graph

```
migrations/sql/000001_users
migrations/sql/000002_refresh_tokens
    └── internal/auth/entity.go          (no deps)
    └── internal/auth/model.go           (no deps)
    └── internal/auth/token.go           (no deps)
            └── internal/auth/repository.go   (entity, apperr, database/sql)
            └── pkg/jwt/jwt.go               (auth.TokenService, auth.Claims)
                    └── internal/auth/service.go  (repository interfaces, TokenService, bcrypt)
                            └── internal/auth/handler.go    (AuthService, response)
                            └── internal/auth/middleware.go (TokenService, response, apperr)
                                    └── pkg/router/router.go  (auth.Handler, middleware)
                                    └── pkg/cmd/serve.go      (all of the above)
```
