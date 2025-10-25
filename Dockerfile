ARG TARGETOS TARGETARCH

FROM --platform=$BUILDPLATFORM golang:1.23-bookworm AS builder

# Install build dependencies
RUN apt-get update && apt-get install -y \
    gcc \
    g++ \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Set GOTOOLCHAIN to auto to allow downloading Go 1.24
ENV GOTOOLCHAIN=auto

# Copy dependency files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary for target platform
ARG VERSION="dev"
ARG COMMIT="unknown"
ARG BUILD_DATE="unknown"

RUN CGO_ENABLED=1 \
    go build -ldflags "\
        -s -w \
        -X seyir/internal/version.Version=${VERSION} \
        -X seyir/internal/version.Commit=${COMMIT} \
        -X seyir/internal/version.BuildDate=${BUILD_DATE}" \
    -o seyir ./cmd/seyir/main.go

FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    wget \
    && rm -rf /var/lib/apt/lists/*

# Install Docker CLI for the target architecture
ARG TARGETARCH
RUN DOCKER_ARCH=$(case ${TARGETARCH} in \
        "amd64") echo "x86_64" ;; \
        "arm64") echo "aarch64" ;; \
        *) echo ${TARGETARCH} ;; \
    esac) && \
    wget https://download.docker.com/linux/static/stable/${DOCKER_ARCH}/docker-24.0.7.tgz && \
    tar -xzf docker-24.0.7.tgz && \
    mv docker/docker /usr/local/bin/ && \
    rm -rf docker docker-24.0.7.tgz

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/seyir .

# Create data directory
RUN mkdir -p /app/data

# Expose web port
EXPOSE 5555

# Environment variable with default
ENV PORT="5555"

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:5555 || exit 1

# Run seyir service (simplified - containers opt-in with labels)
CMD ./seyir service --port ${PORT}