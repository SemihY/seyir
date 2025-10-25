package main

import (
	"fmt"
	"math"
	"seyir/internal/db"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Key bindings
type keyMap struct {
	Up          key.Binding
	Down        key.Binding  
	PageUp      key.Binding
	PageDown    key.Binding
	FirstPage   key.Binding
	LastPage    key.Binding
	Enter       key.Binding
	Back        key.Binding
	Search      key.Binding
	ClearSearch key.Binding
	Refresh     key.Binding
	AllLogs     key.Binding
	Errors      key.Binding
	Warnings    key.Binding
	Traces      key.Binding
	Help        key.Binding
	Quit        key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup", "ctrl+u"),
		key.WithHelp("pgup/ctrl+u", "page up"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdown", "ctrl+d"),
		key.WithHelp("pgdn/ctrl+d", "page down"),
	),
	FirstPage: key.NewBinding(
		key.WithKeys("home", "g"),
		key.WithHelp("home/g", "first page"),
	),
	LastPage: key.NewBinding(
		key.WithKeys("end", "G"),
		key.WithHelp("end/G", "last page"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter", " "),
		key.WithHelp("enter/space", "view message"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Search: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "search"),
	),
	ClearSearch: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "clear search"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	AllLogs: key.NewBinding(
		key.WithKeys("1"),
		key.WithHelp("1", "all logs"),
	),
	Errors: key.NewBinding(
		key.WithKeys("2"),
		key.WithHelp("2", "errors only"),
	),
	Warnings: key.NewBinding(
		key.WithKeys("3"),
		key.WithHelp("3", "warnings"),
	),
	Traces: key.NewBinding(
		key.WithKeys("4"),
		key.WithHelp("4", "with traces"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	),
}

// ViewMode represents different view filters
type ViewMode int

const (
	ViewAll ViewMode = iota
	ViewErrors
	ViewWarnings
	ViewTraces
)

func (v ViewMode) String() string {
	switch v {
	case ViewAll:
		return "All Logs"
	case ViewErrors:
		return "Errors Only"
	case ViewWarnings:
		return "Warnings"
	case ViewTraces:
		return "With Traces"
	default:
		return "Unknown"
	}
}

// UIState represents different UI states
type UIState int

const (
	StateList UIState = iota
	StateDetail
	StateSearch
)

// TUIModel represents the state of the TUI
type TUIModel struct {
	session          *Session
	logs             []*db.LogEntry
	filteredLogs     []*db.LogEntry
	table            table.Model
	textInput        textinput.Model
	viewMode         ViewMode
	uiState          UIState
	loading          bool
	lastUpdate       time.Time
	error            error
	showHelp         bool
	width            int
	height           int
	// Pagination
	currentPage      int
	itemsPerPage     int
	totalPages       int
	// Search/Filter
	searchQuery      string
	searchMode       bool
	searchResults    []*db.LogEntry
	// Detail view
	selectedMessage  *db.LogEntry
	detailScroll     int
}

// Message types for the TUI
type tickMsg time.Time
type logsRefreshedMsg struct {
	logs []*db.LogEntry
	err  error
}

// Styles
var (
	// Primary colors
	primaryColor     = lipgloss.Color("#7C3AED")  // Purple
	secondaryColor   = lipgloss.Color("#10B981")  // Green  
	accentColor      = lipgloss.Color("#F59E0B")  // Amber
	errorColor       = lipgloss.Color("#EF4444")  // Red
	warningColor     = lipgloss.Color("#F97316")  // Orange
	mutedColor       = lipgloss.Color("#6B7280")  // Gray
	brightColor      = lipgloss.Color("#F3F4F6")  // Light gray
	
	// Styles
	headerStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Background(lipgloss.Color("#1F2937")).
		Padding(0, 1).
		MarginBottom(1)

	sessionInfoStyle = lipgloss.NewStyle().
		Foreground(secondaryColor).
		MarginBottom(1)

	statusStyle = lipgloss.NewStyle().
		Foreground(mutedColor).
		Background(lipgloss.Color("#111827")).
		Padding(0, 1).
		MarginBottom(1)

	errorStyle = lipgloss.NewStyle().
		Foreground(errorColor).
		Bold(true)

	helpStyle = lipgloss.NewStyle().
		Foreground(mutedColor).
		Background(lipgloss.Color("#1F2937")).
		Padding(0, 1).
		MarginTop(1)

	selectedRowStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(brightColor).
		Background(primaryColor)
)

// InitialTUIModel creates the initial TUI model
func InitialTUIModel(session *Session) TUIModel {
	columns := []table.Column{
		{Title: "Time", Width: 10},
		{Title: "Level", Width: 6},
		{Title: "Process", Width: 12},
		{Title: "Source", Width: 15},
		{Title: "Trace", Width: 10},
		{Title: "Message", Width: 50},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(20),
	)

	// Modern table styling inspired by lazygit
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		BorderBottom(true).
		Bold(true).
		Foreground(brightColor).
		Background(lipgloss.Color("#1F2937"))
	s.Selected = s.Selected.
		Foreground(brightColor).
		Background(primaryColor).
		Bold(true)
	s.Cell = s.Cell.
		Foreground(lipgloss.Color("#E5E7EB"))
	t.SetStyles(s)

	// Initialize text input for search
	ti := textinput.New()
	ti.Placeholder = "Search logs..."
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 50

	return TUIModel{
		session:      session,
		table:        t,
		textInput:    ti,
		viewMode:     ViewAll,
		uiState:      StateList,
		lastUpdate:   time.Now(),
		width:        120,
		height:       30,
		itemsPerPage: 20,
		currentPage:  0,
	}
}

// Init initializes the TUI
func (m TUIModel) Init() tea.Cmd {
	return tea.Batch(
		m.refreshLogs(),
		tea.Tick(time.Second*3, func(t time.Time) tea.Msg {
			return tickMsg(t)
		}),
	)
}

// Update handles messages and updates the model
func (m TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.itemsPerPage = max(1, m.height-10) // Adjust items per page based on height
		m.updateTableSize()
		m.updatePagination()

	case tea.KeyMsg:
		// Handle different UI states
		switch m.uiState {
		case StateSearch:
			return m.handleSearchInput(msg)
		case StateDetail:
			return m.handleDetailView(msg)
		default:
			return m.handleListView(msg)
		}

	case tickMsg:
		// Auto-refresh every 3 seconds (only in list view)
		if m.uiState == StateList {
			return m, tea.Batch(
				m.refreshLogs(),
				tea.Tick(time.Second*3, func(t time.Time) tea.Msg {
					return tickMsg(t)
				}),
			)
		}
		return m, tea.Tick(time.Second*3, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})

	case logsRefreshedMsg:
		m.loading = false
		m.error = msg.err
		if msg.err == nil {
			m.logs = msg.logs
			m.applySearch()
			m.updatePagination()
			m.updateTable()
			m.lastUpdate = time.Now()
		}
	}

	// Update table only in list view
	if m.uiState == StateList {
		switch msg.(type) {
		case tea.KeyMsg:
			// Skip key messages - we handle them manually
		default:
			m.table, cmd = m.table.Update(msg)
		}
	}
	
	return m, cmd
}

// handleListView handles key input in the main list view
func (m TUIModel) handleListView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp

	case key.Matches(msg, keys.Search):
		m.uiState = StateSearch
		m.textInput.Focus()
		m.textInput.SetValue("")

	case key.Matches(msg, keys.Enter):
		if len(m.getCurrentPageLogs()) > 0 {
			selected := m.table.Cursor()
			if selected < len(m.getCurrentPageLogs()) {
				m.selectedMessage = m.getCurrentPageLogs()[selected]
				m.uiState = StateDetail
				m.detailScroll = 0
			}
		}

	case key.Matches(msg, keys.PageUp):
		if m.currentPage > 0 {
			m.currentPage--
			m.updateTable()
		}

	case key.Matches(msg, keys.PageDown):
		if m.currentPage < m.totalPages-1 {
			m.currentPage++
			m.updateTable()
		}

	case key.Matches(msg, keys.FirstPage):
		m.currentPage = 0
		m.updateTable()

	case key.Matches(msg, keys.LastPage):
		if m.totalPages > 0 {
			m.currentPage = m.totalPages - 1
			m.updateTable()
		}

	case key.Matches(msg, keys.Refresh):
		m.loading = true
		return m, m.refreshLogs()

	case key.Matches(msg, keys.AllLogs):
		m.viewMode = ViewAll
		m.currentPage = 0
		m.loading = true
		return m, m.refreshLogs()

	case key.Matches(msg, keys.Errors):
		m.viewMode = ViewErrors
		m.currentPage = 0
		m.loading = true
		return m, m.refreshLogs()

	case key.Matches(msg, keys.Warnings):
		m.viewMode = ViewWarnings
		m.currentPage = 0
		m.loading = true
		return m, m.refreshLogs()

	case key.Matches(msg, keys.Traces):
		m.viewMode = ViewTraces
		m.currentPage = 0
		m.loading = true
		return m, m.refreshLogs()
	}

	return m, nil
}

