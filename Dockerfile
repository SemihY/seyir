FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    wget \
    && rm -rf /var/lib/apt/lists/*

# Install Docker CLI
RUN wget https://download.docker.com/linux/static/stable/$(uname -m)/docker-24.0.7.tgz && \
    tar -xzf docker-24.0.7.tgz && \
    mv docker/docker /usr/local/bin/ && \
    rm -rf docker docker-24.0.7.tgz

WORKDIR /app

# Copy the pre-built binary
COPY seyir .

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