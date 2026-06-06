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