// handleSearchInput handles key input in search mode
func (m TUIModel) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case key.Matches(msg, keys.Back):
		m.uiState = StateList
		m.textInput.Blur()
		m.searchQuery = ""
		m.applySearch()
		m.updatePagination()
		m.updateTable()

	case key.Matches(msg, keys.ClearSearch):
		m.textInput.SetValue("")
		m.searchQuery = ""
		m.applySearch()
		m.updatePagination()
		m.updateTable()

	case msg.Type == tea.KeyEnter:
		m.uiState = StateList
		m.textInput.Blur()
		m.searchQuery = m.textInput.Value()
		m.currentPage = 0
		m.applySearch()
		m.updatePagination()
		m.updateTable()

	default:
		m.textInput, cmd = m.textInput.Update(msg)
		// Live search as user types
		m.searchQuery = m.textInput.Value()
		m.currentPage = 0
		m.applySearch()
		m.updatePagination()
		m.updateTable()
	}

	return m, cmd
}

// handleDetailView handles key input in detail view
func (m TUIModel) handleDetailView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back):
		m.uiState = StateList
		m.selectedMessage = nil
		m.detailScroll = 0

	case key.Matches(msg, keys.Up):
		if m.detailScroll > 0 {
			m.detailScroll--
		}

	case key.Matches(msg, keys.Down):
		m.detailScroll++

	case key.Matches(msg, keys.PageUp):
		m.detailScroll = max(0, m.detailScroll-10)

	case key.Matches(msg, keys.PageDown):
		m.detailScroll += 10
	}

	return m, nil
}

