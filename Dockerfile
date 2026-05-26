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
FROM alpine:3.20

# Install runtime dependencies and create non-root user
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 -S appgroup \
    && adduser -u 10001 -S appuser -G appgroup

WORKDIR /app

# Copy binary from builder with correct ownership
COPY --from=builder --chown=appuser:appgroup /app/nameforge .

# Copy static frontend assets with correct ownership
COPY --chown=appuser:appgroup public/ ./public/

# Switch to non-root user for execution
USER appuser:appgroup

# Expose HTTP port
EXPOSE 8080

# Run service
ENTRYPOINT ["./nameforge"]
