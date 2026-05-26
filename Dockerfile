# --- Build Stage ---
FROM golang:1.26-alpine AS builder

# Install build essentials
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and UI assets
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o nameforge ./cmd/api

# --- Runtime Stage ---
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/nameforge .

# Copy static frontend assets
COPY public/ ./public/

# Expose HTTP port
EXPOSE 8080

# Run service
ENTRYPOINT ["./nameforge"]
