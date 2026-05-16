# Phase 0 — Implementation Plan

**Goal:** A running HTTP server that connects to PostgreSQL, loads config from env, logs every request, handles panics, and returns a health-check response. No business logic — pure skeleton.

**Estimated time:** ~4h 45m  
**Execution order matters.** Steps are numbered sequentially. Do not skip ahead.

---

## Step 1 — Install dependencies ⏱ 10m

Run in the project root:

```bash
go get github.com/spf13/cobra@latest
go get github.com/lib/pq@latest
go get golang.org/x/crypto/bcrypt@latest
```

**Verify:**
- `go.mod` has exactly these three external dependencies and nothing else
- `go.sum` is updated
- `go run .` still compiles (main.go is currently a stub)

---

## Step 2 — Create folder structure ⏱ 10m

Run:

```bash
mkdir -p internal/database
mkdir -p internal/auth internal/product internal/inventory internal/order internal/payment
mkdir -p pkg/apperr
mkdir -p pkg/cmd/migration
mkdir -p pkg/config
mkdir -p pkg/router
mkdir -p pkg/middleware
mkdir -p pkg/jwt
mkdir -p pkg/response
mkdir -p pkg/stripe
mkdir -p migrations/sql
```

**Verify:**

```bash
find . -type d | grep -v ".git" | sort
```

Every folder from `ARCHITECTURE.md` must exist before writing any `.go` file.

---

## Step 3 — Environment files ⏱ 10m

**Create `.env.example`** at project root:

```
PORT=8080
DB_URL=postgres://user:password@localhost:5432/dbname?sslmode=disable
JWT_SECRET=change-me-to-a-long-random-string
STRIPE_SECRET_KEY=sk_test_xxx
STRIPE_WEBHOOK_SECRET=whsec_xxx
```

**Create `.env`** at project root (your real local values — never committed):

```
PORT=8080
DB_URL=postgres://postgres:postgres@localhost:5432/gogo_db?sslmode=disable
JWT_SECRET=dev-secret-key-replace-in-production
STRIPE_SECRET_KEY=sk_test_xxx
STRIPE_WEBHOOK_SECRET=whsec_xxx
```

**Update `.gitignore`** — confirm these lines exist:

```
.env
*.exe
*.test
/tmp
```

**Rule:** Run `git status` — `.env` must show as ignored, not tracked.

---

## Step 4 — `pkg/apperr/errors.go` ⏱ 15m

**Package:** `apperr`
**Purpose:** Typed domain errors. This is the innermost shared layer — zero imports except stdlib.

**What to implement:**

Define an `AppError` struct with three fields:
- `Code string` — machine-readable identifier (e.g. `"NOT_FOUND"`)
- `Message string` — human-readable description
- `HTTPStatus int` — the HTTP status code this error maps to

Implement the `error` interface on `AppError` — `Error() string` returns the message.

Define these sentinel package-level variables:

| Variable | Code | HTTPStatus |
|---|---|---|
| `ErrNotFound` | `NOT_FOUND` | 404 |
| `ErrUnauthorized` | `UNAUTHORIZED` | 401 |
| `ErrForbidden` | `FORBIDDEN` | 403 |
| `ErrInvalidInput` | `INVALID_INPUT` | 400 |
| `ErrInsufficientStock` | `INSUFFICIENT_STOCK` | 400 |
| `ErrConflict` | `CONFLICT` | 409 |

Also provide a `New(base *AppError, message string) *AppError` helper that clones a sentinel with a custom message. Services use this to attach context:

```go
return apperr.New(apperr.ErrNotFound, "user not found")
```

**Key rule:** No imports except `fmt` if needed. This file must be importable by any layer.

---

## Step 5 — `pkg/config/config.go` ⏱ 15m

**Package:** `config`  
**Purpose:** Load and validate all environment variables at startup. Fail fast if anything critical is missing.

**What to implement:**

Define a `Config` struct:

```
Port               string
DatabaseURL        string
JWTSecret          string
StripeSecretKey    string
StripeWebhookSecret string
```

Write a `Load() (*Config, error)` function:
1. Read each field using `os.Getenv()`
2. If `Port` is empty → default to `"8080"` (only acceptable default)
3. If any other field is empty → return an error with the specific field name: `"DB_URL is required"`
4. Return the populated `*Config`

**Key rule:** Only `Port` may have a default. Every other missing value must be a hard error — do not silently continue with empty secrets.

---

## Step 6 — `pkg/response/response.go` ⏱ 20m