// refreshLogs fetches logs based on current view mode
func (m TUIModel) refreshLogs() tea.Cmd {
	return func() tea.Msg {
		// Get process names from session
		processNames := m.session.GetProcessNames()
		if len(processNames) == 0 {
			return logsRefreshedMsg{logs: []*db.LogEntry{}, err: nil}
		}

		// Create filter for last 1 hour
		filter, err := db.NewQueryFilter("1h")
		if err != nil {
			return logsRefreshedMsg{err: err}
		}

		var allEntries []*db.LogEntry

		// Query each process in the session
		for _, processName := range processNames {
			filter.ProcessName = processName
			
			// Apply view mode filters
			switch m.viewMode {
			case ViewErrors:
				filter.Levels = []string{"ERROR", "FATAL"}
			case ViewWarnings:
				filter.Levels = []string{"WARN", "WARNING", "ERROR", "FATAL"}
			case ViewTraces:
				// Will filter for traces after query
				filter.Levels = nil
			default:
				filter.Levels = nil
			}

			result, err := db.Query(filter)
			if err != nil {
				continue // Skip this process on error
			}

			// For traces view, filter entries that have trace IDs
			if m.viewMode == ViewTraces {
				for _, entry := range result.Entries {
					if entry.TraceID != "" {
						allEntries = append(allEntries, entry)
					}
				}
			} else {
				allEntries = append(allEntries, result.Entries...)
			}
		}

		// Sort by timestamp (newest first)
		sort.Slice(allEntries, func(i, j int) bool {
			return allEntries[i].Ts.After(allEntries[j].Ts)
		})

		// Limit to 100 most recent entries for performance
		if len(allEntries) > 100 {
			allEntries = allEntries[:100]
		}

		return logsRefreshedMsg{logs: allEntries, err: nil}
	}
}

