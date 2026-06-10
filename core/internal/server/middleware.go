package server

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type requestIDKey struct{}
type basePathKey struct{}

func basePathFromContext(ctx context.Context) string {
	basePath, _ := ctx.Value(basePathKey{}).(string)
	return basePath
}

func basePathMiddleware(configuredBasePath string, next http.Handler) http.Handler {
	configuredBasePath = normalizeRequestBasePath(configuredBasePath)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headerBasePath := forwardedPrefixBasePath(r)
		activeBasePath := configuredBasePath
		if headerBasePath != "" {
			activeBasePath = headerBasePath
		}
		if activeBasePath == "" {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == activeBasePath {
			rr := r.Clone(context.WithValue(r.Context(), basePathKey{}, activeBasePath))
			rr.URL.Path = "/"
			rr.URL.RawPath = ""
			next.ServeHTTP(w, rr)
			return
		}
		if strings.HasPrefix(r.URL.Path, activeBasePath+"/") {
			rr := r.Clone(context.WithValue(r.Context(), basePathKey{}, activeBasePath))
			rr.URL.Path = strings.TrimPrefix(r.URL.Path, activeBasePath)
			if rr.URL.Path == "" {
				rr.URL.Path = "/"
			}
			rr.URL.RawPath = ""
			next.ServeHTTP(w, rr)
			return
		}
		rr := r.Clone(context.WithValue(r.Context(), basePathKey{}, activeBasePath))
		next.ServeHTTP(w, rr)
	})
}

func forwardedPrefixBasePath(r *http.Request) string {
	if value := r.Header.Get("X-Forwarded-Prefix"); value != "" {
		if comma := strings.IndexByte(value, ','); comma >= 0 {
			value = value[:comma]
		}
		return normalizeRequestBasePath(value)
	}
	return ""
}

func normalizeRequestBasePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	value = strings.TrimRight(value, "/")
	if value == "/" {
		return ""
	}
	return value
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID = strconv.FormatUint(rand.Uint64(), 36)
		}
		w.Header().Set("X-Request-Id", requestID)
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
		}
		next.ServeHTTP(rec, r)
		logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", rec.Header().Get("X-Request-Id"),
		)
	})
}

func corsMiddleware(allowedOrigins []string, next http.Handler) http.Handler {
	if len(allowedOrigins) == 0 {
		return next
	}
	allowed := make(map[string]struct{}, len(allowedOrigins))
	allowAny := false
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if origin == "*" {
			allowAny = true
			continue
		}
		allowed[origin] = struct{}{}
	}
	if len(allowed) == 0 && !allowAny {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if _, ok := allowed[origin]; ok || allowAny {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Add("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CSRF-Token, X-Request-Id")
				w.Header().Set("Access-Control-Expose-Headers", "X-Request-Id")
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
