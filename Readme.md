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

**One-line install (user-level):**

```bash
curl -fsSL https://semihy.github.io/seyir/scripts/install.sh | bash
```

Or manually:

```bash
make install-user   # Local
make install        # System-wide
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


## 🧩 Coolify Integration

Seyir runs perfectly as a self-hosted app on [Coolify](https://coolify.io/):


> Once deployed, you can view logs from Docker containers or stream local logs directly through the web UI.


## 🤝 Contributing

Contributions and feedback are welcome!
Open an issue or PR at [github.com/SemihY/seyir](https://github.com/semihy/seyir)

---

## 🧭 Version

```bash
seyir --version
# seyir 0.3.1 (build 2025-10-20)
```

---

## 📜 License

MIT License © 2025 Semih Yıldız

---