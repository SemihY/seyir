class LogViewer {
  constructor() {
    this.logs = [];
    this.filteredLogs = [];
    this.currentPage = 1;
    this.pageSize = 50;
    this.bufferSize = 500;
    this.tailMode = true;
    this.searchQuery = "";
    this.evtSource = null;

    this.initElements();
    this.initEventListeners();
    this.initSSE();
    this.loadInitialLogs();
  }

  initElements() {
    this.logContainer = document.getElementById("logContainer");
    this.searchEl = document.getElementById("search");
    this.searchClear = document.getElementById("searchClear");
    this.tailToggle = document.getElementById("tailToggle");
    this.bufferSizeSelect = document.getElementById("bufferSize");
    this.pagination = document.getElementById("pagination");
    this.prevPageBtn = document.getElementById("prevPage");
    this.nextPageBtn = document.getElementById("nextPage");
    this.logCount = document.getElementById("logCount");
    this.pageInfo = document.getElementById("pageInfo");
    this.loading = document.getElementById("loading");
    this.connectionStatus = document.getElementById("connectionStatus");
    this.lastUpdate = document.getElementById("lastUpdate");
  }

  initEventListeners() {
    // Search functionality
    this.searchEl.addEventListener("input", (e) => {
      this.searchQuery = e.target.value.toLowerCase();
      this.searchClear.style.display = this.searchQuery ? "block" : "none";
      this.filterLogs();
    });

    this.searchClear.addEventListener("click", () => {
      this.searchEl.value = "";
      this.searchQuery = "";
      this.searchClear.style.display = "none";
      this.filterLogs();
      this.searchEl.focus();
    });

    // Tail mode toggle
    this.tailToggle.addEventListener("click", () => {
      this.tailMode = !this.tailMode;
      this.tailToggle.classList.toggle("active", this.tailMode);
      this.tailToggle.textContent = this.tailMode
        ? "📡 Tail Mode"
        : "📜 History Mode";

      if (this.tailMode) {
        this.initSSE();
      } else {
        this.closeSSE();
      }
    });

    // Buffer size change
    this.bufferSizeSelect.addEventListener("change", (e) => {
      this.bufferSize = parseInt(e.target.value);
      this.trimLogs();
      this.filterLogs();
    });

    // Pagination
    this.prevPageBtn.addEventListener("click", () => {
      if (this.currentPage > 1) {
        this.currentPage--;
        this.renderPage();
      }
    });

    this.nextPageBtn.addEventListener("click", () => {
      const totalPages = Math.ceil(this.filteredLogs.length / this.pageSize);
      if (this.currentPage < totalPages) {
        this.currentPage++;
        this.renderPage();
      }
    });

    // Auto-focus search
    this.searchEl.focus();
  }

  initSSE() {
    if (this.evtSource) {
      this.evtSource.close();
    }

    this.evtSource = new EventSource("/api/sse");

    this.evtSource.onopen = () => {
      this.connectionStatus.textContent = "🔗 Bağlı";
    };

    this.evtSource.onmessage = (e) => {
      try {
        const log = JSON.parse(e.data);
        this.addLog(log, true);
      } catch (err) {
        console.error("SSE parse error:", err);
      }
    };

    this.evtSource.onerror = () => {
      this.connectionStatus.textContent = "❌ Bağlantı Hatası";
      setTimeout(() => this.initSSE(), 5000);
    };
  }

  closeSSE() {
    if (this.evtSource) {
      this.evtSource.close();
      this.evtSource = null;
      this.connectionStatus.textContent = "⏸️ Durduruldu";
    }
  }

  async loadInitialLogs() {
    this.showLoading(true);
    try {
      // This would be implemented in the backend
      // For now, we'll start with empty logs and rely on SSE
      this.filterLogs();
    } catch (error) {
      console.error("Failed to load initial logs:", error);
    } finally {
      this.showLoading(false);
    }
  }

  addLog(log, isNew = false) {
    // Add timestamp if not present
    if (!log.timestamp) {
      log.timestamp = new Date().toLocaleTimeString();
    }

    // Mark as new or old
    log.isNew = isNew;
    log.id = log.id || Date.now() + Math.random();

    // Add to logs array
    this.logs.unshift(log);

    // Trim buffer if needed
    this.trimLogs();

    // Update display
    this.filterLogs();

    // Update last update time
    this.lastUpdate.textContent = `Son güncelleme: ${new Date().toLocaleTimeString()}`;

    // Auto scroll to top if in tail mode and on first page
    if (this.tailMode && this.currentPage === 1) {
      setTimeout(() => {
        this.logContainer.scrollTop = 0;
      }, 10);
    }
  }

  trimLogs() {
    if (this.logs.length > this.bufferSize) {
      this.logs = this.logs.slice(0, this.bufferSize);
    }
  }

  filterLogs() {
    if (this.searchQuery) {
      this.filteredLogs = this.logs.filter(
        (log) =>
          log.message.toLowerCase().includes(this.searchQuery) ||
          log.source.toLowerCase().includes(this.searchQuery) ||
          log.level.toLowerCase().includes(this.searchQuery)
      );
    } else {
      this.filteredLogs = [...this.logs];
    }

    // Reset to first page when filtering
    this.currentPage = 1;
    this.renderPage();
  }

  renderPage() {
    const startIndex = (this.currentPage - 1) * this.pageSize;
    const endIndex = startIndex + this.pageSize;
    const pageData = this.filteredLogs.slice(startIndex, endIndex);

    this.logContainer.innerHTML = "";

    pageData.forEach((log) => {
      const logElement = this.createLogElement(log);
      this.logContainer.appendChild(logElement);
    });

    this.updatePaginationInfo();
  }

  createLogElement(log) {
    const div = document.createElement("div");
    div.className = `log-entry ${log.isNew ? "new" : "old"}`;

    div.innerHTML = `
      <div class="log-timestamp">${log.timestamp || "--:--:--"}</div>
      <div class="log-source">${this.escapeHtml(log.source || "unknown")}</div>
      <div class="log-level ${log.level}">${log.level || "INFO"}</div>
      <div class="log-message">${this.escapeHtml(log.message || "")}</div>
    `;

    return div;
  }

  updatePaginationInfo() {
    const totalPages = Math.ceil(this.filteredLogs.length / this.pageSize);

    this.logCount.textContent = `${this.filteredLogs.length} log`;
    this.pageInfo.textContent = `Sayfa ${this.currentPage} / ${Math.max(
      1,
      totalPages
    )}`;

    this.prevPageBtn.disabled = this.currentPage <= 1;
    this.nextPageBtn.disabled = this.currentPage >= totalPages;

    // Hide pagination if only one page
    this.pagination.style.display = totalPages <= 1 ? "none" : "flex";
  }

  showLoading(show) {
    this.loading.classList.toggle("active", show);
  }

  escapeHtml(text) {
    const div = document.createElement("div");
    div.textContent = text;
    return div.innerHTML;
  }
}

// Initialize the log viewer
document.addEventListener("DOMContentLoaded", () => {
  window.logViewer = new LogViewer();
});