**Package:** `response`  
**Purpose:** Every HTTP response in the entire project goes through this package. Handlers never call `json.Marshal` or `w.Write` directly.

**What to implement:**

Define the response envelope structs:

```go
type envelope struct {
    Success bool        `json:"success"`
    Data    interface{} `json:"data"`
    Error   *errorBody  `json:"error"`
}

type errorBody struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}
```

Implement two exported functions:

**`JSON(w http.ResponseWriter, status int, data interface{})`**
- Sets `Content-Type: application/json`
- Writes the status code
- Encodes: `{ "success": true, "data": <data>, "error": null }`

**`Error(w http.ResponseWriter, err error)`**
- Type-asserts `err` to `*apperr.AppError`
- If assertion succeeds → use `AppError.HTTPStatus` as the status code and `AppError.Code` + `AppError.Message` for the body
- If assertion fails (unexpected error) → log it, respond with 500 and code `INTERNAL_ERROR`, message `"something went wrong"` — never leak the raw error message to the client
- Encodes: `{ "success": false, "data": null, "error": { "code": "...", "message": "..." } }`

**Key rule:** The 500 branch must log the original error using `log/slog` before responding. The client receives a generic message only.

---

## Step 7 — `pkg/middleware/` — three files ⏱ 45m

**Package:** `middleware`  
All middleware follows the standard Go pattern — takes `http.Handler`, returns `http.Handler`.

```go
func Name(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // before
        next.ServeHTTP(w, r)
        // after
    })
}
```

### `pkg/middleware/recover.go` ⏱ 15m

Wraps the next handler in `defer` + `recover()`. On panic:
1. Log the panic value and stack trace using `log/slog` at error level
2. Call `response.Error(w, apperr.New(apperr.ErrInternalError, "something went wrong"))`

> You need to add `ErrInternalError` to `pkg/apperr/errors.go` — Code: `INTERNAL_ERROR`, HTTPStatus: 500.

**Must be the outermost middleware** — it wraps everything including the logger.

### `pkg/middleware/logger.go` ⏱ 15m

Logs every request. To capture the status code you need a response writer wrapper:

```go
type responseWriter struct {
    http.ResponseWriter
    status int
}

func (rw *responseWriter) WriteHeader(code int) {
    rw.status = code
    rw.ResponseWriter.WriteHeader(code)
}
```

Log fields (use `log/slog`):
- `method` — `r.Method`
- `path` — `r.URL.Path`
- `status` — captured status code
- `duration` — time elapsed (`time.Since(start)`)

Default status to 200 if `WriteHeader` was never explicitly called.

### `pkg/middleware/cors.go` ⏱ 15m

Set these headers on every response:

```
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization
```

If the request method is `OPTIONS` → write status 200 and return immediately. Do not call `next.ServeHTTP`.

---

## Step 8 — `internal/database/postgres.go` ⏱ 20m

**Package:** `database`  
**Purpose:** Open and return a validated `*sql.DB` connection pool. This is the only file that imports `lib/pq`.

**What to implement:**

Write `Open(databaseURL string) (*sql.DB, error)`:

1. Blank-import `lib/pq` for its driver side-effect: `_ "github.com/lib/pq"`
2. Call `sql.Open("postgres", databaseURL)` — this does NOT connect yet
3. Set pool limits:
   - `db.SetMaxOpenConns(25)`
   - `db.SetMaxIdleConns(25)`
   - `db.SetConnMaxLifetime(5 * time.Minute)`
4. Call `db.Ping()` — this opens the first real connection. Return error if it fails.
5. Return `*sql.DB`

**Key rules:**
- Never store `*sql.DB` in a global variable — return it and let the caller own it
- The blank import of `lib/pq` must only appear in this one file in the entire project
- Do not call `db.Close()` here — the caller (`serve.go`) owns the lifecycle and defers Close

---

## Step 9 — `pkg/router/router.go` ⏱ 20m

**Package:** `router`  
**Purpose:** Create the HTTP handler with all middleware applied and all routes registered. Returns a single `http.Handler` that `serve.go` plugs into `http.Server`.

**What to implement:**

Write `New() http.Handler`:

1. Create `mux := http.NewServeMux()`
2. Register one route for now:
   ```
   GET /health → response.JSON(w, 200, map[string]string{"status": "ok"})
   ```
3. Wrap `mux` with middleware in this exact order (outermost first):
   ```
   Recover → Logger → CORS → mux
   ```
   Wrapping order: the last `Use()` call is the outermost wrapper.
   ```go
   var handler http.Handler = mux
   handler = middleware.CORS(handler)
   handler = middleware.Logger(handler)
   handler = middleware.Recover(handler)
   ```
