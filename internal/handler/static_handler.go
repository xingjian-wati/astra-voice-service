package handler

import (
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// StaticHandler handles serving static files with proper caching and security
type StaticHandler struct {
	staticDir string
	maxAge    time.Duration
}

// NewStaticHandler creates a new static file handler
func NewStaticHandler(staticDir string) *StaticHandler {
	return &StaticHandler{
		staticDir: staticDir,
		maxAge:    24 * time.Hour, // Default cache for 24 hours
	}
}

// SetupStaticRoutes configures static file routes (including pages)
func (h *StaticHandler) SetupStaticRoutes(router *mux.Router) {
	// Serve CSS files
	router.PathPrefix("/static/css/").Handler(
		h.createFileHandler("css", "text/css"),
	).Methods("GET")

	// Serve JavaScript files
	router.PathPrefix("/static/js/").Handler(
		h.createFileHandler("js", "application/javascript"),
	).Methods("GET")

	// Serve HTML files
	router.PathPrefix("/static/html/").Handler(
		h.createFileHandler("html", "text/html"),
	).Methods("GET")

	// Serve image files
	router.PathPrefix("/static/images/").Handler(
		h.createFileHandler("images", ""),
	).Methods("GET")

	// Serve font files
	router.PathPrefix("/static/fonts/").Handler(
		h.createFileHandler("fonts", ""),
	).Methods("GET")

	// Management page routes
	router.HandleFunc("/", h.serveManagementDashboard).Methods("GET")
	router.HandleFunc("/dashboard", h.serveManagementDashboard).Methods("GET")
	router.HandleFunc("/tenants", h.serveTenantManagement).Methods("GET")
	router.HandleFunc("/agents", h.serveAgentManagement).Methods("GET")

	// Test client routes
	router.HandleFunc("/test-client", h.serveTestClient).Methods("GET")
	router.HandleFunc("/test-webrtc", h.serveTestWebRTC).Methods("GET")
	router.HandleFunc("/test-livekit", h.serveTestLiveKit).Methods("GET")

	logger.Base().Info("📁 Static file routes registered (including test clients)")
}

// SetupStaticAssetsOnly configures only static asset routes (no page routes)
func (h *StaticHandler) SetupStaticAssetsOnly(router *mux.Router) {
	// Serve CSS files
	router.PathPrefix("/static/css/").Handler(
		h.createFileHandler("css", "text/css"),
	).Methods("GET")

	// Serve JavaScript files
	router.PathPrefix("/static/js/").Handler(
		h.createFileHandler("js", "application/javascript"),
	).Methods("GET")

	// Serve HTML files
	router.PathPrefix("/static/html/").Handler(
		h.createFileHandler("html", "text/html"),
	).Methods("GET")

	// Serve image files
	router.PathPrefix("/static/images/").Handler(
		h.createFileHandler("images", ""),
	).Methods("GET")

	// Serve font files
	router.PathPrefix("/static/fonts/").Handler(
		h.createFileHandler("fonts", ""),
	).Methods("GET")

	logger.Base().Info("📁 Static asset routes registered (no page routes)")
}

// createFileHandler creates a handler for specific file types
func (h *StaticHandler) createFileHandler(subDir, contentType string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security: prevent directory traversal
		if strings.Contains(r.URL.Path, "..") {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		// Extract filename from URL path
		urlPath := r.URL.Path
		relativePath := strings.TrimPrefix(urlPath, "/static/"+subDir+"/")

		// Construct full file path
		filePath := filepath.Join(h.staticDir, subDir, relativePath)

		// Set content type if specified
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}

		// Set caching headers
		h.setCacheHeaders(w, r, filePath)

		// Log the request
		logger.Base().Info("📁 Serving static file: ->", zap.String("filepath", filePath), zap.String("urlpath", urlPath))

		// Serve the file
		http.ServeFile(w, r, filePath)
	})
}