// updateTable updates the table with current page logs
func (m *TUIModel) updateTable() {
	currentLogs := m.getCurrentPageLogs()
	rows := make([]table.Row, len(currentLogs))

	for i, entry := range currentLogs {
		timeStr := entry.Ts.Format("15:04:05")
		
		// Get process name (fallback to source if process is empty)
		processStr := entry.Process
		if processStr == "" {
			processStr = entry.Source
		}
		if len(processStr) > 12 {
			processStr = processStr[:9] + "..."
		}

		// Format trace ID
		traceStr := ""
		if entry.TraceID != "" {
			traceStr = entry.TraceID
			if len(traceStr) > 10 {
				traceStr = traceStr[:7] + "..."
			}
		}

		// Format source
		sourceStr := entry.Source
		if len(sourceStr) > 15 {
			sourceStr = sourceStr[:12] + "..."
		}

		// Format message
		messageStr := entry.Message
		if len(messageStr) > 50 {
			messageStr = messageStr[:47] + "..."
		}

		rows[i] = table.Row{
			timeStr,
			string(entry.Level),
			processStr,
			sourceStr,
			traceStr,
			messageStr,
		}
	}

	m.table.SetRows(rows)
}

// updateTableSize adjusts table size based on terminal dimensions
func (m *TUIModel) updateTableSize() {
	// Reserve space for header, session info, status, and help
	availableHeight := m.height - 8
	if m.showHelp {
		availableHeight -= 3
	}
	
	if availableHeight < 5 {
		availableHeight = 5
	}

	// Create new table with updated dimensions
	messageWidth := m.width - 53 - 6 // Total fixed width - borders
	if messageWidth < 20 {
		messageWidth = 20
	}

	columns := []table.Column{
		{Title: "Time", Width: 10},
		{Title: "Level", Width: 6},
		{Title: "Process", Width: 12},
		{Title: "Source", Width: 15},
		{Title: "Trace", Width: 10},
		{Title: "Message", Width: messageWidth},
	}

	// Create new table with updated configuration
	newTable := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(availableHeight),
	)
	
	// Apply styles
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	newTable.SetStyles(s)
	
	// Transfer rows if any exist
	if len(m.table.Rows()) > 0 {
		newTable.SetRows(m.table.Rows())
	}
	
	m.table = newTable
}

// View renders the TUI
func (m TUIModel) View() string {
	switch m.uiState {
	case StateDetail:
		return m.detailView()
	case StateSearch:
		return m.searchView()
	default:
		return m.listView()
	}
}

// listView renders the main log list
func (m TUIModel) listView() string {
	var b strings.Builder

	// Header
	title := fmt.Sprintf("🔍 Seyir Session: %s (%s)", m.session.Name, m.session.ID)
	b.WriteString(headerStyle.Render(title))
	b.WriteString("\n")

	// Session info
	stats := m.session.GetStats()
	sessionInfo := fmt.Sprintf("Processes: %d | Total Logs: %d | Errors: %d | Warnings: %d | Duration: %s",
		stats.ProcessCount,
		stats.TotalLogs,
		stats.ErrorCount,
		stats.WarningCount,
		formatDuration(stats.Duration))
	b.WriteString(sessionInfoStyle.Render(sessionInfo))
	b.WriteString("\n")

	// Current view and status with enhanced pagination info
	loadingIndicator := ""
	if m.loading {
		loadingIndicator = " ⟳ Loading..."
	}
	
	searchInfo := ""
	if m.searchQuery != "" {
		searchInfo = fmt.Sprintf(" | 🔍 \"%s\"", m.searchQuery)
	}
	
	// Enhanced pagination info with cursor position
	pageInfo := ""
	cursor := m.table.Cursor()
	currentLogs := m.getCurrentPageLogs()
	if m.totalPages > 1 {
		totalFiltered := len(m.filteredLogs)
		globalPos := m.currentPage*m.itemsPerPage + cursor + 1
		pageInfo = fmt.Sprintf(" | Page %d/%d (%d/%d entries)", 
			m.currentPage+1, m.totalPages, globalPos, totalFiltered)
	} else if len(currentLogs) > 0 {
		pageInfo = fmt.Sprintf(" | Entry %d/%d", cursor+1, len(currentLogs))
	}
	
	status := fmt.Sprintf("View: %s | Total: %d | Filtered: %d%s%s | Updated: %s%s",
		m.viewMode.String(),
		len(m.logs),
		len(m.filteredLogs),
		searchInfo,
		pageInfo,
		m.lastUpdate.Format("15:04:05"),
		loadingIndicator)
	b.WriteString(statusStyle.Render(status))
	b.WriteString("\n")

	// Error display
	if m.error != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("❌ Error: %v", m.error)))
		b.WriteString("\n")
	}

	// Main table
	b.WriteString(m.table.View())
	b.WriteString("\n")

	// Lazygit-style bottom status bar
	if m.showHelp {
		help := m.helpView()
		b.WriteString(helpStyle.Render(help))
	} else {
		// Build context-sensitive shortcuts
		shortcuts := []string{}
		
		// Navigation shortcuts
		if len(currentLogs) > 0 {
			shortcuts = append(shortcuts, "↑↓/jk:navigate")
			shortcuts = append(shortcuts, "enter:details")
		}
		
		// Pagination shortcuts  
		if m.totalPages > 1 {
			shortcuts = append(shortcuts, "pgup/pgdn:pages")
			shortcuts = append(shortcuts, "g/G:first/last")
		}
		
		// Filter shortcuts
		shortcuts = append(shortcuts, "1-4:filters")
		shortcuts = append(shortcuts, "/:search")
		shortcuts = append(shortcuts, "r:refresh")
		shortcuts = append(shortcuts, "?:help")
		shortcuts = append(shortcuts, "q:quit")
		
		bottomBar := fmt.Sprintf("│ %s │", strings.Join(shortcuts, " │ "))
		b.WriteString(helpStyle.Render(bottomBar))
	}

	return b.String()
}