4. Return `handler`

**Key rule:** Every future module (auth, product, etc.) will register its routes here. `router.go` is the only file that imports all handlers. Right now it only has `/health`.

---

## Step 10 — `migrations/runner.go` ⏱ 20m

**Package:** `migrations`  
**Purpose:** Read and execute `.sql` files in order, tracking applied versions in the DB.

**What to implement:**

Write `Run(db *sql.DB, direction string) error`:

1. Create the tracking table if it does not exist:
   ```sql
   CREATE TABLE IF NOT EXISTS schema_migrations (
       version    TEXT PRIMARY KEY,
       applied_at TIMESTAMPTZ DEFAULT NOW()
   );
   ```

2. Read all files from `migrations/sql/` matching `*.up.sql` (for `"up"`) or `*.down.sql` (for `"down"`)

3. Sort files alphabetically — this guarantees sequential execution (`000001_` before `000002_`)

4. For direction `"up"`:
   - For each file, check if its version (`000001_users`) is already in `schema_migrations` — skip if it is
   - Execute the SQL
   - Insert the version into `schema_migrations`

5. For direction `"down"`:
   - Reverse the file list
   - For each file, check if its version is in `schema_migrations` — skip if it is NOT
   - Execute the SQL
   - Delete the version from `schema_migrations`

6. Return any error immediately — do not continue past a failed migration

**Key rules:**
- Extract the version from the filename by stripping the extension: `000001_users.up.sql` → `000001_users`
- Each migration file must execute in a single `db.Exec()` call
- Log each migration being applied using `log/slog`

---

## Step 11 — `pkg/cmd/migration/` ⏱ 35m

Three files. These are the cobra commands that call `migrations.Run()`.

### `pkg/cmd/migration/migration.go` ⏱ 15m

**Package:** `migration`

```go
var MigrateCmd = &cobra.Command{
    Use:   "migration",
    Short: "Manage database migrations",
}

func init() {
    MigrateCmd.AddCommand(upCmd)
    MigrateCmd.AddCommand(downCmd)
}
```

`MigrateCmd` is the parent — it does nothing by itself.

### `pkg/cmd/migration/up.go` ⏱ 10m

```go
var upCmd = &cobra.Command{
    Use:   "up",
    Short: "Run all pending migrations",
    RunE: func(cmd *cobra.Command, args []string) error {
        // 1. config.Load()
        // 2. database.Open(cfg.DatabaseURL)
        // 3. migrations.Run(db, "up")
        // 4. log success
        return nil
    },
}
```

Use `RunE` (not `Run`) — it returns an error and cobra handles it cleanly.

### `pkg/cmd/migration/down.go` ⏱ 10m

Same structure as `up.go` but calls `migrations.Run(db, "down")`.

**Key rule for both:** If any step fails, return the error — let cobra print it and exit with code 1. Do not `log.Fatal` inside cobra command functions.

---

## Step 12 — `pkg/cmd/serve.go` ⏱ 20m

**Package:** `cmd`  
**Purpose:** Wire all dependencies and start the HTTP server with graceful shutdown.

**What to implement:**

```go
var serveCmd = &cobra.Command{
    Use:   "serve",
    Short: "Start the HTTP server",
    RunE:  runServe,
}

func runServe(cmd *cobra.Command, args []string) error {
    // wire here
}
```

Wire in this exact order inside `runServe`:

1. `cfg, err := config.Load()` — fail if error
2. `db, err := database.Open(cfg.DatabaseURL)` — fail if error
3. `defer db.Close()`
4. `handler := router.New()` — returns the full middleware-wrapped mux
5. Create `http.Server`:
   ```go
   srv := &http.Server{
       Addr:         ":" + cfg.Port,
       Handler:      handler,
       ReadTimeout:  10 * time.Second,
       WriteTimeout: 10 * time.Second,
       IdleTimeout:  60 * time.Second,
   }
   ```
6. **Graceful shutdown** — run the server in a goroutine, listen for `os.Signal` on a channel:
   ```
   signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
   <-quit  // block until signal received
   ```
   On signal received → create a 10-second context → call `srv.Shutdown(ctx)`
7. Log server start: `"server starting on :<port>"`
8. Log shutdown: `"server shutting down"`

**Key rule:** `ListenAndServe` returns `http.ErrServerClosed` on graceful shutdown — that is not an error. Only return an error if it returns something else.

