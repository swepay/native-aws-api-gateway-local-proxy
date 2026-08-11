// Security middleware for AWS API Gateway HTTP API v2 Local Proxy.
//
// Implements F-SEC-10 from Swepay GAPS_ROADMAP.md:
//
//	--max-body-bytes     limits request payload size (default: 6 MiB, aligned with
//	                     API Gateway HTTP API v2 6MB payload limit).
//	--cors-allowed-origins restricts which Origin headers may pass through.
//	--require-auth-header rejects requests without an Authorization header.
//
// Configuration is env-var driven to stay consistent with the existing pattern:
//
//	MAX_BODY_BYTES        (int, bytes; default 6291456)
//	CORS_ALLOWED_ORIGINS  (CSV string; empty = allow all, "*" = explicit allow all)
//	REQUIRE_AUTH_HEADER   ("true"/"false"; default false)
//
// These defaults are intentionally permissive so the local dev loop is not broken;
// production-leaning deployments should tighten them explicitly.

package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// SecurityConfig holds the runtime limits applied to every incoming request.
type SecurityConfig struct {
	// MaxBodyBytes caps the Content-Length of accepted requests. Requests with
	// Content-Length above this return 413 Payload Too Large. Also caps the
	// bytes actually read, defending against chunked requests that lie about
	// their size.
	MaxBodyBytes int64

	// CORSAllowedOrigins is the whitelist of Origin values. An empty slice
	// (default) means "no CORS enforcement" — any Origin passes through.
	// A slice containing exactly "*" means "allow all" and is functionally
	// the same as empty; kept distinct for audit/log clarity.
	CORSAllowedOrigins []string

	// RequireAuthHeader, when true, rejects any request without an
	// Authorization header with 401 Unauthorized. Does NOT validate the
	// header's content — that is upstream Lambda's job.
	RequireAuthHeader bool
}

// Default limits aligned with AWS API Gateway HTTP API v2.
const (
	DefaultMaxBodyBytes int64 = 6 * 1024 * 1024 // 6 MiB
)

// loadSecurityConfig reads security knobs from environment variables.
// Unknown or malformed values fall back to safe defaults with a warning log.
func loadSecurityConfig() SecurityConfig {
	cfg := SecurityConfig{
		MaxBodyBytes:       DefaultMaxBodyBytes,
		CORSAllowedOrigins: nil,
		RequireAuthHeader:  false,
	}

	if v := getEnv("MAX_BODY_BYTES", ""); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			cfg.MaxBodyBytes = n
		}
	}

	if v := getEnv("CORS_ALLOWED_ORIGINS", ""); v != "" {
		for _, o := range strings.Split(v, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				cfg.CORSAllowedOrigins = append(cfg.CORSAllowedOrigins, o)
			}
		}
	}

	cfg.RequireAuthHeader = getEnv("REQUIRE_AUTH_HEADER", "false") == "true"

	return cfg
}

// originAllowed reports whether origin is permitted by the CORS policy.
// Empty allowlist or explicit "*" → allow all.
func (c SecurityConfig) originAllowed(origin string) bool {
	if len(c.CORSAllowedOrigins) == 0 {
		return true
	}
	for _, allowed := range c.CORSAllowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

// securityMiddleware wraps next with the checks described in SecurityConfig.
// Order of rejection: auth-header > origin > body-size. Body-size uses
// http.MaxBytesReader to enforce even when Content-Length is absent/lies.
func securityMiddleware(next http.Handler, cfg SecurityConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Authorization header check (if required)
		if cfg.RequireAuthHeader && r.Header.Get("Authorization") == "" {
			writeJSONError(w, http.StatusUnauthorized,
				"missing Authorization header (REQUIRE_AUTH_HEADER=true)")
			return
		}

		// 2. Origin whitelist check (only if Origin is present)
		if origin := r.Header.Get("Origin"); origin != "" {
			if !cfg.originAllowed(origin) {
				writeJSONError(w, http.StatusForbidden,
					fmt.Sprintf("origin %q not in CORS_ALLOWED_ORIGINS", origin))
				return
			}
		}

		// 3. Content-Length pre-check for early rejection.
		if r.ContentLength > cfg.MaxBodyBytes {
			writeJSONError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("Content-Length %d exceeds MAX_BODY_BYTES %d",
					r.ContentLength, cfg.MaxBodyBytes))
			return
		}

		// 4. Hard cap on actual bytes read — defends against chunked lies
		// and missing Content-Length.
		r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxBodyBytes)

		next.ServeHTTP(w, r)
	})
}

// writeJSONError emits a problem+json-style error body.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":%q,"status":%d}`, message, status)
}
