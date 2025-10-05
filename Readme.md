# 🪶 seyir

> Lightweight, zero-dependency log viewer that collects and searches local or container logs from multiple sources in real time

---

---

## 🚀 Features

* **🧩 Zero dependencies:** Uses only DuckDB — no external services or databases.
* **🔍 Instant search:** Simple HTML/JS interface to view and filter logs.
* **📦 Multiple operating modes:**
  * **Pipe Mode:** Stream logs directly from any terminal command (`| seyir`).
  * **Container Mode (beta):** Automatically discovers and collects logs from containers.
* **🗂 Retention:** Background cleaner automatically purges old logs.
* **💾 DuckDB backend:** Fast analytical queries with minimal resource usage.
* **🖥 macOS Menubar UI:** Quick access to logs from the system tray.
* **🧱 Modular & Extensible:** Add your own collectors for new log sources.
* **⚙️ CLI-first:** Install, run, and configure entirely from the terminal.

---

## 🧑‍💻 Installation

### 🍺 Homebrew

```bash
brew install seyir
```

or

### 🌐 Curl installer

```bash
curl -fsSL https://get.seyir.sh | bash
```

---

### 2️⃣ Container Mode (Beta)

Run seyir in your Coolify or Docker environment to automatically collect logs from other containers:

```bash
docker run -d \
  --name seyir \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v ~/.seyir:/data \
  -p 7777:7777 \
  -e ENABLE_DOCKER_CAPTURE=true \
  ghcr.io/seyir/seyir:latest
```

## 🧰 Development

### Build locally

```bash
make build
```

### Run locally

```bash
./bin/seyir
```

### Test with multiple pipes

```bash
# Terminal 1
echo "App1: Starting service" | ./bin/seyir

# Terminal 2  
echo "App2: Database connected" | ./bin/seyir

# Terminal 3
echo "App3: Error occurred" | ./bin/seyir

# All logs appear in the same dashboard at http://localhost:7777
```

### Package with GoReleaser

```bash
goreleaser release --snapshot --clean
```

---

## 🧼 Configuration

| Env Variable             | Description                      | Default                    |
| ------------------------ | -------------------------------- | -------------------------- |
| `seyir_PORT`           | HTTP port for the web UI         | `7777`                     |
| `seyir_DB_PATH`        | Path to the DuckDB database file | `~/.seyir/logs.duckdb`   |
| `seyir_RETENTION_DAYS` | Retention period for logs        | `7`                        |
| `ENABLE_DOCKER_CAPTURE`  | Enable container auto-discovery  | `false`                    |
| `DISABLE_AUTO_OPEN`      | Disable auto-opening browser     | `false`                    |

---

## 🧪 Example Usage

### Basic Logging
```bash
# Single application
docker logs my-service -f | seyir

# Multiple applications (each creates own DuckDB instance)
kubectl logs -f deployment/api | seyir &
kubectl logs -f deployment/worker | seyir &
kubectl logs -f deployment/scheduler | seyir &
```

### Querying the Lake
Visit: 👉 [http://localhost:7777](http://localhost:7777)

- **Live Search**: Type to filter logs across all sources instantly
- **Source Filtering**: Filter by application/container name  
- **Level Filtering**: Show only ERROR, WARN, INFO, or DEBUG logs
- **Time Range**: Search within specific time periods
- **Federated View**: See logs from all connected sources in one interface

---

## 🏗️ Architecture Benefits

### Scalability
- **Horizontal**: Add more pipe sources without affecting existing ones
- **Isolation**: Each collector runs independently 
- **Performance**: Parallel ingestion to shared lake

### Reliability  
- **Fault Tolerance**: One failed collector doesn't affect others
- **Data Consistency**: Unified lake ensures all logs are queryable
- **Resource Efficiency**: Each DuckDB instance optimized for its workload

### Developer Experience
- **Simple Integration**: Just add `| seyir` to any command
- **Unified View**: All logs searchable in one dashboard
- **Zero Config**: Works out of the box with sensible defaults

---

## 🛣️ Roadmap

- [ ] **Query API**: REST endpoints for programmatic access
- [ ] **Log Correlation**: Trace ID tracking across services  
- [ ] **Alerting**: Real-time notifications on error patterns
- [ ] **Dashboards**: Custom analytics views and metrics
- [ ] **Export**: Export logs to external systems

---

## 🧱 License

MIT © seyir Contributors
