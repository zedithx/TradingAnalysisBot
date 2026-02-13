# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bot main.go

# Runtime stage — minimal image
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /bot /app/bot

# Create data directory for persistent storage
RUN mkdir -p /app/data

# Default data directory (mount a volume here for persistence)
ENV DATA_DIR=/app/data

ENTRYPOINT ["/app/bot"]
