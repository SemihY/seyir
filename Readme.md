# 🪶 Logspot

> Lightweight, zero-dependency log viewer that collects and searches local or container logs from multiple sources in real time.

---

## 🚀 Features

* **🧩 Zero dependencies:** Uses only DuckDB — no external services or databases.
* **🔍 Instant search:** Simple HTML/JS interface to view and filter logs.
* **📦 Two operating modes:**

  * **Local Mode:** Pipe logs directly from any terminal command (`| logspot`).
  * **Container Mode (beta):** Automatically discovers and collects logs from containers in a Coolify or Docker environment.
* **🗂 Retention:** Background cleaner (`retention.go`) automatically purges old logs.
* **💾 DuckDB backend:** Fast analytical queries with minimal resource usage.
* **🖥 macOS Menubar UI:** Quick access to logs from the system tray.
* **🧱 Modular & Extensible:** Add your own collectors for new log sources (e.g., Docker, journald).
* **⚙️ CLI-first:** Install, run, and configure entirely from the terminal.

---

## 🧑‍💻 Installation

### 🍺 Homebrew

```bash
brew install logspot
```

or

### 🌐 Curl installer

```bash
curl -fsSL https://get.logspot.sh | bash
```

---

## ⚡ Usage

### 1️⃣ Local Mode

Pipe logs from any CLI command into Logspot:

```bash
myapp | logspot
```

Then open the dashboard in your browser:
👉 **[http://localhost:7070](http://localhost:7070)**

You’ll see a live feed of your logs with instant text search.

---

### 2️⃣ Container Mode (Beta)

Run Logspot in your Coolify or Docker environment to automatically collect logs from other containers:

```bash
docker run -d \
  --name logspot \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -p 7070:7070 \
  ghcr.io/logspot/logspot:latest
```

The built-in **auto-discovery** module (`autodiscovery.go`) watches running containers and streams their logs into DuckDB.

---

## 🧱 Project Structure

```
logspot/
├── cmd/
│   ├── logspot/             # Main CLI entrypoint
│   └── menu/                # macOS menubar UI
├── internal/
│   ├── collector/           # stdin & container log collectors
│   ├── db/                  # DuckDB setup & query layer
│   ├── discovery/           # Container auto-discovery
│   ├── retention/           # Log retention and cleanup
│   ├── server/              # HTTP server + web UI
│   └── tail/                # Live log tailing
├── ui/
│   ├── index.html           # Lightweight log viewer
│   └── main.js              # Instant search logic
├── Makefile                 # Build, run, clean targets
├── .goreleaser.yaml         # Release configuration for Brew & GHCR
├── go.mod                   # Go module definition
└── README.md
```

---

## 🧩 Architecture Overview

* **Collector**: Streams logs from stdin or containers into DuckDB.
* **Database (DuckDB)**: Persistent local storage with SQL query support.
* **Server**: Minimal embedded HTTP server for the UI and search API.
* **UI**: Pure HTML/JS page served from `/`, no frameworks.
* **Retention**: Background goroutine that deletes old logs based on configured TTL.
* **Menu App (macOS)**: Optional menubar integration using `webview` or `fyne`.

---

## 🧰 Development

### Build locally

```bash
make build
```

### Run locally

```bash
./bin/logspot
```

### Package with GoReleaser

```bash
goreleaser release --snapshot --clean
```

---

## 🧼 Configuration

| Env Variable             | Description                      | Default              |
| ------------------------ | -------------------------------- | -------------------- |
| `LOGSPOT_PORT`           | HTTP port for the web UI         | `7070`               |
| `LOGSPOT_DB_PATH`        | Path to the DuckDB database file | `~/.logspot/logs.db` |
| `LOGSPOT_RETENTION_DAYS` | Retention period for logs        | `7`                  |
| `LOGSPOT_MODE`           | `local` or `container`           | `local`              |

---

## 🧪 Example

```bash
docker logs my-service -f | logspot
```

Visit:
👉 [http://localhost:7070](http://localhost:7070)

Type to search logs instantly.

---

## 🧱 License

MIT © Logspot Contributors
