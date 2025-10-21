# Seyir

Centralized log collector and viewer with Docker container auto-discovery.

## Features

- **Auto-discovery**: Monitors Docker containers with `seyir.enable=true` label
- **Pipe logs**: Stream logs from any source via stdin
- **Fast search**: DuckDB-powered queries on compressed Parquet storage
- **Web UI**: Real-time log viewing at `http://localhost:5555`
- **Structured parsing**: JSON, key-value pairs, timestamps
- **Compaction**: Automatic merging of small files
- **Retention**: Configurable log retention policies

## Quick Start

### Install

```bash
# User install
make install-user

# System-wide install
make install
```

### Run Service

```bash
# Start with Docker auto-discovery + web UI
seyir service

# Web UI only
seyir web --port 8080
```

### Label Containers

```bash
# Basic tracking
docker run -l seyir.enable=true my-app

# With metadata
docker run -l seyir.enable=true -l seyir.project=web -l seyir.component=api backend-service
```

### Pipe Logs

```bash
# From Docker
docker logs mycontainer | seyir

# From Kubernetes
kubectl logs -f deployment/api | seyir

# From file
tail -f app.log | seyir
```

## Commands

```bash
seyir service              # Start collector + web UI
seyir web                  # Web UI only
seyir search <query>       # Search logs
seyir sessions             # List active sessions
seyir cleanup              # Remove old sessions
seyir batch stats          # Show buffer statistics
seyir batch config         # Manage configuration
```

## Search Examples

```bash
# Simple search
seyir --search "error" search

# With filters
seyir query filter --levels=ERROR,WARN --limit=100

# By trace ID
seyir query filter --trace-ids=abc123

# Time range
seyir query filter --start='2025-01-01 00:00:00' --end='2025-01-02 00:00:00'

# Distinct values
seyir query distinct --column=source
```

## Configuration

Located at `~/.seyir/config/config.json` or `config/config.json`:

```json
{
  "ultra_light": {
    "enabled": true,
    "buffer_size": 10000,
    "export_interval_seconds": 30
  },
  "retention": {
    "enabled": true,
    "retention_days": 30
  },
  "debug": {
    "enable_query_debug": false,
    "enable_batch_debug": false,
    "enable_server_debug": false,
    "enable_db_debug": false
  }
}
```

Update config:

```bash
seyir batch config set buffer_size 5000
seyir batch config set export_interval 60
seyir batch retention enable
```

Enable debug logging (shows INFO/DEBUG messages):

```bash
# Edit config file and set any debug flag to true
# Then restart seyir
```

## Docker Deployment

```bash
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v seyir-data:/app/data \
  -p 5555:5555 \
  seyir:latest
```

Or with compose:

```bash
make docker-run
```

## Development

```bash
# Build
make build

# Run locally
make run

# Test pipe mode
make demo

# Dependencies
make deps
```

## Data Storage

Logs are stored in `~/.seyir/lake/` as compressed Parquet files, organized by session and timestamp.

## Architecture

- **Collectors**: Docker auto-discovery, stdin pipe
- **Parser**: Structured log parsing (JSON, key-value)
- **Storage**: DuckDB + Parquet (columnar compression)
- **Web Server**: Real-time log streaming and search
- **Compaction**: Automatic file merging
- **Retention**: Time-based cleanup

## License

MIT