// searchView renders the search input interface
func (m TUIModel) searchView() string {
	var b strings.Builder

	// Header
	title := "🔍 Search Logs"
	b.WriteString(headerStyle.Render(title))
	b.WriteString("\n\n")

	// Search input
	b.WriteString("Search query (press Enter to search, Esc to cancel):\n")
	b.WriteString(m.textInput.View())
	b.WriteString("\n\n")

	// Show live results count if there's a query
	if m.searchQuery != "" {
		resultInfo := fmt.Sprintf("Found %d matching entries", len(m.filteredLogs))
		b.WriteString(statusStyle.Render(resultInfo))
		b.WriteString("\n\n")
		
		// Show preview of first few results
		previewLogs := m.filteredLogs
		if len(previewLogs) > 5 {
			previewLogs = previewLogs[:5]
		}
		
		b.WriteString("Preview:\n")
		for _, log := range previewLogs {
			preview := fmt.Sprintf("%s [%s] %s", 
				log.Ts.Format("15:04:05"),
				log.Level,
				log.Message)
			if len(preview) > 80 {
				preview = preview[:77] + "..."
			}
			b.WriteString(fmt.Sprintf("  %s\n", preview))
		}
	}

	return b.String()
}

// detailView renders the detailed message view
func (m TUIModel) detailView() string {
	if m.selectedMessage == nil {
		return "No message selected"
	}

	var b strings.Builder
	msg := m.selectedMessage

	// Header
	title := fmt.Sprintf("📄 Message Detail - %s", msg.Ts.Format("2006-01-02 15:04:05"))
	b.WriteString(headerStyle.Render(title))
	b.WriteString("\n\n")

	// Message metadata
	metadata := fmt.Sprintf("Level: %s | Process: %s | Source: %s",
		msg.Level, msg.Process, msg.Source)
	if msg.TraceID != "" {
		metadata += fmt.Sprintf(" | Trace: %s", msg.TraceID)
	}
	b.WriteString(sessionInfoStyle.Render(metadata))
	b.WriteString("\n\n")

	// Full message content with scrolling
	lines := strings.Split(msg.Message, "\n")
	availableHeight := m.height - 10 // Reserve space for header and footer
	
	start := m.detailScroll
	end := min(start+availableHeight, len(lines))
	
	if start >= len(lines) {
		start = max(0, len(lines)-1)
		end = len(lines)
	}
	
	for i := start; i < end; i++ {
		b.WriteString(lines[i])
		b.WriteString("\n")
	}

	// Scroll indicator
	if len(lines) > availableHeight {
		scrollInfo := fmt.Sprintf("Line %d-%d of %d", start+1, end, len(lines))
		b.WriteString("\n")
		b.WriteString(statusStyle.Render(scrollInfo))
	}

	// Help
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("[↑↓] Scroll [PgUp/PgDn] Page [Esc] Back"))

	return b.String()
}

