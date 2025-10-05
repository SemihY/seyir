# 🪶 seyir

> Lightweight, zero-dependency log viewer that collects and searches local or container logs from multiple sources in real time with DuckLake federation support.

---

---

## 🚀 Features

* **🧩 Zero dependencies:** Uses only DuckDB — no external services or databases.
* **🔍 Instant search:** Simple HTML/JS interface to view and filter logs.
* **🏗️ Lake Architecture:** Each pipe operation creates its own DuckDB instance, all connected to a unified DuckLake for federated queries.
* **📦 Multiple operating modes:**
  * **Pipe Mode:** Stream logs directly from any terminal command (`| seyir`).
  * **Container Mode (beta):** Automatically discovers and collects logs from containers.
* **🌊 DuckLake Federation:** Query across multiple log sources with unified SQL interface.
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

## ⚡ Usage

### 1️⃣ Pipe Mode (Lake Architecture)

Each command creates its own DuckDB instance connected to the same DuckLake:

```bash
# Each pipe creates a new DuckDB instance
myapp1 | seyir    # Creates DuckDB instance 1 → connects to shared lake
myapp2 | seyir    # Creates DuckDB instance 2 → connects to shared lake  
myapp3 | seyir    # Creates DuckDB instance 3 → connects to shared lake
```

All logs are stored in the unified DuckLake at `~/.seyir/logs.duckdb` for federated querying.

Then open the dashboard in your browser:
👉 **[http://localhost:7777](http://localhost:7777)**

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

---

## 🌊 DuckLake Architecture

### How It Works

1. **Each Pipe = New DuckDB Instance**: Every `| seyir` command creates a fresh DuckDB process
2. **Shared Lake Connection**: All instances connect to the same database file (`~/.seyir/logs.duckdb`)
3. **Federated Queries**: Query across all log sources through the unified lake interface
4. **Concurrent Safe**: Multiple collectors can write simultaneously to the shared lake

### Example Architecture
```
myapp1 | seyir  →  DuckDB Instance A  ┐
myapp2 | seyir  →  DuckDB Instance B  ├→ Shared DuckLake
myapp3 | seyir  →  DuckDB Instance C  ┘   (~/.seyir/logs.duckdb)
                                           ↓
                                      Web Dashboard
                                   (federated queries)
```

---

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
- [ ] **Clustering**: Multi-node DuckLake federation

---

## 🧱 License

MIT © seyir Contributors
