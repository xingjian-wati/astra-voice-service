package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/golang-jwt/jwt/v4"
	"go.uber.org/zap"
)

// LoggingMiddleware logs HTTP requests for API endpoints
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		logger.Base().Info("api request",
			zap.String("method", r.Method),
			zap.String("path", r.RequestURI),
			zap.String("remote_addr", r.RemoteAddr),
			zap.Int("status", wrapped.statusCode),
			zap.Duration("latency", time.Since(start)),
		)
	})
}

// ValidationMiddleware validates common request parameters
func ValidationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add common validation logic here if needed
		// For example, check Content-Type for POST/PUT requests
		if r.Method == "POST" || r.Method == "PUT" {
			contentType := r.Header.Get("Content-Type")
			if contentType != "" && contentType != "application/json" {
				http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// CORSMiddleware adds CORS headers to all requests
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Hub-Signature-256")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// GlobalLoggingMiddleware logs all HTTP requests (not just API)
func GlobalLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		logger.Base().Info("http request",
			zap.String("method", r.Method),
			zap.String("path", r.RequestURI),
			zap.String("remote_addr", r.RemoteAddr),
			zap.Int("status", wrapped.statusCode),
			zap.Duration("latency", time.Since(start)),
		)
	})
}

// APIKeyMiddleware validates key from X-API-Key header for API endpoints
func APIKeyMiddleware(secretKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip validation if no secret key is configured (for development)
			if secretKey == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Get JWT token from header
			jwtToken := r.Header.Get("X-API-Key")
			if jwtToken == "" {
				// For browser requests (HTML), missing key is expected on first visit (shows login page)
				if isHTMLRequest(r) {
					logger.Base().Debug("initial page access without key, showing login page",
						zap.String("path", r.URL.Path),
						zap.String("remote_addr", r.RemoteAddr))
				} else {
					// For API requests, missing key is a warning
					logger.Base().Warn("missing api key for api request",
						zap.String("path", r.URL.Path),
						zap.String("remote_addr", r.RemoteAddr))
				}
				sendUnauthorizedResponse(w, r, "missing key", "")
				return
			}

			// Parse and validate JWT token
			token, claims, err := parseAndValidateJWT(jwtToken, secretKey)
			if err != nil || !token.Valid {
				logger.Base().Warn("invalid api key",
					zap.String("remote_addr", r.RemoteAddr),
					zap.Error(err),
				)
				sendUnauthorizedResponse(w, r, "invalid key", "Invalid key format. Please provide a valid JWT token.")
				return
			}

			// Validate claims format
			if claims == nil {
				logger.Base().Warn("invalid token claims",
					zap.String("remote_addr", r.RemoteAddr),
				)
				sendUnauthorizedResponse(w, r, "invalid key", "")
				return
			}

			// Validate credentials
			if !validateCredentials(claims, r.RemoteAddr) {
				sendUnauthorizedResponse(w, r, "invalid key", "Invalid key credentials. Please generate a new key using the generate_jwt tool.")
				return
			}

			// Token is valid, proceed
			logger.Base().Info("api key validated", zap.String("remote_addr", r.RemoteAddr))
			next.ServeHTTP(w, r)
		})
	}
}

// isHTMLRequest checks if the request accepts HTML content
func isHTMLRequest(r *http.Request) bool {
	acceptHeader := r.Header.Get("Accept")
	return acceptHeader == "" || strings.Contains(acceptHeader, "text/html")
}

// sendUnauthorizedResponse sends an appropriate unauthorized response based on request type
func sendUnauthorizedResponse(w http.ResponseWriter, r *http.Request, jsonError, htmlFallbackMsg string) {
	if isHTMLRequest(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)

		htmlContent := readKeyInputPage()
		if htmlContent != "" {
			w.Write([]byte(htmlContent))
			return
		}

		// Fallback HTML if file read fails
		fallbackMsg := "Please provide API key in X-API-Key header"
		if htmlFallbackMsg != "" {
			fallbackMsg = htmlFallbackMsg
		}
		w.Write([]byte(fmt.Sprintf(`<html><body><h1>Key Verification Required</h1><p>%s</p></body></html>`, fallbackMsg)))
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(fmt.Sprintf(`{"error": "%s"}`, jsonError)))
	}
}

// parseAndValidateJWT parses and validates a JWT token
func parseAndValidateJWT(jwtToken, secretKey string) (*jwt.Token, jwt.MapClaims, error) {
	token, err := jwt.Parse(jwtToken, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		// Validate algorithm
		if alg, ok := token.Header["alg"].(string); !ok || alg != "HS256" {
			return nil, jwt.ErrSignatureInvalid
		}
		// Validate type
		if typ, ok := token.Header["typ"].(string); !ok || typ != "JWT" {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secretKey), nil
	})

	if err != nil || !token.Valid {
		return token, nil, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return token, nil, fmt.Errorf("invalid token claims format")
	}

	return token, claims, nil
}

// validateCredentials validates the name and password in JWT claims
func validateCredentials(claims jwt.MapClaims, remoteAddr string) bool {
	name, nameOk := claims["name"].(string)
	password, passwordOk := claims["password"].(string)

	if !nameOk || name != "byoa" || !passwordOk || password != "astra" {
		logger.Base().Warn("invalid credentials in api key",
			zap.String("remote_addr", remoteAddr),
			zap.String("name", fmt.Sprintf("%v", name)),
			zap.String("password", fmt.Sprintf("%v", password)),
		)
		return false
	}

	return true
}

// readKeyInputPage reads the key input HTML page from static files
func readKeyInputPage() string {
	// Try multiple possible paths for the static directory
	possiblePaths := []string{
		"static/html/key_input.html",
		"static/html/key_input.html",
		"./static/html/key_input.html",
	}

	for _, path := range possiblePaths {
		// Get absolute path
		absPath, err := filepath.Abs(path)
		if err != nil {
			continue
		}

		// Read file
		content, err := os.ReadFile(absPath)
		if err == nil {
			return string(content)
		}
	}

	logger.Base().Warn("failed to read key_input.html from any path")
	return ""
}