// helpView returns the detailed help text
func (m TUIModel) helpView() string {
	helpSections := []string{
		"┌─ Navigation ─────────────────────────────────────────────────────────────┐",
		"│ ↑/↓, j/k          Move cursor up/down                                   │",
		"│ PgUp/PgDn         Navigate pages (Ctrl+U/Ctrl+D)                       │", 
		"│ Home/g            Jump to first page                                    │",
		"│ End/G             Jump to last page                                     │",
		"│ Enter/Space       View full log message details                        │",
		"│ Esc/Backspace     Return to previous view                              │",
		"└─────────────────────────────────────────────────────────────────────────┘",
		"",
		"┌─ Filtering & Search ─────────────────────────────────────────────────────┐",
		"│ 1, a              Show all logs                                         │",
		"│ 2, e              Show errors only (ERROR, FATAL)                      │",
		"│ 3, w              Show warnings and errors                             │", 
		"│ 4, t              Show logs with trace IDs                             │",
		"│ /, Ctrl+F         Open search/filter interface                         │",
		"│ Ctrl+L            Clear current search                                 │",
		"└─────────────────────────────────────────────────────────────────────────┘",
		"",
		"┌─ Actions ────────────────────────────────────────────────────────────────┐",
		"│ r, F5, Ctrl+R    Manual refresh (auto-refresh: 3s)                     │",
		"│ ?, h, F1          Toggle this help panel                               │",
		"│ q, Ctrl+C         Quit application                                     │",
		"└─────────────────────────────────────────────────────────────────────────┘",
		"",
		"┌─ Features ───────────────────────────────────────────────────────────────┐",
		"│ • Real-time log monitoring with auto-refresh every 3 seconds           │",
		"│ • Efficient pagination for large log datasets                          │", 
		"│ • Live search with instant filtering across all log fields             │",
		"│ • Full message view with scrollable content                            │",
		"│ • Session-based multi-process log aggregation                          │",
		"│ • Time range: Last 1 hour | Storage: Parquet + DuckDB                 │",
		"└─────────────────────────────────────────────────────────────────────────┘",
	}
	
	return strings.Join(helpSections, "\n")
}

// Helper functions for pagination and search

// applySearch filters logs based on the current search query
func (m *TUIModel) applySearch() {
	if m.searchQuery == "" {
		m.filteredLogs = m.logs
		return
	}

	query := strings.ToLower(m.searchQuery)
	var filtered []*db.LogEntry
	
	for _, log := range m.logs {
		if strings.Contains(strings.ToLower(log.Message), query) ||
			strings.Contains(strings.ToLower(log.Source), query) ||
			strings.Contains(strings.ToLower(log.Process), query) ||
			strings.Contains(strings.ToLower(string(log.Level)), query) {
			filtered = append(filtered, log)
		}
	}
	
	m.filteredLogs = filtered
}

// updatePagination calculates pagination values
func (m *TUIModel) updatePagination() {
	if len(m.filteredLogs) == 0 {
		m.totalPages = 1
		m.currentPage = 0
		return
	}
	
	m.totalPages = int(math.Ceil(float64(len(m.filteredLogs)) / float64(m.itemsPerPage)))
	if m.currentPage >= m.totalPages {
		m.currentPage = max(0, m.totalPages-1)
	}
}

// getCurrentPageLogs returns the logs for the current page
func (m *TUIModel) getCurrentPageLogs() []*db.LogEntry {
	if len(m.filteredLogs) == 0 {
		return []*db.LogEntry{}
	}
	
	start := m.currentPage * m.itemsPerPage
	end := min(start+m.itemsPerPage, len(m.filteredLogs))
	
	if start >= len(m.filteredLogs) {
		return []*db.LogEntry{}
	}
	
	return m.filteredLogs[start:end]
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// min returns the minimum of two integers  
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

// StartTUI launches the terminal user interface
func StartTUI(session *Session) error {
	model := InitialTUIModel(session)
	p := tea.NewProgram(model, tea.WithAltScreen())
	
	_, err := p.Run()
	return err
}