// setCacheHeaders sets appropriate caching headers
func (h *StaticHandler) setCacheHeaders(w http.ResponseWriter, r *http.Request, filePath string) {
	// Generate ETag based on file content hash
	etag := h.generateETag(filePath)
	w.Header().Set("ETag", etag)

	// Check if client has the same version
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// Set cache control headers - 短缓存时间避免 CDN 问题
	w.Header().Set("Cache-Control", "public, max-age=300") // 5分钟
	w.Header().Set("Expires", time.Now().Add(5*time.Minute).Format(http.TimeFormat))

	// 添加 CDN 控制头
	w.Header().Set("Vary", "Accept-Encoding, If-None-Match")
	w.Header().Set("Pragma", "no-cache")
}

// generateETag generates an ETag based on file content hash
func (h *StaticHandler) generateETag(filePath string) string {
	file, err := os.Open(filePath)
	if err != nil {
		// If file doesn't exist, use timestamp
		return fmt.Sprintf(`"%d"`, time.Now().Unix())
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		// If error reading file, use timestamp
		return fmt.Sprintf(`"%d"`, time.Now().Unix())
	}

	return fmt.Sprintf(`"%x"`, hash.Sum(nil))
}

// Management page handlers
func (h *StaticHandler) serveManagementDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	filePath := filepath.Join(h.staticDir, "html", "management_dashboard.html")
	h.setCacheHeaders(w, r, filePath)
	logger.Base().Info("Serving management dashboard", zap.String("url", r.URL.String()), zap.String("path", "static/html/management_dashboard.html"))
	http.ServeFile(w, r, filePath)
}

func (h *StaticHandler) serveTenantManagement(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	filePath := filepath.Join(h.staticDir, "html", "tenant_management.html")
	h.setCacheHeaders(w, r, filePath)
	logger.Base().Info("📁 Serving tenant management: /tenants -> static/html/tenant_management.html")
	http.ServeFile(w, r, filePath)
}

func (h *StaticHandler) serveAgentManagement(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	filePath := filepath.Join(h.staticDir, "html", "agent_management.html")
	h.setCacheHeaders(w, r, filePath)
	logger.Base().Info("📁 Serving agent management: /agents -> static/html/agent_management.html")
	http.ServeFile(w, r, filePath)
}

// Test client handlers
func (h *StaticHandler) serveTestClient(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	filePath := filepath.Join(h.staticDir, "html", "test_client.html")
	h.setCacheHeaders(w, r, filePath)
	logger.Base().Info("📁 Serving test client: /test-client -> static/html/test_client.html")
	http.ServeFile(w, r, filePath)
}

func (h *StaticHandler) serveTestWebRTC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	filePath := filepath.Join(h.staticDir, "html", "test_client_webrtc.html")
	h.setCacheHeaders(w, r, filePath)
	logger.Base().Info("📁 Serving WebRTC test client: /test-webrtc -> static/html/test_client_webrtc.html")
	http.ServeFile(w, r, filePath)
}

func (h *StaticHandler) serveTestLiveKit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	filePath := filepath.Join(h.staticDir, "html", "test_livekit_client.html")
	h.setCacheHeaders(w, r, filePath)
	logger.Base().Info("🎙 Serving LiveKit test client: /test-livekit -> static/html/test_livekit_client.html")
	http.ServeFile(w, r, filePath)
}

// ServeFile serves a specific file with proper headers
func (h *StaticHandler) ServeFile(w http.ResponseWriter, r *http.Request, filename string) {
	// Security check
	if strings.Contains(filename, "..") {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(h.staticDir, filename)

	// Set appropriate content type based on file extension
	ext := filepath.Ext(filename)
	switch ext {
	case ".css":
		w.Header().Set("Content-Type", "text/css")
	case ".js":
		w.Header().Set("Content-Type", "application/javascript")
	case ".html":
		w.Header().Set("Content-Type", "text/html")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".gif":
		w.Header().Set("Content-Type", "image/gif")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".woff", ".woff2":
		w.Header().Set("Content-Type", "font/woff")
	case ".ttf":
		w.Header().Set("Content-Type", "font/ttf")
	}

	h.setCacheHeaders(w, r, filePath)
	logger.Base().Info("📁 Serving file", zap.String("filepath", filePath))
	http.ServeFile(w, r, filePath)
}
