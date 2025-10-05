package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"logspot/internal/db"
	"net/http"
	"strconv"

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
	
	// API endpoint for logs with pagination
	http.HandleFunc("/api/logs", s.serveLogsAPI)
	
	// API endpoint for search
	http.HandleFunc("/api/search", s.serveSearchAPI)
	
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

// serveLogsAPI handles the /api/logs endpoint with pagination
func (s *Server) serveLogsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	// Parse pagination parameters
	pageStr := r.URL.Query().Get("page")
	perPageStr := r.URL.Query().Get("perPage")
	
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	
	perPage, err := strconv.Atoi(perPageStr)
	if err != nil || perPage < 1 || perPage > 1000 {
		perPage = 50
	}
	
	// Get total count first
	total, err := db.CountLogs("*")
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	
	// Calculate offset for pagination
	offset := (page - 1) * perPage
	
	// Get logs with proper pagination
	logs, err := db.SearchLogsWithPagination("*", perPage, offset)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	
	// Calculate pagination flags
	hasNext := offset+perPage < total
	hasPrevious := page > 1
	
	response := LogsResponse{
		Logs:        logs,
		Total:       total,
		Page:        page,
		PerPage:     perPage,
		HasNext:     hasNext,
		HasPrevious: hasPrevious,
	}
	
	json.NewEncoder(w).Encode(response)
}

// serveSearchAPI handles the /api/search endpoint
func (s *Server) serveSearchAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	query := r.URL.Query().Get("q")
	if query == "" {
		query = "*"
	}
	
	// Parse pagination parameters
	pageStr := r.URL.Query().Get("page")
	perPageStr := r.URL.Query().Get("perPage")
	
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	
	perPage, err := strconv.Atoi(perPageStr)
	if err != nil || perPage < 1 || perPage > 1000 {
		perPage = 50
	}
	
	// Get total count for search query
	total, err := db.CountLogs(query)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	
	// Calculate offset for pagination
	offset := (page - 1) * perPage
	
	// Get search results with proper pagination
	logs, err := db.SearchLogsWithPagination(query, perPage, offset)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	
	// Calculate pagination flags
	hasNext := offset+perPage < total
	hasPrevious := page > 1
	
	response := LogsResponse{
		Logs:        logs,
		Total:       total,
		Page:        page,
		PerPage:     perPage,
		HasNext:     hasNext,
		HasPrevious: hasPrevious,
	}
	
	json.NewEncoder(w).Encode(response)
}
