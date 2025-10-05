FROM golang:1.24-bullseye AS builder

# Install dependencies for building
RUN apt-get update && apt-get install -y \
    gcc \
    libc6-dev \
    libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application with simpler flags
RUN CGO_ENABLED=1 go build -o seyir ./cmd/seyir

FROM alpine:latest

# Install docker CLI and ca-certificates
RUN apk --no-cache add ca-certificates docker-cli

WORKDIR /app

# Copy the binary
COPY --from=builder /app/seyir .

# Create data directory
RUN mkdir -p /app/data

# Expose web port
EXPOSE 8080

# Environment variable with default
ENV PORT="8080"

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:${PORT} || exit 1

# Run seyir service (simplified - containers opt-in with labels)
CMD ["sh", "-c", "./seyir service --port ${PORT}"]