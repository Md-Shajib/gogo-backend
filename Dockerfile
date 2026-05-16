# Stage 1 — build
FROM golang:1.21-alpine AS builder

WORKDIR /build

# copy dependency files first — cached layer if deps unchanged
COPY go.mod go.sum ./
RUN go mod download

# copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o app .

# Stage 2 — final image
FROM alpine:latest

# ca-certificates required for HTTPS calls (Stripe API)
RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /build/app .
COPY --from=builder /build/migrations ./migrations

EXPOSE 8080

CMD ["/app/app", "serve"]
