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
  }

  initEventListeners() {
    // Filter functionality
    this.applyFiltersBtn.addEventListener("click", () => {
      this.applyFilters();
    });

    this.clearFiltersBtn.addEventListener("click", () => {
      this.clearAllFilters();
    });

    this.refreshSourcesBtn.addEventListener("click", () => {
      this.loadDistinctSources();
    });

    // Refresh button
    document.getElementById("refreshBtn").addEventListener("click", () => {
      this.applyFilters();
    });

    // Buffer size change (affects page size)
    this.bufferSizeSelect.addEventListener("change", (e) => {
      this.pageSize = parseInt(e.target.value);
      this.applyFilters();
    });

    // Limit filter change
    this.limitFilter.addEventListener("change", (e) => {
      this.pageSize = parseInt(e.target.value);
      this.applyFilters();
    });

    // Enter key on trace ID input
    this.traceIdFilter.addEventListener("keypress", (e) => {
      if (e.key === "Enter") {
        this.applyFilters();
      }
    });

    // Pagination
    this.prevPageBtn.addEventListener("click", () => {
      if (this.currentPage > 1) {
        this.navigateToPage(this.currentPage - 1);
      }
    });

    this.nextPageBtn.addEventListener("click", () => {
      this.navigateToPage(this.currentPage + 1);
    });
  }

  applyFilters() {
    if (this.isLoading) return;

    this.currentPage = 1;
    this.showLoading(true);
    this.connectionStatus.textContent = "🔍 Applying filters...";

    try {
      // Build query URL with filters
      let apiUrl = `/api/query?limit=${this.pageSize}`;

      // Add filters
      const sourceFilter = this.sourceFilter.value.trim();
      if (sourceFilter) {
        apiUrl += `&source=${encodeURIComponent(sourceFilter)}`;
      }

      const levelFilter = this.levelFilter.value.trim();
      if (levelFilter) {
        apiUrl += `&level=${encodeURIComponent(levelFilter)}`;
      }

      const traceIdFilter = this.traceIdFilter.value.trim();
      if (traceIdFilter) {
        apiUrl += `&trace_id=${encodeURIComponent(traceIdFilter)}`;
      }

      const timeFromFilter = this.timeFromFilter.value.trim();
      if (timeFromFilter) {
        apiUrl += `&from=${encodeURIComponent(timeFromFilter)}`;
      }

      const timeToFilter = this.timeToFilter.value.trim();
      if (timeToFilter) {
        apiUrl += `&to=${encodeURIComponent(timeToFilter)}`;
      }

      this.executeQuery(apiUrl);
    } catch (error) {
      console.error("Failed to apply filters:", error);
      this.showError("Error applying filters: " + error.message);
      this.connectionStatus.textContent = "❌ Filter Error";
      this.showLoading(false);
    }
  }

  clearAllFilters() {
    this.sourceFilter.value = "";
    this.levelFilter.value = "";
    this.traceIdFilter.value = "";
    this.timeFromFilter.value = "";
    this.timeToFilter.value = "";
    this.limitFilter.value = "100";

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
      this.connectionStatus.textContent = `🎯 Found ${transformedData.total} logs (${transformedData.query_time_ms}ms)`;
      this.lastUpdate.textContent = `Last update: ${new Date().toLocaleTimeString()}`;
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
    // Use applyFilters instead of direct loading
    this.applyFilters();
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
    const totalPages = Math.ceil(data.total / this.pageSize);

    this.logCount.textContent = `${data.total || 0} logs`;
    this.pageInfo.textContent = `Page ${data.page || 1} / ${Math.max(
      1,
      totalPages
    )}`;

    this.prevPageBtn.disabled = !data.hasPrevious;
    this.nextPageBtn.disabled = !data.hasNext;

    // Hide pagination if only one page or no logs
    this.pagination.style.display = totalPages <= 1 ? "none" : "flex";
  }

  navigateToPage(page) {
    this.applyFilters();
  }

  showLoading(show) {
    this.isLoading = show;
    this.loading.classList.toggle("active", show);
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
