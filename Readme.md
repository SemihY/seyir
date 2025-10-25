# Seyir

> **Seyir** is a lightweight, self-hosted log collector and viewer — stream your local or container logs through a simple pipe and search them instantly.

Built for developers who want to **pipe**, **store**, and **search** logs locally — no cloud, no agents, no external dependencies.

![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)
![Docker](https://img.shields.io/badge/Docker-ready-blue)
![Made with Go](https://img.shields.io/badge/Made%20with-Go-00ADD8)

---

## ✨ Features

* **Pipe anything** → View and search logs streamed via `stdin`
* **Auto-discovery** → Detects Docker containers with `seyir.enable=true`
* **DuckDB storage** → Fast search on compressed Parquet files
* **Web UI** → Real-time viewing at `http://localhost:5555`
* **Structured parsing** → JSON, key-value, and timestamps
* **Compaction** → Automatic file merging for performance
* **Retention** → Configurable cleanup by time or size
* **Self-contained** → No database, no dependencies

---

## ⚡ Quick Start

### 1. Install

**Quick install (Linux/macOS):**

```bash
# Latest version
curl -fsSL https://semihy.github.io/seyir/scripts/install.sh | bash

# Verify installation
seyir version
```

**Docker:**

```bash
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v seyir-data:/app/data \
  -p 8080:8080 \
  ghcr.io/semihy/seyir:latest
```

**From source:**

```bash
git clone https://github.com/SemihY/seyir.git
cd seyir
make install-user   # Local (~/.local/bin)
make install        # System-wide (/usr/local/bin)
```

---

### 2. Run Service

```bash
# Start collector + Web UI
seyir service

# Only Web UI (read-only mode)
seyir web --port 8080
```

Then visit → [http://localhost:5555](http://localhost:5555)

---

### 3. Pipe Logs

```bash
# From Docker
docker logs mycontainer | seyir

# From Kubernetes
kubectl logs -f deployment/api | seyir

# From file
tail -f app.log | seyir
```

---

## 🔍 Search Examples

```bash
seyir search "error"
seyir query filter --levels=ERROR,WARN --limit=100
seyir query filter --trace-ids=abc123
seyir query filter --start='2025-01-01' --end='2025-01-02'
seyir query distinct --column=source
```

---

## ⚙️ Configuration

`~/.seyir/config/config.json`:

```json
{
  "retention": { "enabled": true, "retention_days": 30 },
  "ultra_light": { "enabled": true, "buffer_size": 10000 },
  "debug": { "enable_query_debug": false }
}
```

Reload after editing:

```bash
seyir service restart
```


## 🐳 Coolify Deployment

Deploy Seyir as a self-hosted log collector on [Coolify](https://coolify.io/) with zero configuration:

### Quick Setup

1. **Create New Resource** → **Docker Service**
2. **Image:** `ghcr.io/semihy/seyir:latest`
3. **Port:** `5555:5555`
4. **Deploy** ✅

### Detailed Configuration

**Docker Image Settings:**
```
Image: ghcr.io/semihy/seyir:latest
Tag: latest (or specific version like v1.0.0)
```

**Port Mapping:**
```
Internal Port: 5555
External Port: 5555 (or your preferred port)
```

**Volume Mappings:**
```bash
# Docker socket for container discovery
/var/run/docker.sock → /var/run/docker.sock (bind mount)

# Persistent log storage
seyir-data → /app/data (named volume)
```

**Environment Variables:**
```bash
PORT=5555                    # Web server port
```

### Container Auto-Discovery

Once deployed, Seyir automatically discovers Docker containers. To make your containers visible in Seyir, add labels:

```yaml
# In your docker-compose.yml or Coolify service
services:
  your-app:
    image: your-app:latest
    labels:
      - "seyir.enable=true"
      - "seyir.name=My Application"
      - "seyir.description=Production API service"
      - "seyir.env=production"
```

### Access & Usage

After deployment:
- **Web UI:** `https://your-domain.com` (via Coolify proxy)
- **Direct Access:** `http://server-ip:5555`

### Version Updates

To update Seyir in Coolify:

1. **Auto-update:** Enable auto-deploy for `latest` tag
2. **Manual:** Change image tag to specific version: `ghcr.io/semihy/seyir:v1.2.0`
3. **Redeploy** the service

### Backup & Persistence

Seyir stores logs in `/app/data` - ensure this volume is backed up:

### Troubleshooting

**Can't see containers:**
- Ensure `/var/run/docker.sock` is mounted
- Add `seyir.enable=true` label to containers
- Check Seyir logs for discovery errors

**Performance issues:**
- Increase volume size for log storage
- Configure log retention in Seyir settings
- Monitor disk usage in Coolify

### Example Coolify Configuration

**Service Configuration:**
```yaml
# Coolify Docker Service Settings
name: seyir
image: ghcr.io/semihy/seyir:latest
ports:
  - "5555:5555"
volumes:
  - "/var/run/docker.sock:/var/run/docker.sock"
  - "seyir-data:/app/data"
environment:
  PORT: "5555"
restart: unless-stopped
```

**Domain & SSL:**
- Enable Coolify's automatic SSL
- Set custom domain: `logs.yourdomain.com`
- Configure basic auth if needed for security

Once deployed, you can view logs from all your Coolify services in one centralized location! 🚀

---

## 🤝 Contributing

Contributions and feedback are welcome!
Open an issue or PR at [github.com/SemihY/seyir](https://github.com/semihy/seyir)

---

## 🧭 Version

```bash
seyir --version
# Version: v1.0.0
# Commit: a1b2c3d4
# Built: 2025-10-25_14:30:15
# Go: go1.24.7
# Platform: linux/amd64
```

---

## 📜 License

MIT License © 2025 Semih Yıldız

---