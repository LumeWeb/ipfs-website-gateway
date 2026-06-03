# Build stage
FROM golang:1.26-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o gateway cmd/gateway/main.go

# Runtime stage
FROM alpine:latest

# Install runtime dependencies and su-exec for privilege dropping
RUN apk --no-cache add ca-certificates tzdata su-exec

# Create non-root user
RUN addgroup -g 1000 gateway && \
    adduser -D -u 1000 -G gateway gateway

# Create data directories with correct ownership
RUN mkdir -p /data/cache /data/ipns /etc/lumeweb/gateway && \
    chown -R gateway:gateway /data /etc/lumeweb

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/gateway /app/gateway

# Copy entrypoint script
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

# Change ownership
RUN chown -R gateway:gateway /app

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:8080/healthz || exit 1

ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/gateway"]
