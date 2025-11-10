package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// AuthMiddleware handles authentication
type AuthMiddleware struct {
	apiKey     string
	authTokens map[string]bool
	tokensMu   sync.RWMutex
	enabled    bool
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(apiKey string, authTokens string) *AuthMiddleware {
	auth := &AuthMiddleware{
		authTokens: make(map[string]bool),
	}
	
	// Set API key if provided
	if apiKey != "" {
		auth.apiKey = apiKey
		auth.enabled = true
	}
	
	// Parse auth tokens if provided
	if authTokens != "" {
		tokens := strings.Split(authTokens, ",")
		for _, token := range tokens {
			token = strings.TrimSpace(token)
			if token != "" {
				auth.authTokens[token] = true
			}
		}
		auth.enabled = true
	}
	
	return auth
}

// IsEnabled returns true if authentication is enabled
func (a *AuthMiddleware) IsEnabled() bool {
	return a.enabled
}

// Authenticate validates the request
func (a *AuthMiddleware) Authenticate(r *http.Request) bool {
	if !a.enabled {
		return true // No auth required
	}
	
	// Check API key in header
	if a.apiKey != "" {
		providedKey := r.Header.Get("X-API-Key")
		if providedKey != "" && subtle.ConstantTimeCompare([]byte(providedKey), []byte(a.apiKey)) == 1 {
			return true
		}
	}
	
	// Check Bearer token
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			token := parts[1]
			a.tokensMu.RLock()
			valid := a.authTokens[token]
			a.tokensMu.RUnlock()
			if valid {
				return true
			}
		}
	}
	
	// Check Basic Auth (username:password or token:)
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "basic" {
			decoded, err := base64.StdEncoding.DecodeString(parts[1])
			if err == nil {
				credentials := strings.SplitN(string(decoded), ":", 2)
				if len(credentials) == 2 {
					// Check if password matches API key
					if a.apiKey != "" && subtle.ConstantTimeCompare([]byte(credentials[1]), []byte(a.apiKey)) == 1 {
						return true
					}
					// Check if password is a valid token
					a.tokensMu.RLock()
					valid := a.authTokens[credentials[1]]
					a.tokensMu.RUnlock()
					if valid {
						return true
					}
				}
			}
		}
	}
	
	return false
}

// Middleware wraps HTTP handlers with authentication
func (a *AuthMiddleware) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.Authenticate(r) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("WWW-Authenticate", `Basic realm="Page Server"`)
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "error",
				"error":  "Authentication required",
			})
			return
		}
		next(w, r)
	}
}

// AddToken adds a new authentication token
func (a *AuthMiddleware) AddToken(token string) {
	a.tokensMu.Lock()
	defer a.tokensMu.Unlock()
	a.authTokens[token] = true
	a.enabled = true
}

// RemoveToken removes an authentication token
func (a *AuthMiddleware) RemoveToken(token string) {
	a.tokensMu.Lock()
	defer a.tokensMu.Unlock()
	delete(a.authTokens, token)
}