---

## Step 13 — `pkg/cmd/root.go` ⏱ 20m

**Package:** `cmd`  
**Purpose:** The cobra root command and the public `Execute()` function that `main.go` calls.

**What to implement:**

```go
var rootCmd = &cobra.Command{
    Use:   "gogo-backend",
    Short: "Single product e-commerce backend",
}

func Execute() {
    if err := rootCmd.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

func init() {
    rootCmd.AddCommand(serveCmd)
    rootCmd.AddCommand(migration.MigrateCmd)
}
```

**Key rule:** `init()` is the only place subcommands are registered. No subcommand registers itself in root — they are always pulled in here.

---

## Step 14 — `main.go` ⏱ 15m

Replace the current placeholder `main.go` entirely:

```go
package main

import "github.com/md-shajib/gogo-backend/pkg/cmd"

func main() {
    cmd.Execute()
}
```

That is the complete file. If `main.go` has more than these 7 lines, something is wrong.

---

## Step 15 — `docker-compose.yml` ⏱ 15m

Two services — `postgres` and `app`.

**postgres service:**
- Image: `postgres:16-alpine`
- Environment: `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`
- Port: `5432:5432`
- Named volume for data persistence
- Healthcheck: `pg_isready -U $$POSTGRES_USER`

**app service:**
- Build context: `.` (uses Dockerfile)
- Depends on `postgres` with condition `service_healthy`
- `env_file: .env`
- Port: `8080:8080`
- Command: `./app serve`

**Key rule:** The `app` service must wait for postgres to be healthy — not just started. Use `depends_on` with `condition: service_healthy`, not just `depends_on: postgres`.

---

## Step 16 — `Dockerfile` ⏱ 15m

Two-stage build.

**Stage 1 — builder:**
- Base: `golang:1.21-alpine`
- Set `WORKDIR /build`
- Copy `go.mod` + `go.sum` first → `RUN go mod download` (this layer is cached if deps don't change)
- Copy all source → `RUN go build -o app ./...` or `RUN CGO_ENABLED=0 go build -o app .`

**Stage 2 — final:**
- Base: `alpine:latest`
- `RUN apk --no-cache add ca-certificates` (needed for HTTPS calls to Stripe)
- Copy binary from builder: `COPY --from=builder /build/app /app`
- `EXPOSE 8080`
- `CMD ["/app", "serve"]`

**Key rule:** The final image must contain only the binary + `ca-certificates`. No Go toolchain, no source code. Run `docker image ls` after build — the final image should be under 20MB.

---

## Completion Checklist

Verify every point before marking Phase 0 done and moving to Phase 1:

```
[ ] go.mod has exactly 3 external dependencies (cobra, lib/pq, bcrypt)
[ ] go run . → prints help with "serve" and "migration" subcommands
[ ] go run . serve → server starts, logs "server starting on :8080"
[ ] curl localhost:8080/health → {"success":true,"data":{"status":"ok"},"error":null}
[ ] Every request is logged: method, path, status, duration
[ ] Panic in a handler is recovered — server stays alive, returns 500
[ ] CORS headers present on every response
[ ] go run . migration up → "Running migrations up..." (no sql files yet, just no crash)
[ ] go run . migration down → "Running migrations down..."
[ ] go run . unknowncmd → cobra prints error + usage
[ ] .env is NOT tracked by git (git status shows nothing for .env)
[ ] docker-compose up → postgres + app both healthy
[ ] docker-compose up → curl localhost:8080/health returns correct response
[ ] Docker final image is under 20MB
```

**Only move to Phase 1 when all 14 boxes are checked.**

---

## Dependency Graph (build order reference)

```
go.mod deps
    └── pkg/apperr/errors.go         (no deps)
    └── pkg/config/config.go         (no deps)
            └── pkg/response/response.go    (apperr)
                    └── pkg/middleware/recover.go   (response, apperr)
                    └── pkg/middleware/logger.go    (no response dep)
                    └── pkg/middleware/cors.go      (no deps)
                            └── internal/database/postgres.go  (no deps)
                            └── pkg/router/router.go           (middleware, response)
                            └── migrations/runner.go           (database/sql)
                                    └── pkg/cmd/migration/     (config, database, migrations)
                                    └── pkg/cmd/serve.go       (config, database, router)
                                            └── pkg/cmd/root.go    (serve, migration)
                                                    └── main.go    (cmd)
```

Build and test each node before moving to the next. If a node fails to compile, fix it before continuing.
