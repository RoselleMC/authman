package server

import (
	"encoding/json"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if s.hasCoreWeb() && (r.URL.Path != "/" || acceptsHTML(r)) {
		s.serveCoreWeb(w, r)
		return
	}
	s.handleYggdrasilMetadata(w, r)
}

func (s *Server) handleCoreWeb(w http.ResponseWriter, r *http.Request) {
	if !s.hasCoreWeb() {
		http.NotFound(w, r)
		return
	}
	s.serveCoreWeb(w, r)
}

func (s *Server) handleCoreWebConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	basePath := requestBasePath(r, s.cfg.HTTPBasePath)
	cfg := map[string]string{
		"apiBase":       joinURLPath(basePath, "/api"),
		"basePath":      basePath,
		"appKind":       "admin",
		"defaultLocale": s.cfg.DefaultLocale,
	}
	raw, _ := json.Marshal(cfg)
	_, _ = w.Write([]byte("window.__AUTHMAN_RUNTIME_CONFIG__ = "))
	_, _ = w.Write(raw)
	_, _ = w.Write([]byte(";\n"))
}

func requestBasePath(r *http.Request, fallback string) string {
	if basePath := basePathFromContext(r.Context()); basePath != "" {
		return basePath
	}
	if basePath := forwardedPrefixBasePath(r); basePath != "" {
		return basePath
	}
	return normalizeRequestBasePath(fallback)
}

func joinURLPath(basePath string, path string) string {
	basePath = normalizeRequestBasePath(basePath)
	if path == "" || path == "/" {
		if basePath == "" {
			return "/"
		}
		return basePath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if basePath == "" {
		return path
	}
	return basePath + path
}

func (s *Server) hasCoreWeb() bool {
	return strings.TrimSpace(s.cfg.WebRoot) != ""
}

func (s *Server) serveCoreWeb(w http.ResponseWriter, r *http.Request) {
	root := strings.TrimSpace(s.cfg.WebRoot)
	if root == "" {
		http.NotFound(w, r)
		return
	}
	clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if clean == "." || clean == string(filepath.Separator) {
		clean = "index.html"
	}
	target := filepath.Join(root, clean)
	if !isPathInside(root, target) {
		http.NotFound(w, r)
		return
	}
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		if filepath.Base(target) == "index.html" {
			s.serveCoreWebIndex(w, r, target)
			return
		}
		http.ServeFile(w, r, target)
		return
	}
	s.serveCoreWebIndex(w, r, filepath.Join(root, "index.html"))
}

func (s *Server) serveCoreWebIndex(w http.ResponseWriter, r *http.Request, indexPath string) {
	body, err := os.ReadFile(indexPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	basePath := requestBasePath(r, s.cfg.HTTPBasePath)
	baseHref := "/"
	if basePath != "" {
		baseHref = basePath + "/"
	}
	baseTag := []byte(`<base href="` + html.EscapeString(baseHref) + `" />`)
	if !strings.Contains(string(body), "<base ") {
		body = []byte(strings.Replace(string(body), "<head>", "<head>\n    "+string(baseTag), 1))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}

func acceptsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html") || strings.Contains(accept, "application/xhtml+xml")
}

func isPathInside(root string, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if target == root {
		return true
	}
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}
