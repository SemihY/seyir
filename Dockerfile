FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o seyir ./cmd/seyir

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

# Environment variables with defaults
ENV PROJECT=""
ENV LABELS=""
ENV PORT="8080"

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:${PORT} || exit 1

# Run seyir service
CMD ["sh", "-c", "./seyir service --project ${PROJECT} --port ${PORT} --labels \"${LABELS}\""]