class LogViewer {
  constructor() {
    this.currentPage = 1;
    this.pageSize = 50;
    this.searchQuery = "";
    this.totalLogs = 0;
    this.isLoading = false;

    this.initElements();
    this.initEventListeners();
    this.loadLogs(1);
  }

  initElements() {
    this.logContainer = document.getElementById("logContainer");
    this.searchEl = document.getElementById("search");
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
    this.searchEl.addEventListener("input", () => {
      clearTimeout(this.searchTimeout);
      this.searchTimeout = setTimeout(() => {
        this.performSearch();
      }, 500); // Debounce search
    });

    // Enter key for immediate search
    this.searchEl.addEventListener("keypress", (e) => {
      if (e.key === "Enter") {
        clearTimeout(this.searchTimeout);
        this.performSearch();
      }
    });

    // Search clear functionality (removed since no clear button in HTML)

    // Refresh button
    document.getElementById("refreshBtn").addEventListener("click", () => {
      if (this.searchQuery) {
        this.performSearch();
      } else {
        this.loadLogs(1);
      }
    });

    // Buffer size change (affects page size)
    this.bufferSizeSelect.addEventListener("change", (e) => {
      this.pageSize = parseInt(e.target.value);
      this.loadLogs(1);
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

    // Auto-focus search
    this.searchEl.focus();
  }

  performSearch() {
    const query = this.searchEl.value.trim();
    this.searchQuery = query;

    // Reset to page 1 for new search
    this.currentPage = 1;

    if (query) {
      this.searchLogs(query, 1);
    } else {
      this.loadLogs(1);
    }
  }

  async loadLogs(page) {
    if (this.isLoading) return;

    this.currentPage = page;
    this.showLoading(true);
    this.connectionStatus.textContent = "🔄 Yükleniyor...";

    try {
      const response = await fetch(
        `/api/logs?page=${page}&perPage=${this.pageSize}`
      );
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Failed to load logs");
      }

      this.displayLogs(data);
      this.connectionStatus.textContent = "🔗 Bağlı";
      this.lastUpdate.textContent = `Son güncelleme: ${new Date().toLocaleTimeString()}`;
    } catch (error) {
      console.error("Failed to load logs:", error);
      this.showError("Loglar yüklenirken hata: " + error.message);
      this.connectionStatus.textContent = "❌ Bağlantı Hatası";
    } finally {
      this.showLoading(false);
    }
  }

  async searchLogs(query, page) {
    if (this.isLoading) return;

    this.currentPage = page;
    this.showLoading(true);
    this.connectionStatus.textContent = "🔍 Aranıyor...";

    try {
      const response = await fetch(
        `/api/search?q=${encodeURIComponent(query)}&page=${page}&perPage=${
          this.pageSize
        }`
      );
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Failed to search logs");
      }

      this.displayLogs(data);
      this.connectionStatus.textContent = "🔗 Bağlı";
      this.lastUpdate.textContent = `Son güncelleme: ${new Date().toLocaleTimeString()}`;
    } catch (error) {
      console.error("Failed to search logs:", error);
      this.showError("Arama sırasında hata: " + error.message);
      this.connectionStatus.textContent = "❌ Arama Hatası";
    } finally {
      this.showLoading(false);
    }
  }

  displayLogs(data) {
    this.totalLogs = data.total || 0;
    this.logContainer.innerHTML = "";

    if (!data.logs || data.logs.length === 0) {
      this.logContainer.innerHTML =
        '<tr><td colspan="7" class="status">Log bulunamadı</td></tr>';
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

    // Generate unique ID for collapsible details
    const detailId = `detail_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;

    // Check if we have additional structured data
    const hasStructuredData = log.trace_id || log.process || log.component || log.thread || log.user_id || log.request_id || (log.tags && log.tags.length > 0);

    tr.innerHTML = `
      <td class="timestamp">${timestamp}</td>
      <td>
        <span class="log-level log-level-${levelLower}">${
      log.level || "INFO"
    }</span>
      </td>
      <td class="message">${this.escapeHtml(log.message || "")}</td>
      <td class="source">${this.escapeHtml(log.source || "unknown")}</td>
      <td class="process">${this.escapeHtml(log.process || "-")}</td>
      <td class="trace-id">
        ${log.trace_id ? `<code class="trace-id-code">${this.escapeHtml(log.trace_id)}</code>` : "-"}
      </td>
      <td class="details-toggle">
        ${hasStructuredData ? `<button onclick="toggleDetails('${detailId}')" class="details-btn">📋</button>` : "-"}
      </td>
    `;

    // Add structured data details row if available
    if (hasStructuredData) {
      const detailsRow = document.createElement("tr");
      detailsRow.id = detailId;
      detailsRow.className = "details-row hidden";
      
      const structuredFields = [];
      if (log.component) structuredFields.push(`<strong>Bileşen:</strong> ${this.escapeHtml(log.component)}`);
      if (log.thread) structuredFields.push(`<strong>Thread:</strong> ${this.escapeHtml(log.thread)}`);
      if (log.user_id) structuredFields.push(`<strong>Kullanıcı ID:</strong> ${this.escapeHtml(log.user_id)}`);
      if (log.request_id) structuredFields.push(`<strong>İstek ID:</strong> <code>${this.escapeHtml(log.request_id)}</code>`);
      if (log.tags && log.tags.length > 0) {
        const tagsHtml = log.tags.map(tag => `<span class="tag">${this.escapeHtml(tag)}</span>`).join(' ');
        structuredFields.push(`<strong>Etiketler:</strong> ${tagsHtml}`);
      }
      
      detailsRow.innerHTML = `
        <td colspan="7" class="details-content">
          <div class="structured-data">
            ${structuredFields.join('<br>')}
          </div>
        </td>
      `;
      
      // Insert details row after the main row
      tr.after(detailsRow);
    }

    return tr;
  }

  updatePaginationInfo(data) {
    const totalPages = Math.ceil(data.total / this.pageSize);

    this.logCount.textContent = `${data.total || 0} log`;
    this.pageInfo.textContent = `Sayfa ${data.page || 1} / ${Math.max(
      1,
      totalPages
    )}`;

    this.prevPageBtn.disabled = !data.hasPrevious;
    this.nextPageBtn.disabled = !data.hasNext;

    // Hide pagination if only one page or no logs
    this.pagination.style.display = totalPages <= 1 ? "none" : "flex";
  }

  navigateToPage(page) {
    if (this.searchQuery) {
      this.searchLogs(this.searchQuery, page);
    } else {
      this.loadLogs(page);
    }
  }

  showLoading(show) {
    this.isLoading = show;
    this.loading.classList.toggle("active", show);
  }

  showError(message) {
    this.logContainer.innerHTML = `
      <tr>
        <td colspan="4" class="error">
          <div>⚠️ Hata</div>
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
    detailsRow.classList.toggle('hidden');
  }
}

// Initialize the log viewer
document.addEventListener("DOMContentLoaded", () => {
  window.logViewer = new LogViewer();
});
