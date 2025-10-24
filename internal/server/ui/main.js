class LogViewer {
  constructor() {
    this.currentPage = 1;
    this.pageSize = 100;
    this.totalLogs = 0;
    this.isLoading = false;

    this.initElements();
    this.initEventListeners();

    // Load initial data
    this.loadDistinctSources();
    this.applyFilters();
  }

  initElements() {
    this.logContainer = document.getElementById("logContainer");
    this.bufferSizeSelect = document.getElementById("bufferSize");
    this.pagination = document.getElementById("pagination");
    this.prevPageBtn = document.getElementById("prevPage");
    this.nextPageBtn = document.getElementById("nextPage");
    this.logCount = document.getElementById("logCount");
    this.pageInfo = document.getElementById("pageInfo");
    this.loading = document.getElementById("loading");
    this.connectionStatus = document.getElementById("connectionStatus");
    this.lastUpdate = document.getElementById("lastUpdate");

    // Validate critical elements
    if (!this.logContainer) {
      console.error("Critical element 'logContainer' not found");
      return;
    }
    if (!this.connectionStatus) {
      console.error("Critical element 'connectionStatus' not found");
      return;
    }

    // Filter elements
    this.sourceFilter = document.getElementById("sourceFilter");
    this.levelFilter = document.getElementById("levelFilter");
    this.traceIdFilter = document.getElementById("traceIdFilter");
    this.timeFromFilter = document.getElementById("timeFromFilter");
    this.timeToFilter = document.getElementById("timeToFilter");
    this.limitFilter = document.getElementById("limitFilter");
    this.applyFiltersBtn = document.getElementById("applyFilters");
    this.clearFiltersBtn = document.getElementById("clearFilters");
    this.refreshSourcesBtn = document.getElementById("refreshSources");

    // Time preset buttons
    this.timePresets = {
      last1h: document.getElementById("last1h"),
      last6h: document.getElementById("last6h"),
      last24h: document.getElementById("last24h"),
      last7d: document.getElementById("last7d"),
    };

    // Set default time range (last 24 hours)
    this.setDefaultTimeRange();
  }

  setDefaultTimeRange() {
    this.setTimeRange("last24h");
  }

  setTimeRange(preset) {
    if (!this.timeFromFilter || !this.timeToFilter) return;

    const now = new Date();
    let fromTime;

    switch (preset) {
      case "last1h":
        fromTime = new Date(now.getTime() - 1 * 60 * 60 * 1000);
        break;
      case "last6h":
        fromTime = new Date(now.getTime() - 6 * 60 * 60 * 1000);
        break;
      case "last24h":
        fromTime = new Date(now.getTime() - 24 * 60 * 60 * 1000);
        break;
      case "last7d":
        fromTime = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
        break;
      default:
        fromTime = new Date(now.getTime() - 24 * 60 * 60 * 1000);
    }

    // Format for datetime-local input (YYYY-MM-DDTHH:MM)
    const formatDateTime = (date) => {
      const year = date.getFullYear();
      const month = String(date.getMonth() + 1).padStart(2, "0");
      const day = String(date.getDate()).padStart(2, "0");
      const hours = String(date.getHours()).padStart(2, "0");
      const minutes = String(date.getMinutes()).padStart(2, "0");
      return `${year}-${month}-${day}T${hours}:${minutes}`;
    };

    this.timeFromFilter.value = formatDateTime(fromTime);
    this.timeToFilter.value = formatDateTime(now);

    // Update active button state
    Object.values(this.timePresets).forEach((btn) => {
      if (btn) btn.classList.remove("active");
    });
    if (this.timePresets[preset]) {
      this.timePresets[preset].classList.add("active");
    }
  }

  initEventListeners() {
    // Filter functionality
    if (this.applyFiltersBtn) {
      this.applyFiltersBtn.addEventListener("click", () => {
        this.applyFilters();
      });
    }

    if (this.clearFiltersBtn) {
      this.clearFiltersBtn.addEventListener("click", () => {
        this.clearAllFilters();
      });
    }

    if (this.refreshSourcesBtn) {
      this.refreshSourcesBtn.addEventListener("click", () => {
        this.loadDistinctSources();
      });
    }

    // Refresh button
    const refreshBtn = document.getElementById("refreshBtn");
    if (refreshBtn) {
      refreshBtn.addEventListener("click", () => {
        this.applyFilters();
      });
    }

    // Buffer size change (affects page size)
    if (this.bufferSizeSelect) {
      this.bufferSizeSelect.addEventListener("change", (e) => {
        this.pageSize = parseInt(e.target.value);
        this.applyFilters();
      });
    }

    // Limit filter change
    if (this.limitFilter) {
      this.limitFilter.addEventListener("change", (e) => {
        this.pageSize = parseInt(e.target.value);
        this.applyFilters();
      });
    }

    // Enter key on trace ID input
    if (this.traceIdFilter) {
      this.traceIdFilter.addEventListener("keypress", (e) => {
        if (e.key === "Enter") {
          this.applyFilters();
        }
      });
    }

    // Pagination
    if (this.prevPageBtn) {
      this.prevPageBtn.addEventListener("click", (e) => {
        e.preventDefault();
        if (!this.prevPageBtn.disabled && this.currentPage > 1) {
          this.navigateToPage(this.currentPage - 1);
        }
      });
    }

    if (this.nextPageBtn) {
      this.nextPageBtn.addEventListener("click", (e) => {
        e.preventDefault();
        if (!this.nextPageBtn.disabled) {
          this.navigateToPage(this.currentPage + 1);
        }
      });
    }

    // Time preset buttons
    Object.entries(this.timePresets).forEach(([key, button]) => {
      if (button) {
        button.addEventListener("click", () => {
          this.setTimeRange(key);
          this.applyFilters();
        });
      }
    });
  }

  applyFilters() {
    this.currentPage = 1;
    this.applyFiltersWithPage(1);
  }

  applyFiltersWithPage(page) {
    if (this.isLoading) return;

    this.currentPage = page || 1;
    this.showLoading(true);
    if (this.connectionStatus) {
      this.connectionStatus.textContent = "🔍 Querying...";
    }

    try {
      // Build query URL with filters and pagination
      let apiUrl = `/api/query?limit=${this.pageSize}`;

      // Add pagination offset
      const offset = (this.currentPage - 1) * this.pageSize;
      if (offset > 0) {
        apiUrl += `&offset=${offset}`;
      }

      // Add filters
      const sourceFilter = this.sourceFilter
        ? this.sourceFilter.value.trim()
        : "";
      if (sourceFilter) {
        apiUrl += `&source=${encodeURIComponent(sourceFilter)}`;
      }

      const levelFilter = this.levelFilter ? this.levelFilter.value.trim() : "";
      if (levelFilter) {
        apiUrl += `&level=${encodeURIComponent(levelFilter)}`;
      }

      const traceIdFilter = this.traceIdFilter
        ? this.traceIdFilter.value.trim()
        : "";
      if (traceIdFilter) {
        apiUrl += `&trace_id=${encodeURIComponent(traceIdFilter)}`;
      }

      const timeFromFilter = this.timeFromFilter
        ? this.timeFromFilter.value.trim()
        : "";
      if (timeFromFilter) {
        apiUrl += `&from=${encodeURIComponent(timeFromFilter)}`;
      }

      const timeToFilter = this.timeToFilter
        ? this.timeToFilter.value.trim()
        : "";
      if (timeToFilter) {
        apiUrl += `&to=${encodeURIComponent(timeToFilter)}`;
      }

      this.executeQuery(apiUrl);
    } catch (error) {
      console.error("Failed to apply filters:", error);
      this.showError("Error applying filters: " + error.message);
      if (this.connectionStatus) {
        this.connectionStatus.textContent = "❌ Filter Error";
      }
      this.showLoading(false);
    }
  }

  clearAllFilters() {
    if (this.sourceFilter) this.sourceFilter.value = "";
    if (this.levelFilter) this.levelFilter.value = "";
    if (this.traceIdFilter) this.traceIdFilter.value = "";
    if (this.limitFilter) this.limitFilter.value = "100";

    // Reset time filters to default (last 24 hours)
    this.setDefaultTimeRange();

    // Apply cleared filters
    this.applyFilters();
  }

  async loadDistinctSources() {
    try {
      this.refreshSourcesBtn.disabled = true;
      this.refreshSourcesBtn.textContent = "Loading...";

      const response = await fetch(
        "/api/query/distinct?column=source&limit=50"
      );
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Failed to load sources");
      }

      // Clear existing options except "All Sources"
      this.sourceFilter.innerHTML = '<option value="">All Sources</option>';

      // Add distinct sources
      if (data.values) {
        data.values.forEach((source) => {
          const option = document.createElement("option");
          option.value = source;
          option.textContent = source;
          this.sourceFilter.appendChild(option);
        });
      }
    } catch (error) {
      console.error("Failed to load sources:", error);
      this.connectionStatus.textContent = "❌ Source Load Error";
    } finally {
      this.refreshSourcesBtn.disabled = false;
      this.refreshSourcesBtn.innerHTML = `
        <svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16" style="margin-right: 0.25rem;">
          <path fill-rule="evenodd" d="M8 3a5 5 0 1 0 4.546 2.914.5.5 0 0 1 .908-.417A6 6 0 1 1 8 2v1z"/>
          <path d="M8 4.466V.534a.25.25 0 0 1 .41-.192l2.36 1.966c.12.1.12.284 0 .384L8.41 4.658A.25.25 0 0 1 8 4.466z"/>
        </svg>
        Refresh Sources
      `;
    }
  }

  async executeQuery(apiUrl) {
    try {
      console.log("Executing query:", apiUrl);
      this.connectionStatus.textContent = "🔍 Querying...";

      const response = await fetch(apiUrl);
      console.log("Response status:", response.status);

      const data = await response.json();
      console.log("Response data:", data);

      if (!response.ok) {
        throw new Error(
          data.error || `HTTP ${response.status}: Failed to execute query`
        );
      }

      // Transform data format to match existing UI expectations
      const transformedData = {
        logs: data.logs || [],
        total: data.total || 0,
        page: 1,
        hasNext: false,
        hasPrevious: false,
        query_time_ms: data.query_time_ms || 0,
      };

      this.displayLogs(transformedData);
      if (this.connectionStatus) {
        this.connectionStatus.textContent = `🎯 Found ${transformedData.total} logs (${transformedData.query_time_ms}ms)`;
      }
      if (this.lastUpdate) {
        this.lastUpdate.textContent = `Last update: ${new Date().toLocaleTimeString()}`;
      }
    } catch (error) {
      console.error("Failed to execute query:", error);

      // Check if it's a network error
      if (error instanceof TypeError && error.message.includes("fetch")) {
        this.showError(
          "Network Error: Cannot connect to server. Is the seyir server running?"
        );
        this.connectionStatus.textContent = "❌ Server Offline";
      } else {
        this.showError("Query execution failed: " + error.message);
        this.connectionStatus.textContent = "❌ Query Error";
      }
    } finally {
      this.showLoading(false);
    }
  }

  async loadLogs(page) {
    // Use applyFiltersWithPage for pagination
    this.applyFiltersWithPage(page || 1);
  }

  async searchLogs(query, page) {
    if (this.isLoading) return;

    this.currentPage = page;
    this.showLoading(true);
    this.connectionStatus.textContent = "🔍 Searching...";

    try {
      // Parse search query for filters (simple implementation)
      let apiUrl = `/api/query?limit=${this.pageSize}`;

      // Try to parse structured filters from search
      if (query.includes("level:")) {
        const levelMatch = query.match(/level:(\w+)/);
        if (levelMatch) {
          apiUrl += `&level=${encodeURIComponent(levelMatch[1])}`;
        }
      }

      if (query.includes("source:")) {
        const sourceMatch = query.match(/source:([^\s]+)/);
        if (sourceMatch) {
          apiUrl += `&source=${encodeURIComponent(sourceMatch[1])}`;
        }
      }

      const response = await fetch(apiUrl);
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Failed to search logs");
      }

      // Transform data format to match existing UI expectations
      const transformedData = {
        logs: data.logs || [],
        total: data.total || 0,
        page: 1,
        hasNext: false,
        hasPrevious: false,
      };

      this.displayLogs(transformedData);
      this.connectionStatus.textContent = `Found (${data.query_time_ms}ms)`;
      this.lastUpdate.textContent = `Last update: ${new Date().toLocaleTimeString()}`;
    } catch (error) {
      console.error("Failed to search logs:", error);
      this.showError("Error during search: " + error.message);
      this.connectionStatus.textContent = "❌ Search Error";
    } finally {
      this.showLoading(false);
    }
  }

  displayLogs(data) {
    this.totalLogs = data.total || 0;
    this.logContainer.innerHTML = "";

    if (!data.logs || data.logs.length === 0) {
      this.logContainer.innerHTML =
        '<tr><td colspan="6" class="status">No logs found with current projection pushdown query</td></tr>';
    } else {
      data.logs.forEach((log) => {
        const logElement = this.createLogElement(log);
        this.logContainer.appendChild(logElement);
      });
    }

    this.updatePaginationInfo(data);
  }

  createLogElement(log) {
    const tr = document.createElement("tr");

    // Format timestamp
    const timestamp = log.ts ? new Date(log.ts).toLocaleString() : "--:--:--";

    // Determine log level class
    const levelLower = (log.level || "INFO").toLowerCase();

    // Simplified layout for projection pushdown fields only
    const tagsHtml =
      log.tags && log.tags.length > 0
        ? log.tags
            .map((tag) => `<span class="tag">${this.escapeHtml(tag)}</span>`)
            .join(" ")
        : "-";

    tr.innerHTML = `
      <td class="timestamp">${timestamp}</td>
      <td>
        <span class="log-level log-level-${levelLower}">${
      log.level || "INFO"
    }</span>
      </td>
      <td class="message">${this.escapeHtml(log.message || "")}</td>
      <td class="source">${this.escapeHtml(log.source || "unknown")}</td>
      <td class="trace-id">
        ${
          log.trace_id
            ? `<code class="trace-id-code">${this.escapeHtml(
                log.trace_id
              )}</code>`
            : "-"
        }
      </td>
      <td class="tags">${tagsHtml}</td>
    `;

    return tr;
  }

  updatePaginationInfo(data) {
    const totalPages = Math.ceil((data.total || 0) / this.pageSize);
    const currentPage = this.currentPage || 1;

    if (this.logCount) {
      this.logCount.textContent = `${data.total || 0} logs`;
    }

    if (this.pageInfo) {
      this.pageInfo.textContent = `Page ${currentPage} / ${Math.max(
        1,
        totalPages
      )}`;
    }

    // Update button states based on current page and total pages
    if (this.prevPageBtn) {
      this.prevPageBtn.disabled = currentPage <= 1;
    }

    if (this.nextPageBtn) {
      this.nextPageBtn.disabled = currentPage >= totalPages || totalPages <= 1;
    }

    // Hide pagination if only one page or no logs
    if (this.pagination) {
      this.pagination.style.display = totalPages <= 1 ? "none" : "flex";
    }
  }

  navigateToPage(page) {
    if (this.isLoading) return;

    this.currentPage = Math.max(1, page);
    this.applyFiltersWithPage(this.currentPage);
  }

  showLoading(show) {
    this.isLoading = show;
    if (this.loading) {
      this.loading.classList.toggle("active", show);
    }
  }

  showError(message) {
    this.logContainer.innerHTML = `
      <tr>
        <td colspan="6" class="error">
          <div>⚠️ Error</div>
          <div>${message}</div>
        </td>
      </tr>
    `;
    this.pagination.style.display = "none";
  }

  escapeHtml(text) {
    const div = document.createElement("div");
    div.textContent = text;
    return div.innerHTML;
  }
}

// Toggle details function for structured data
function toggleDetails(detailId) {
  const detailsRow = document.getElementById(detailId);
  if (detailsRow) {
    detailsRow.classList.toggle("hidden");
  }
}

// Initialize the log viewer
document.addEventListener("DOMContentLoaded", () => {
  window.logViewer = new LogViewer();
});
