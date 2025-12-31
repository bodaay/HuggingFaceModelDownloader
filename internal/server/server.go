// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

// Package server provides the HTTP server for the web UI and REST API.
package server

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/bodaay/HuggingFaceModelDownloader/internal/assets"
)

// Config holds server configuration.
type Config struct {
	Addr           string
	Port           int
	Token          string // HuggingFace token
	ModelsDir      string // Output directory for models (not configurable via API)
	DatasetsDir    string // Output directory for datasets (not configurable via API)
	Concurrency    int
	MaxActive      int
	AllowedOrigins []string // CORS origins
	Endpoint       string   // Custom HuggingFace endpoint (e.g., for mirrors)
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Addr:        "0.0.0.0",
		Port:        8080,
		ModelsDir:   "./Models",
		DatasetsDir: "./Datasets",
		Concurrency: 8,
		MaxActive:   3,
	}
}

// Server is the HTTP server for hfdownloader.
type Server struct {
	config     Config
	httpServer *http.Server
	jobs       *JobManager
	wsHub      *WSHub
}

// New creates a new server with the given configuration.
func New(cfg Config) *Server {
	wsHub := NewWSHub()
	s := &Server{
		config: cfg,
		jobs:   NewJobManager(cfg, wsHub),
		wsHub:  wsHub,
	}
	return s
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(ctx context.Context) error {
	// Start WebSocket hub
	go s.wsHub.Run()

	mux := http.NewServeMux()

	// API routes
	s.registerAPIRoutes(mux)

	// Static files (embedded)
	staticFS := assets.StaticFS()
	fileServer := http.FileServer(http.FS(staticFS))

	// Serve index.html for SPA routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Check if file exists
		if f, err := staticFS.(fs.ReadFileFS).ReadFile(path[1:]); err == nil {
			// Serve with correct content type
			contentType := "text/html; charset=utf-8"
			switch {
			case len(path) > 4 && path[len(path)-4:] == ".css":
				contentType = "text/css; charset=utf-8"
			case len(path) > 3 && path[len(path)-3:] == ".js":
				contentType = "application/javascript; charset=utf-8"
			case len(path) > 5 && path[len(path)-5:] == ".json":
				contentType = "application/json; charset=utf-8"
			case len(path) > 4 && path[len(path)-4:] == ".svg":
				contentType = "image/svg+xml"
			}
			w.Header().Set("Content-Type", contentType)
			w.Write(f)
			return
		}

		// Fallback to index.html for SPA routing
		fileServer.ServeHTTP(w, r)
	})

	addr := fmt.Sprintf("%s:%d", s.config.Addr, s.config.Port)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s.corsMiddleware(s.loggingMiddleware(mux)),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("🚀 Server starting on http://%s", addr)
	log.Printf("   Dashboard: http://localhost:%d", s.config.Port)
	log.Printf("   API:       http://localhost:%d/api", s.config.Port)

	err := s.httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// registerAPIRoutes sets up all API endpoints.
func (s *Server) registerAPIRoutes(mux *http.ServeMux) {
	// Health check
	mux.HandleFunc("GET /api/health", s.handleHealth)

	// Downloads
	mux.HandleFunc("POST /api/download", s.handleStartDownload)
	mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	mux.HandleFunc("GET /api/jobs/{id}", s.handleGetJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.handleCancelJob)

	// Settings
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("POST /api/settings", s.handleUpdateSettings)

	// Plan (dry-run)
	mux.HandleFunc("POST /api/plan", s.handlePlan)

	// WebSocket
	mux.HandleFunc("GET /api/ws", s.handleWebSocket)
}

// Middleware

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		
		// Allow same-origin and configured origins
		if origin != "" {
			allowed := false
			if len(s.config.AllowedOrigins) == 0 {
				// Default: allow same host
				allowed = true
			} else {
				for _, o := range s.config.AllowedOrigins {
					if o == "*" || o == origin {
						allowed = true
						break
					}
				}
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

