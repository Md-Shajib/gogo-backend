# Phase 5 — Payment Module Implementation Plan

**Goal:** Support two payment gateways — **Stripe** (card payments) and **bKash** (mobile banking). The payment service depends only on the `PaymentGateway` interface — it never knows whether it is talking to Stripe or bKash. Gateway selection happens at request time. Cross-module order status update is done via `OrderStatusUpdater` interface (no direct import of the order module).

**Estimated time:** ~7h 30m  
**Prerequisites:** Phase 4 complete. Orders can be placed and listed.

> **Stripe:** Requires Stripe test API keys. Use Stripe CLI for local webhook forwarding.  
> **bKash:** Requires bKash PGW sandbox credentials (app_key, app_secret, username, password). Register at [pgw.sandbox.bka.sh](https://pgw.sandbox.bka.sh) for sandbox access.

---

## Payment Flow Comparison

| Step | Stripe | bKash |
|------|--------|-------|
| 1. Initiate | Create PaymentIntent → return `client_secret` | Create payment → return `bkash_url` |
| 2. User action | Frontend uses `Stripe.js` with `client_secret` | User redirects to `bkash_url` in browser |
| 3. Confirmation | Stripe sends webhook (POST) | bKash redirects to callback URL (GET) |
| 4. Execute | Not needed — Stripe auto-confirms | Must call Execute API with `paymentID` |
| 5. DB update | Webhook handler updates payment + order | Callback handler executes + updates |
| Refund | Stripe Refunds API | bKash Refund API |

---

## Execution Order

Steps must be done in sequence — each step depends on the previous.

---

## Step 1 — Migration: payments table ⏱ 10m

**File:** `migrations/sql/000008_payments.up.sql`

```sql
CREATE TYPE payment_status AS ENUM (
    'initiated', 'success', 'failed', 'refunded'
);

CREATE TABLE payments (
    id              UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id        UUID           NOT NULL REFERENCES orders(id),
    gateway         TEXT           NOT NULL,
    gateway_ref     TEXT           UNIQUE,
    amount          NUMERIC(10,2)  NOT NULL,
    currency        TEXT           NOT NULL,
    status          payment_status NOT NULL DEFAULT 'initiated',
    webhook_payload JSONB,
    created_at      TIMESTAMPTZ    DEFAULT NOW(),
    updated_at      TIMESTAMPTZ    DEFAULT NOW()
);
```

**File:** `migrations/sql/000008_payments.down.sql`

```sql
DROP TABLE IF EXISTS payments;
DROP TYPE IF EXISTS payment_status;
```

**Rules:**
- `gateway` stores `"stripe"` or `"bkash"` — which gateway processed this payment
- `gateway_ref` stores Stripe PaymentIntent ID or bKash `paymentID`
- `webhook_payload` stores raw Stripe webhook event or bKash execute response — for audit
- `gateway_ref` is UNIQUE — one gateway transaction per payment row

---

## Step 2 — `internal/payment/entity.go` ⏱ 10m

**Package:** `payment`  
**Purpose:** Pure domain struct. Zero external imports. No json or db tags.

```
Payment
  ID             string
  OrderID        string
  Gateway        string    ← "stripe" | "bkash"
  GatewayRef     string
  Amount         float64
  Currency       string
  Status         string
  WebhookPayload string    ← raw JSON stored for audit
  CreatedAt      time.Time
  UpdatedAt      time.Time
```

---

## Step 3 — `internal/payment/model.go` ⏱ 15m

**Package:** `payment`  
**Purpose:** HTTP request/response DTOs.

```
InitiateRequest
  Gateway string `json:"gateway"`   ← "stripe" | "bkash"

InitiateResponse
  Gateway      string `json:"gateway"`
  ClientSecret string `json:"client_secret,omitempty"`  ← Stripe only
  PaymentURL   string `json:"payment_url,omitempty"`    ← bKash only
  PaymentID    string `json:"payment_id"`

PaymentStatusResponse
  ID         string    `json:"id"`
  OrderID    string    `json:"order_id"`
  Gateway    string    `json:"gateway"`
  Status     string    `json:"status"`
  Amount     float64   `json:"amount"`
  Currency   string    `json:"currency"`
  GatewayRef string    `json:"gateway_ref"`
  CreatedAt  time.Time `json:"created_at"`
```

**Rules:**
- `omitempty` on `ClientSecret` and `PaymentURL` — only one is ever populated per gateway
- `Gateway` is required in `InitiateRequest` — must be `"stripe"` or `"bkash"`, validated in service

---

## Step 4 — `internal/payment/gateway.go` ⏱ 25m

**Package:** `payment`  
**Purpose:** Define the `PaymentGateway` interface that both Stripe and bKash implement. The service depends on this interface — never on either provider directly.

```go
type CreateIntentRequest struct {
    Amount   int64     // smallest currency unit (e.g. paisa for BDT, cents for USD)
    Currency string
    OrderID  string    // stored as metadata for reconciliation
    UserID   string    // bKash payerReference
    CallbackURL string // bKash redirects here after payment; ignored by Stripe
}

type CreateIntentResponse struct {
    GatewayRef  string   // Stripe: PaymentIntent ID  | bKash: paymentID
    ClientSecret string  // Stripe: client_secret     | bKash: empty
    PaymentURL   string  // bKash: bkashURL            | Stripe: empty
}

type ExecutePaymentResponse struct {
    GatewayRef string   // bKash: trxID (final transaction ID)
    Status     string   // "success" | "failed"
}

type RefundRequest struct {
    GatewayRef string   // Stripe: PaymentIntent ID | bKash: paymentID
    TrxID      string   // bKash only — trxID from execute response
    Amount     int64
}

type PaymentGateway interface {
    GatewayName() string
    CreatePaymentIntent(req CreateIntentRequest) (*CreateIntentResponse, error)
    ExecutePayment(gatewayRef string) (*ExecutePaymentResponse, error)
    VerifyWebhookSignature(payload []byte, signature string) (eventType string, rawEvent []byte, err error)
    CreateRefund(req RefundRequest) error
}
```

**Design notes:**
- `GatewayName()` returns `"stripe"` or `"bkash"` — used when inserting the payment row
- `ExecutePayment` is a bKash-required step; Stripe implementation returns `nil, nil` (no-op)
- `VerifyWebhookSignature` is Stripe-specific; bKash confirmation goes through `ExecutePayment` via the callback handler, so the bKash implementation returns `nil, nil, nil` for this method
- `TrxID` in `RefundRequest` is needed by bKash; Stripe ignores it

---

## Step 5 — `pkg/stripe/client.go` ⏱ 25m

**Package:** `stripe`  
**Purpose:** Implement `payment.PaymentGateway` using raw `net/http` Stripe API calls. No Stripe SDK.

### Struct

```
Client
  apiKey         string
  webhookSecret  string
  httpClient     *http.Client   ← 10-second timeout
```

```go
func NewClient(apiKey, webhookSecret string) payment.PaymentGateway
func (c *Client) GatewayName() string { return "stripe" }
```

### Implement CreatePaymentIntent ⏱ 10m

POST to `https://api.stripe.com/v1/payment_intents` with form body:

```
amount   = req.Amount
currency = req.Currency
metadata[order_id] = req.OrderID
```

Headers: `Authorization: Bearer <apiKey>`, `Content-Type: application/x-www-form-urlencoded`

Parse JSON response — extract `id` (PaymentIntent ID) and `client_secret`. Return `CreateIntentResponse{GatewayRef: id, ClientSecret: clientSecret}`.

### Implement ExecutePayment ⏱ 2m

```go
func (c *Client) ExecutePayment(_ string) (*payment.ExecutePaymentResponse, error) {
    return nil, nil // no-op for Stripe — confirmation happens via webhook
}
```

### Implement VerifyWebhookSignature ⏱ 10m

Stripe sends a `Stripe-Signature` header. Verify using HMAC-SHA256:

1. Parse header to extract `t` (timestamp) and `v1` (signature)
2. Construct signed payload: `<t>.<raw_body>`
3. Compute `HMAC-SHA256(webhookSecret, signedPayload)` using `crypto/hmac` + `crypto/sha256`
4. Compare against `v1` using `hmac.Equal` — return `apperr.ErrUnauthorized` on mismatch
5. Check timestamp within 5 minutes — return `apperr.ErrUnauthorized` if stale
6. Parse `type` field from the raw event JSON — return as `eventType`

### Implement CreateRefund ⏱ 3m

POST to `https://api.stripe.com/v1/refunds` with form body:

```
payment_intent = req.GatewayRef
amount         = req.Amount
```

---

## Step 6 — `pkg/bkash/client.go` ⏱ 45m

**Package:** `bkash`  
**Purpose:** Implement `payment.PaymentGateway` using raw `net/http` bKash Tokenized Checkout API. No bKash SDK.

### bKash API base URLs

```
Sandbox:    https://tokenized.sandbox.bka.sh/v1.2.0-beta/tokenized/checkout
Production: https://tokenized.pay.bka.sh/v1.2.0-beta/tokenized/checkout
```

### Struct

```
Client
  appKey       string
  appSecret    string
  username     string
  password     string
  baseURL      string
  callbackURL  string
  httpClient   *http.Client

  // cached token — bKash tokens expire in ~1 hour
  tokenMu     sync.Mutex
  idToken     string
  tokenExp    time.Time
```

```go
func NewClient(appKey, appSecret, username, password, baseURL, callbackURL string) payment.PaymentGateway
func (c *Client) GatewayName() string { return "bkash" }
```

### Token grant helper (private) ⏱ 10m

bKash requires a Bearer token for all API calls. It expires in ~3600 seconds.

```
POST <baseURL>/token/grant
Headers:
  username: <username>
  password: <password>
  Content-Type: application/json
Body:
  {"app_key": "<appKey>", "app_secret": "<appSecret>"}
Response:
  {"id_token": "...", "expires_in": 3600, ...}
```

Implementation:
1. Lock `tokenMu`
2. If `idToken != ""` and `time.Now().Before(tokenExp)` — return cached token (unlock)
3. Make grant request
4. Store `idToken` and set `tokenExp = time.Now().Add(55 * time.Minute)` (5-minute buffer)
5. Unlock and return

### Implement CreatePaymentIntent ⏱ 15m

```
POST <baseURL>/create
Headers:
  Authorization: Bearer <id_token>
  X-App-Key: <appKey>
  Content-Type: application/json
Body:
  {
    "mode": "0011",
    "payerReference": "<req.UserID>",
    "callbackURL": "<callbackURL>",
    "amount": "<req.Amount as decimal string>",
    "currency": "BDT",
    "intent": "sale",
    "merchantInvoiceNumber": "<req.OrderID>"
  }
Response on success (statusCode "0000"):
  {"paymentID": "...", "bkashURL": "...", "statusCode": "0000", ...}
```

**Amount conversion:** bKash uses decimal string (e.g. `"150.00"`), not smallest unit. Convert `req.Amount` (int64 paisa) → `float64 / 100` → formatted string.

Return `CreateIntentResponse{GatewayRef: paymentID, PaymentURL: bkashURL}`.

Return `apperr.ErrInternal` if `statusCode != "0000"`, including the bKash `statusMessage` in the error.

### Implement ExecutePayment ⏱ 10m

Called after user completes payment on bKash and callback arrives.

```
POST <baseURL>/execute
Headers:
  Authorization: Bearer <id_token>
  X-App-Key: <appKey>
  Content-Type: application/json
Body:
  {"paymentID": "<gatewayRef>"}
Response on success:
  {"statusCode": "0000", "paymentID": "...", "trxID": "...", "amount": "...", ...}
```

Return `ExecutePaymentResponse{GatewayRef: trxID, Status: "success"}`.

Return `apperr.ErrInternal` if `statusCode != "0000"`.

### Implement VerifyWebhookSignature ⏱ 2m

bKash uses a callback GET redirect, not a signed webhook. This method is unused for bKash.

```go
func (c *Client) VerifyWebhookSignature(_, _ []byte, _ string) (string, []byte, error) {
    return "", nil, nil // no-op
}
```

Wait — the signature has `(payload []byte, signature string)`. Let me correct:
```go
func (c *Client) VerifyWebhookSignature(payload []byte, signature string) (string, []byte, error) {
    return "", nil, nil // bKash uses callback redirect, not webhook
}
```

### Implement CreateRefund ⏱ 8m

```
POST <baseURL>/payment/refund
Headers:
  Authorization: Bearer <id_token>
  X-App-Key: <appKey>
  Content-Type: application/json
Body:
  {
    "paymentID": "<req.GatewayRef>",
    "amount": "<decimal string>",
    "trxID": "<req.TrxID>",
    "sku": "order-refund",
    "reason": "customer refund"
  }
Response on success: {"statusCode": "0000", ...}
```

Return `apperr.ErrInternal` if `statusCode != "0000"`.

**Rules:**
- All bKash API calls go through `getToken()` first — never hardcode or skip token refresh
- Amount is always in BDT decimal string for bKash — convert from int64 paisa internally
- Never log `id_token`, `appSecret`, or `password`
- bKash status codes: `"0000"` = success, anything else = failure

---

## Step 7 — `internal/payment/repository.go` — Interfaces ⏱ 15m

**Package:** `payment`

```
PaymentRepository interface
  InsertPayment(payment *Payment) error
  GetPaymentByOrderID(orderID string) (*Payment, error)
  GetPaymentByGatewayRef(gatewayRef string) (*Payment, error)
  UpdatePaymentStatus(paymentID, status, gatewayRef string) error
  SaveWebhookPayload(paymentID string, payload []byte) error
```

`GetPaymentByGatewayRef` is needed for bKash: when the callback arrives with a `paymentID`, we look up the payment row using it.

---

## Step 8 — `internal/payment/repository.go` — Implementation ⏱ 20m

**Concrete struct:** `paymentRepository` with field `db *sql.DB`

### InsertPayment

```sql
INSERT INTO payments (order_id, gateway, gateway_ref, amount, currency, status)
VALUES ($1, $2, $3, $4, $5, 'initiated')
RETURNING id, created_at, updated_at
```

### GetPaymentByOrderID

```sql
SELECT id, order_id, gateway, gateway_ref, amount, currency, status,
       webhook_payload, created_at, updated_at
FROM payments WHERE order_id = $1
```

Return `apperr.ErrNotFound` on `sql.ErrNoRows`.

### GetPaymentByGatewayRef

```sql
SELECT id, order_id, gateway, gateway_ref, amount, currency, status,
       webhook_payload, created_at, updated_at
FROM payments WHERE gateway_ref = $1
```

Return `apperr.ErrNotFound` on `sql.ErrNoRows`.

### UpdatePaymentStatus

```sql
UPDATE payments SET status = $2, gateway_ref = $3, updated_at = NOW()
WHERE id = $1
```

### SaveWebhookPayload

```sql
UPDATE payments SET webhook_payload = $2, updated_at = NOW()
WHERE id = $1
```

### Constructor

```go
func NewPaymentRepository(db *sql.DB) PaymentRepository
```

---

## Step 9 — `internal/payment/service.go` ⏱ 2h 10m

**Package:** `payment`  
**Purpose:** Business logic. Supports multiple gateways via a gateway registry. Never imports `internal/order`.

### Part A — Cross-module interfaces ⏱ 10m

```go
// Satisfied by order.orderRepository.UpdateOrderStatus — wired in serve.go
type OrderStatusUpdater interface {
    UpdateOrderStatus(orderID, status string) error
}

// Satisfied by order.orderRepository — wired in serve.go
type OrderReader interface {
    GetOrderSnapshot(orderID string) (*OrderSnapshot, error)
}

type OrderSnapshot struct {
    ID          string
    UserID      string
    TotalAmount float64
    Currency    string
    Status      string
}
```

### Part B — Define PaymentService interface ⏱ 5m

```
PaymentService interface
  InitiatePayment(userID, orderID string, req *InitiateRequest) (*InitiateResponse, error)
  HandleStripeWebhook(payload []byte, signature string) error
  HandleBkashCallback(paymentID, status string) error
  GetPaymentStatus(orderID string) (*Payment, error)
  RefundPayment(orderID string) error
```

### Part C — Concrete struct ⏱ 5m

```
paymentService
  gateways    map[string]PaymentGateway   ← keyed by GatewayName()
  repo        PaymentRepository
  orderUpdate OrderStatusUpdater
  orderReader OrderReader
```

Constructor:
```go
func NewPaymentService(
    gateways    []PaymentGateway,          // both Stripe and bKash passed in
    repo        PaymentRepository,
    orderUpdate OrderStatusUpdater,
    orderReader OrderReader,
) PaymentService {
    gwMap := make(map[string]PaymentGateway, len(gateways))
    for _, gw := range gateways {
        gwMap[gw.GatewayName()] = gw
    }
    return &paymentService{gateways: gwMap, ...}
}
```

### Part D — Implement InitiatePayment ⏱ 25m

1. Validate `req.Gateway` — must be `"stripe"` or `"bkash"`; look up in `s.gateways` map — return `apperr.ErrInvalidInput` if not found
2. Fetch order via `orderReader.GetOrderSnapshot(orderID)`
3. Check order belongs to `userID` — return `apperr.ErrForbidden` if not
4. Check order status is `"pending"` — return `apperr.ErrInvalidInput` if not
5. Convert `order.TotalAmount` to smallest unit (int64): `amount = int64(order.TotalAmount * 100)`
6. Build `CreateIntentRequest`:
   ```go
   CreateIntentRequest{
       Amount:      amount,
       Currency:    order.Currency,
       OrderID:     orderID,
       UserID:      userID,
       CallbackURL: "<from config or env>",  // bKash needs this
   }
   ```
7. Call `gw.CreatePaymentIntent(req)` — returns `*CreateIntentResponse`
8. Insert payment row:
   ```go
   Payment{
       OrderID:    orderID,
       Gateway:    gw.GatewayName(),
       GatewayRef: resp.GatewayRef,
       Amount:     order.TotalAmount,
       Currency:   order.Currency,
   }
   ```
9. Return `&InitiateResponse{Gateway: gw.GatewayName(), ClientSecret: resp.ClientSecret, PaymentURL: resp.PaymentURL, PaymentID: payment.ID}`

### Part E — Implement HandleStripeWebhook ⏱ 30m

1. Look up Stripe gateway: `gw := s.gateways["stripe"]`
2. Call `gw.VerifyWebhookSignature(payload, signature)` — return error if invalid
3. Based on `eventType`:
   - `"payment_intent.succeeded"`:
     - Parse `order_id` from `metadata` in raw event JSON
     - Parse `paymentIntentID` from raw event JSON (the `id` field)
     - Fetch payment via `repo.GetPaymentByGatewayRef(paymentIntentID)`
     - `repo.UpdatePaymentStatus(payment.ID, "success", paymentIntentID)`
     - `repo.SaveWebhookPayload(payment.ID, payload)`
     - `orderUpdate.UpdateOrderStatus(orderID, "paid")`
   - `"payment_intent.payment_failed"`:
     - Same lookup flow
     - `repo.UpdatePaymentStatus(payment.ID, "failed", paymentIntentID)`
     - `repo.SaveWebhookPayload(payment.ID, payload)`
   - Other event types: log with `slog.Info` and return nil (200 expected by Stripe)

### Part F — Implement HandleBkashCallback ⏱ 30m

Called when bKash redirects the user back to the callback URL.

```
paymentID = bKash paymentID (matches gateway_ref stored at initiation)
status    = "success" | "failure" | "cancel"
```

1. Look up bKash gateway: `gw := s.gateways["bkash"]`
2. Fetch payment from DB: `repo.GetPaymentByGatewayRef(paymentID)` — return `apperr.ErrNotFound` if missing
3. If `status != "success"`:
   - `repo.UpdatePaymentStatus(payment.ID, "failed", paymentID)`
   - Return nil (no need to error — the payment simply failed)
4. If `status == "success"`:
   - Call `gw.ExecutePayment(paymentID)` — this confirms payment with bKash
   - If execute fails: `repo.UpdatePaymentStatus(payment.ID, "failed", paymentID)`; return the error
   - If execute succeeds:
     - `repo.UpdatePaymentStatus(payment.ID, "success", executeResp.GatewayRef)`
     - Store execute response JSON: `repo.SaveWebhookPayload(payment.ID, rawExecuteJSON)`
     - `orderUpdate.UpdateOrderStatus(payment.OrderID, "paid")`
5. Return nil

### Part G — Implement GetPaymentStatus ⏱ 5m

Call `repo.GetPaymentByOrderID(orderID)`. Return `*Payment` or `apperr.ErrNotFound`.

### Part H — Implement RefundPayment ⏱ 20m

1. Fetch payment: `repo.GetPaymentByOrderID(orderID)`
2. Check status is `"success"` — return `apperr.ErrInvalidInput` if not
3. Look up gateway by `payment.Gateway` name
4. Convert amount to smallest unit
5. Build `RefundRequest`:
   - For Stripe: `{GatewayRef: payment.GatewayRef, Amount: amountInt}`
   - For bKash: `{GatewayRef: payment.GatewayRef, TrxID: <trxID from webhook_payload>, Amount: amountInt}`
   - Parse `trxID` from `payment.WebhookPayload` JSON when gateway is bKash
6. Call `gw.CreateRefund(req)`
7. `repo.UpdatePaymentStatus(payment.ID, "refunded", payment.GatewayRef)`
8. `orderUpdate.UpdateOrderStatus(orderID, "refunded")`

**Rules:**
- Never import `internal/order` — all cross-module calls go through interfaces
- `HandleStripeWebhook` and `HandleBkashCallback` are separate service methods — the handler routes to the correct one
- Always return nil (200) from `HandleStripeWebhook` for unknown event types — Stripe retries on error responses

---

## Step 10 — `internal/payment/handler.go` ⏱ 1h 10m

**Package:** `payment`  
**Purpose:** Parse HTTP requests, call service, write responses.

### Handler struct

```
Handler
  svc PaymentService
```

Constructor:
```go
func NewHandler(svc PaymentService) *Handler
```

### POST /api/v1/payments/initiate/:order_id ⏱ 15m (auth required)

1. Extract `order_id` path param: `r.PathValue("order_id")`
2. Get `userID` from context: `auth.UserIDFromContext(r.Context())`
3. Decode JSON body into `InitiateRequest` — validate `Gateway` field is non-empty
4. Call `svc.InitiatePayment(userID, orderID, &req)`
5. On error → `response.Error(w, err)`
6. On success → `response.JSON(w, http.StatusOK, initiateResponse)`

**Response shape:**

For Stripe: `{"gateway":"stripe","client_secret":"pi_...secret","payment_id":"..."}`  
For bKash: `{"gateway":"bkash","payment_url":"https://sandbox.bka.sh/...","payment_id":"..."}`

### POST /api/v1/payments/webhook ⏱ 20m (public — Stripe only)

```go
payload, err := io.ReadAll(r.Body)
if err != nil {
    response.Error(w, apperr.ErrInvalidInput)
    return
}
```

1. Read raw body with `io.ReadAll` — BEFORE any JSON decoding
2. Read `Stripe-Signature` header
3. Call `svc.HandleStripeWebhook(payload, signature)`
4. On error → `response.Error(w, err)`
5. On success → `response.JSON(w, http.StatusOK, map[string]string{"received": "true"})`

### GET /api/v1/payments/bkash/callback ⏱ 20m (public — bKash only)

bKash hits this as a GET redirect with query params.

```
GET /api/v1/payments/bkash/callback?paymentID=<id>&status=success
```

1. Read `paymentID` from query: `r.URL.Query().Get("paymentID")`
2. Read `status` from query: `r.URL.Query().Get("status")`
3. Validate both are non-empty — return 400 if missing
4. Call `svc.HandleBkashCallback(paymentID, status)`
5. On error → log with `slog.Error` → redirect to frontend failure URL (or return JSON error)
6. On success:
   - If `status == "success"` → redirect to frontend success page or `response.JSON(w, 200, ...)`
   - If status is failure → redirect to frontend failure page or `response.JSON(w, 200, ...)`

> **Design note:** In production the callback handler would redirect the user's browser to a frontend URL (e.g. `https://shop.example.com/payment/success`). For this backend-only project, return JSON. Define a `FRONTEND_URL` env var for future use.

### GET /api/v1/payments/:order_id ⏱ 10m (auth required)

1. Extract `order_id` path param
2. Call `svc.GetPaymentStatus(orderID)`
3. Map `*Payment` → `PaymentStatusResponse`
4. On error → `response.Error(w, err)`
5. On success → `response.JSON(w, http.StatusOK, statusResponse)`

### POST /api/v1/payments/refund/:order_id ⏱ 10m (admin)

1. Extract `order_id` path param
2. Call `svc.RefundPayment(orderID)`
3. On error → `response.Error(w, err)`
4. On success → `response.JSON(w, http.StatusOK, map[string]string{"message": "refund initiated"})`

---

## Step 11 — Register routes in `pkg/router/router.go` ⏱ 10m

Update `router.New()` to accept payment handler:

```go
func New(
    authHandler       *auth.Handler,
    authMiddleware    func(http.Handler) http.Handler,
    adminOnly         func(http.Handler) http.Handler,
    productHandler    *product.Handler,
    inventoryHandler  *inventory.Handler,
    orderHandler      *order.Handler,
    paymentHandler    *payment.Handler,
) http.Handler
```

Register payment routes:

```
POST /api/v1/payments/initiate/{order_id}    → paymentHandler.Initiate        (jwt)
POST /api/v1/payments/webhook                → paymentHandler.StripeWebhook   (public)
GET  /api/v1/payments/bkash/callback         → paymentHandler.BkashCallback   (public)
GET  /api/v1/payments/{order_id}             → paymentHandler.GetStatus        (jwt)
POST /api/v1/payments/refund/{order_id}      → paymentHandler.Refund           (jwt + adminOnly)
```

**Registration order matters — register literal paths before wildcard paths:**

```go
mux.HandleFunc("POST /api/v1/payments/webhook", paymentHandler.StripeWebhook)
mux.HandleFunc("GET  /api/v1/payments/bkash/callback", paymentHandler.BkashCallback)
mux.Handle("POST /api/v1/payments/initiate/{order_id}", jwt(http.HandlerFunc(paymentHandler.Initiate)))
mux.Handle("GET  /api/v1/payments/{order_id}", jwt(http.HandlerFunc(paymentHandler.GetStatus)))
mux.Handle("POST /api/v1/payments/refund/{order_id}", adminOnly(jwt(http.HandlerFunc(paymentHandler.Refund))))
```

---

## Step 12 — Update `pkg/config/config.go` ⏱ 15m

Add payment gateway credentials:

```go
type Config struct {
    Port        string
    DatabaseURL string
    JWTSecret   string

    // Stripe
    StripeKey           string
    StripeWebhookSecret string

    // bKash
    BkashAppKey      string
    BkashAppSecret   string
    BkashUsername    string
    BkashPassword    string
    BkashBaseURL     string   // default: sandbox URL
    BkashCallbackURL string   // e.g. https://yourserver.com/api/v1/payments/bkash/callback
}
```

Load from env vars:
```
STRIPE_KEY, STRIPE_WEBHOOK_SECRET
BKASH_APP_KEY, BKASH_APP_SECRET, BKASH_USERNAME, BKASH_PASSWORD
BKASH_BASE_URL (default to sandbox if empty)
BKASH_CALLBACK_URL
```

**Rule:** Payment gateway credentials are optional at startup — if both Stripe and bKash credentials are missing, log a warning but allow the server to start. The service returns `apperr.ErrInvalidInput` when a missing gateway is requested.

---

## Step 13 — Wire payment in `pkg/cmd/serve.go` ⏱ 20m

```go
// Build gateway list — include only configured gateways
var gateways []payment.PaymentGateway

if cfg.StripeKey != "" {
    gateways = append(gateways, stripe.NewClient(cfg.StripeKey, cfg.StripeWebhookSecret))
}
if cfg.BkashAppKey != "" {
    gateways = append(gateways, bkash.NewClient(
        cfg.BkashAppKey, cfg.BkashAppSecret,
        cfg.BkashUsername, cfg.BkashPassword,
        cfg.BkashBaseURL, cfg.BkashCallbackURL,
    ))
}

paymentRepo    := payment.NewPaymentRepository(db)
orderUpdater   := orderRepo   // implements payment.OrderStatusUpdater
orderReader    := orderRepo   // implements payment.OrderReader (add GetOrderSnapshot method)
paymentSvc     := payment.NewPaymentService(gateways, paymentRepo, orderUpdater, orderReader)
paymentHandler := payment.NewHandler(paymentSvc)
```

**Note on `orderReader`:** Add `GetOrderSnapshot(orderID string) (*payment.OrderSnapshot, error)` method to `internal/order/repository.go` implementation. The method queries the orders table and maps to `payment.OrderSnapshot` — this is the only place the order repo returns a payment-package type, which is fine because `serve.go` wires them together.

---

## Step 14 — Test Stripe flow ⏱ 15m

```bash
# Start Stripe webhook forwarding
stripe listen --forward-to localhost:8080/api/v1/payments/webhook

# Start server
go run . serve
```

```bash
# Initiate payment (Stripe)
curl -X POST localhost:8080/api/v1/payments/initiate/<order_id> \
  -H "Authorization: Bearer <customer_token>" \
  -H "Content-Type: application/json" \
  -d '{"gateway":"stripe"}'

# Trigger test webhook via Stripe CLI
stripe trigger payment_intent.succeeded

# Check status
curl -H "Authorization: Bearer <customer_token>" localhost:8080/api/v1/payments/<order_id>

# Refund (admin)
curl -X POST localhost:8080/api/v1/payments/refund/<order_id> \
  -H "Authorization: Bearer <admin_token>"
```

---

## Step 15 — Test bKash flow ⏱ 20m

```bash
# Initiate payment (bKash)
curl -X POST localhost:8080/api/v1/payments/initiate/<order_id> \
  -H "Authorization: Bearer <customer_token>" \
  -H "Content-Type: application/json" \
  -d '{"gateway":"bkash"}'
# Response includes "payment_url" — open in browser to simulate user payment

# Simulate bKash success callback (as if bKash redirected the user)
curl "localhost:8080/api/v1/payments/bkash/callback?paymentID=<bkash_payment_id>&status=success"

# Check payment status — should be "success"
curl -H "Authorization: Bearer <customer_token>" localhost:8080/api/v1/payments/<order_id>

# Check order status — should be "paid"
curl -H "Authorization: Bearer <customer_token>" localhost:8080/api/v1/orders/<order_id>

# Simulate failure callback
curl "localhost:8080/api/v1/payments/bkash/callback?paymentID=<bkash_payment_id>&status=failure"
# Payment status should be "failed", order status stays "pending"
```

---

## Completion Checklist

```
[ ] go run . migration up → 000008_payments applied
[ ] go build ./... → zero errors

Stripe:
[ ] POST /api/v1/payments/initiate/:order_id with "gateway":"stripe" → 200 with client_secret
[ ] POST /api/v1/payments/webhook with valid Stripe event → 200, order status updated to "paid"
[ ] POST /api/v1/payments/webhook with invalid signature → 401 UNAUTHORIZED
[ ] Unhandled Stripe event types return 200 (Stripe must not retry)

bKash:
[ ] POST /api/v1/payments/initiate/:order_id with "gateway":"bkash" → 200 with payment_url
[ ] GET /api/v1/payments/bkash/callback?status=success → 200, ExecutePayment called, order paid
[ ] GET /api/v1/payments/bkash/callback?status=failure → 200, payment status is "failed"
[ ] GET /api/v1/payments/bkash/callback with missing paymentID → 400 INVALID_INPUT
[ ] bKash token is cached — second request does not call /token/grant again

Both gateways:
[ ] POST /api/v1/payments/initiate with "gateway":"unknown" → 400 INVALID_INPUT
[ ] POST /api/v1/payments/initiate for another user's order → 403 FORBIDDEN
[ ] POST /api/v1/payments/initiate for already-paid order → 400 INVALID_INPUT
[ ] GET /api/v1/payments/:order_id → 200 with correct gateway name in response
[ ] GET /api/v1/payments/:order_id with no payment initiated → 404 NOT_FOUND
[ ] POST /api/v1/payments/refund/:order_id (admin) → 200 (Stripe and bKash)
[ ] POST /api/v1/payments/refund on non-paid order → 400 INVALID_INPUT
[ ] webhook_payload column in DB has raw JSON after successful payment
```

**Only move to Phase 6 when all boxes are checked.**

---

## Dependency Graph

```
migrations/sql/000008_payments
    └── internal/payment/entity.go         (time only)
    └── internal/payment/model.go          (time, json)
    └── internal/payment/gateway.go        (interfaces, primitive types only)
            ├── pkg/stripe/client.go        (payment.PaymentGateway impl — net/http, crypto/hmac)
            └── pkg/bkash/client.go         (payment.PaymentGateway impl — net/http, sync, time)
            └── internal/payment/repository.go  (entity, apperr, database/sql)
                    └── internal/payment/service.go
                          (PaymentGateway map, PaymentRepository,
                           OrderStatusUpdater, OrderReader — all interfaces)
                            └── internal/payment/handler.go
                                  (PaymentService, model, response, io, auth context)
                                    └── pkg/router/router.go
                                    └── pkg/cmd/serve.go
                                          (wires stripe.Client + bkash.Client into []PaymentGateway)
```

---

## bKash Sandbox Reference

| Credential | Env var | Where to get |
|------------|---------|-------------|
| App Key | `BKASH_APP_KEY` | bKash sandbox portal |
| App Secret | `BKASH_APP_SECRET` | bKash sandbox portal |
| Username | `BKASH_USERNAME` | bKash sandbox portal |
| Password | `BKASH_PASSWORD` | bKash sandbox portal |
| Base URL | `BKASH_BASE_URL` | `https://tokenized.sandbox.bka.sh/v1.2.0-beta/tokenized/checkout` |
| Callback URL | `BKASH_CALLBACK_URL` | Your server URL, e.g. `http://localhost:8080/api/v1/payments/bkash/callback` |

bKash sandbox test numbers: Use bKash-provided test wallet numbers from their sandbox documentation. Real phone numbers will not work in sandbox mode.
