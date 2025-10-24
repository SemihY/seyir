package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"seyir/internal/db"
	"strconv"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"
)

// LogsResponse represents the JSON response for API endpoints
type LogsResponse struct {
	Logs        []*db.LogEntry `json:"logs"`
	Total       int            `json:"total"`
	Page        int            `json:"page"`
	PerPage     int            `json:"perPage"`
	HasNext     bool           `json:"hasNext"`
	HasPrevious bool           `json:"hasPrevious"`
}


//go:embed ui/*
var uiFiles embed.FS

// Server represents the web server instance
type Server struct {
	port string
}

// New creates a new server instance
func New(port string) *Server {
	return &Server{
		port: port,
	}
}

// Start starts the web server
func (s *Server) Start() error {
	fmt.Printf("Starting web server on port %s\n", s.port)
	fmt.Printf("Access at: http://localhost:%s\n", s.port)
	
	// Serve static files from embedded UI files
	uiFS, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		return fmt.Errorf("failed to create UI filesystem: %v", err)
	}
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(uiFS))))
	
	// Serve main HTML page
	http.HandleFunc("/", s.serveHomePage)
	

	
	// API endpoint for fast query with projection pushdown
	http.HandleFunc("/api/query", s.serveQueryAPI)
	
	// API endpoint for distinct values
	http.HandleFunc("/api/query/distinct", s.serveDistinctAPI)
	
	// API endpoint for system health
	http.HandleFunc("/api/health", s.serveHealthAPI)
	
	// Start server
	return http.ListenAndServe(":"+s.port, nil)
}

// serveHomePage serves the main HTML page from embedded files
func (s *Server) serveHomePage(w http.ResponseWriter, r *http.Request) {
	indexHTML, err := fs.ReadFile(uiFiles, "ui/index.html")
	if err != nil {
		http.Error(w, "Failed to read index.html", http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}





// serveQueryAPI handles the /api/query endpoint with projection pushdown optimization
func (s *Server) serveQueryAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	
	// Parse query parameters
	query := r.URL.Query()
	
	// Build filter from query parameters
	filter := &db.QueryFilter{
		Limit: 100, // Default limit for UI
	}
	
	// Parse source filter
	if sources := query.Get("source"); sources != "" {
		filter.Sources = []string{sources} // Single source for simplicity
	}
	
	// Parse level filter  
	if levels := query.Get("level"); levels != "" {
		filter.Levels = []string{levels} // Single level for simplicity
	}
	
	// Parse trace_id filter
	if traceId := query.Get("trace_id"); traceId != "" {
		filter.TraceIDs = []string{traceId}
	}
	
	// Parse time range - if not provided, default to last 24 hours
	now := time.Now()
	filter.StartTime = now.Add(-24 * time.Hour) // Default: last 24 hours
	filter.EndTime = now
	
	if from := query.Get("from"); from != "" {
		if t, err := parseTimeParam(from); err == nil {
			filter.StartTime = t
		}
	}
	
	if to := query.Get("to"); to != "" {
		if t, err := parseTimeParam(to); err == nil {
			filter.EndTime = t
		}
	}
	
	// Parse process filter
	if process := query.Get("process"); process != "" {
		filter.ProcessName = process
	}
	
	// Parse message search
	if search := query.Get("search"); search != "" {
		filter.MessageSearch = search
	}
	
	// Parse limit
	if limitStr := query.Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 && limit <= 1000 {
			filter.Limit = limit
		}
	}
	
	// Parse offset
	if offsetStr := query.Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			filter.Offset = offset
		}
	}
	
	// Execute query
	result, err := db.Query(filter)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	
	// Stream JSON response
	response := map[string]interface{}{
		"logs":          result.Entries,
		"total":         result.TotalCount,
		"query_time_ms": result.QueryTime.Milliseconds(),
		"files_scanned": result.FilesScanned,
		"limit":         filter.Limit,
		"offset":        filter.Offset,
		"returned":      len(result.Entries),
	}
	
	json.NewEncoder(w).Encode(response)
}

// serveDistinctAPI handles the /api/query/distinct endpoint for getting distinct column values
func (s *Server) serveDistinctAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	
	// Parse query parameters
	query := r.URL.Query()
	
	column := query.Get("column")
	if column == "" {
		http.Error(w, `{"error": "column parameter is required"}`, http.StatusBadRequest)
		return
	}
	
	limitStr := query.Get("limit")
	limit := 50 // default
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}
	
	// Get distinct values
	values, err := db.GetDistinctValues(column, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	
	// Return response
	response := map[string]interface{}{
		"column": column,
		"values": values,
		"count":  len(values),
		"limit":  limit,
	}
	
	json.NewEncoder(w).Encode(response)
}

// parseTimeParam parses time parameter in various formats
func parseTimeParam(timeStr string) (time.Time, error) {
	// Try different time formats
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02",
	}
	
	for _, format := range formats {
		if t, err := time.Parse(format, timeStr); err == nil {
			return t, nil
		}
	}
	
	return time.Time{}, fmt.Errorf("invalid time format: %s", timeStr)
}

// serveHealthAPI handles the /api/health endpoint
func (s *Server) serveHealthAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	// Get session information
	sessions := db.GetActiveSessions()
	
	// Combine health information
	health := map[string]interface{}{
		"status":          "ok",
		"active_sessions": len(sessions),
		"sessions":        sessions,
	}
	
	json.NewEncoder(w).Encode(health)
}
