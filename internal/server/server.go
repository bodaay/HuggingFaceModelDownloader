// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

// Package server provides the HTTP server for the web UI and REST API.
package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/bodaay/HuggingFaceModelDownloader/internal/assets"
	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
	"github.com/gorilla/websocket"
)

// Config holds server configuration.
type Config struct {
	Addr               string
	Port               int
	Token              string // HuggingFace token
	ModelsDir          string // Output directory for models (not configurable via API)
	DatasetsDir        string // Output directory for datasets (not configurable via API)
	CacheDir           string // HuggingFace cache directory for v3 mode
	// LocalDir, when set, puts the whole server in flat/local-file mode: every
	// download writes real files into <LocalDir>/<owner>/<repo> instead of the
	// HF cache layout. Set once at startup (serve --local-dir); not changeable
	// per request. Empty = HF cache mode.
	LocalDir string
	Concurrency        int
	MaxActive          int
	MultipartThreshold string // Minimum size for multipart download
	Verify             string // Verification mode: none, size, sha256
	Retries            int    // Number of retry attempts
	AllowedOrigins     []string // CORS origins
	// LinkMode is how HF cache entries refer to downloaded data (see
	// hfdownloader.LinkMode); empty = auto.
	LinkMode string
	// ExportDir enables "Export as real files" in the web UI: exports are
	// written to <ExportDir>/<owner>/<name>. Empty disables exporting from
	// the web, so the API can't be used to write files anywhere else.
	ExportDir          string
	Endpoint           string   // Custom HuggingFace endpoint (e.g., for mirrors)

	// Authentication
	AuthUser string // Basic auth username (empty = no auth)
	AuthPass string // Basic auth password

	// Proxy configuration
	Proxy *hfdownloader.ProxyConfig

	// Version is the build version string (injected via ldflags and passed
	// down from the CLI). Reported by /api/health and the WebSocket init
	// message so the web UI shows the real running version instead of a stamp
	// hardcoded in source. Empty in tests / library use.
	Version string
}

// version returns the build version, or "dev" when unset (tests/library use).
func (s *Server) version() string {
	if s.config.Version != "" {
		return s.config.Version
	}
	return "dev"
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Addr:               "0.0.0.0",
		Port:               8080,
		ModelsDir:          "./Models",
		DatasetsDir:        "./Datasets",
		Concurrency:        8,
		MaxActive:          3,
		MultipartThreshold: "32MiB",
		Verify:             "size",
		Retries:            4,
	}
}

// Server is the HTTP server for hfdownloader.
type Server struct {
	// configMu guards the mutable fields of config (those changeable via
	// POST /api/settings: Token, Concurrency, MaxActive, MultipartThreshold,
	// Verify, Retries, Endpoint, Proxy). Startup-only fields (Addr, Port,
	// dirs, AllowedOrigins, Auth*) are never written after New and are read
	// without the lock.
	configMu   sync.RWMutex
	config     Config
	httpServer *http.Server
	jobs       *JobManager
	wsHub      *WSHub
	upgrader   websocket.Upgrader
}

// New creates a new server with the given configuration.
func New(cfg Config) *Server {
	wsHub := NewWSHub()
	s := &Server{
		config: cfg,
		jobs:   NewJobManager(cfg, wsHub),
		wsHub:  wsHub,
	}
	s.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     s.isAllowedOrigin,
	}
	return s
}

// settingsConfig returns a snapshot of the API-mutable settings under the read
// lock. Callers that need the current Token/Concurrency/Verify/Proxy/etc. use
// this so they never read a field while handleUpdateSettings is writing it.
func (s *Server) settingsConfig() Config {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.config
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

	// Build middleware chain: CORS -> Auth -> Logging -> Handler
	handler := s.corsMiddleware(s.basicAuthMiddleware(s.loggingMiddleware(limitBodyMiddleware(mux))))

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      handler,
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
		// Halt the WebSocket broadcast coalescer so its pending AfterFunc
		// timers don't fire (and leak) after the server has stopped.
		s.jobs.Stop()
	}()

	log.Printf("🚀 Server starting on http://%s", addr)
	log.Printf("   Dashboard: http://localhost:%d", s.config.Port)
	log.Printf("   API:       http://localhost:%d/api", s.config.Port)
	if s.config.AuthUser != "" {
		log.Printf("   Auth:      enabled (user: %s)", s.config.AuthUser)
	}

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
	mux.HandleFunc("POST /api/jobs/{id}/pause", s.handlePauseJob)
	mux.HandleFunc("POST /api/jobs/{id}/resume", s.handleResumeJob)
	mux.HandleFunc("POST /api/jobs/{id}/dismiss", s.handleDismissJob)

	// Settings
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("POST /api/settings", s.handleUpdateSettings)

	// Plan (dry-run)
	mux.HandleFunc("POST /api/plan", s.handlePlan)

	// Smart Analyzer
	mux.HandleFunc("GET /api/analyze/{repo...}", s.handleAnalyze)

	// Cache browser
	mux.HandleFunc("GET /api/cache", s.handleCacheList)
	mux.HandleFunc("GET /api/cache/{repo...}", s.handleCacheInfo)
	mux.HandleFunc("POST /api/cache/rebuild", s.handleCacheRebuild)
	mux.HandleFunc("DELETE /api/cache/{repo...}", s.handleCacheDelete)
	mux.HandleFunc("POST /api/cache/export", s.handleCacheExport)

	// Mirror - Target management
	mux.HandleFunc("GET /api/mirror/targets", s.handleMirrorTargetsList)
	mux.HandleFunc("POST /api/mirror/targets", s.handleMirrorTargetAdd)
	mux.HandleFunc("DELETE /api/mirror/targets/{name}", s.handleMirrorTargetRemove)

	// Mirror - Operations
	mux.HandleFunc("POST /api/mirror/diff", s.handleMirrorDiff)
	mux.HandleFunc("POST /api/mirror/push", s.handleMirrorPush)
	mux.HandleFunc("POST /api/mirror/pull", s.handleMirrorPull)

	// WebSocket
	mux.HandleFunc("GET /api/ws", s.handleWebSocket)
}

// Middleware

// maxRequestBody caps API request bodies. Requests are small JSON documents;
// without a cap a single 200 MB body pushed the server to ~680 MB of RAM.
const maxRequestBody = 1 << 20

func limitBodyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

// corsMiddleware enforces the origin policy (see isAllowedOrigin) for every
// request. Requests from other origins are rejected outright rather than just
// denied CORS headers: without CORS headers the browser hides the response,
// but a cross-site POST or DELETE would still have been executed. Previously,
// with no AllowedOrigins configured, any origin was echoed back as allowed.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		w.Header().Add("Vary", "Origin")

		if origin != "" {
			if !s.isAllowedOrigin(r) {
				writeError(w, http.StatusForbidden, "Cross-origin request blocked",
					fmt.Sprintf("origin %q is not allowed; start the server with --allow-origin %s to permit it", origin, origin))
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// basicAuthMiddleware provides HTTP Basic Authentication.
func (s *Server) basicAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if not configured
		if s.config.AuthUser == "" {
			next.ServeHTTP(w, r)
			return
		}

		user, pass, ok := r.BasicAuth()
		// Constant-time comparison avoids leaking the configured credentials
		// through response-timing differences. Both comparisons always run.
		userOK := subtle.ConstantTimeCompare([]byte(user), []byte(s.config.AuthUser)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(s.config.AuthPass)) == 1
		if !ok || !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="HF Downloader"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

