package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/auth"
	"github.com/RoselleMC/authman/core/internal/config"
	"github.com/RoselleMC/authman/core/internal/store"
	protocolpack "github.com/RoselleMC/authman/limbo/protocol/pack"
)

func TestLimboProtocolPackLifecycle(t *testing.T) {
	srv := New(Options{
		Config: config.Config{
			HTTPAddr:      ":0",
			PublicBaseURL: "http://example.test",
			AdminUsername: "admin",
			AdminPassword: "correct admin password",
		},
		Logger: slog.Default(),
		Store:  store.NewMemory(),
		PasswordParams: auth.Argon2idParams{
			MemoryKiB:   1024,
			Iterations:  1,
			Parallelism: 1,
			SaltLength:  16,
			KeyLength:   32,
		},
	})
	adminCookie, csrf := adminLogin(t, srv)
	created := requestJSONWithSession(t, srv, http.MethodPost, "/api/admin/login-portals", `{"name":"pack-test"}`, adminCookie, csrf)
	if created.Code != http.StatusCreated {
		t.Fatalf("create limbo portal status = %d body=%s", created.Code, created.Body.String())
	}
	var envelope api.Envelope
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	createdData := envelope.Data.(map[string]any)
	nodeID := createdData["node"].(map[string]any)["id"].(string)
	nodeToken := createdData["token"].(string)
	adminPath := "/api/admin/login-portals/" + nodeID + "/protocol-pack"

	builtin := requestJSONWithSession(t, srv, http.MethodGet, adminPath, "", adminCookie, csrf)
	if builtin.Code != http.StatusOK {
		t.Fatalf("get built-in pack status = %d body=%s", builtin.Code, builtin.Body.String())
	}
	builtinData := protocolPackEnvelopeData(t, builtin)
	if builtinData["source"] != "builtin" || builtinData["in_sync"] != false {
		t.Fatalf("unexpected built-in state: %#v", builtinData)
	}
	builtinConfigured := builtinData["configured"].(map[string]any)
	assertStringListContains(t, builtinConfigured["minecraft_versions"], "1.21.6", "1.21.11", "26.1.2")
	assertStringListExcludes(t, builtinConfigured["minecraft_versions"], "1.21.5")

	download := requestNodeProtocolPack(t, srv, nodeToken)
	if download.Code != http.StatusOK {
		t.Fatalf("download built-in pack status = %d body=%s", download.Code, download.Body.String())
	}
	loadedBuiltin, err := protocolpack.LoadZip(download.Body.Bytes())
	if err != nil {
		t.Fatalf("load downloaded built-in pack: %v", err)
	}
	if loadedBuiltin.Metadata().SHA256 != builtinConfigured["sha256"] {
		t.Fatalf("downloaded SHA = %s configured = %#v", loadedBuiltin.Metadata().SHA256, builtinConfigured["sha256"])
	}

	invalid := requestProtocolPackUpload(t, srv, adminPath, "broken.zip", []byte("not a zip"), adminCookie, csrf)
	if invalid.Code != http.StatusBadRequest || !bytes.Contains(invalid.Body.Bytes(), []byte("limbo_protocol_pack.validation_failed")) {
		t.Fatalf("invalid upload status = %d body=%s", invalid.Code, invalid.Body.String())
	}
	afterInvalid := protocolPackEnvelopeData(t, requestJSONWithSession(t, srv, http.MethodGet, adminPath, "", adminCookie, csrf))
	if afterInvalid["source"] != "builtin" {
		t.Fatalf("invalid upload changed configured source: %#v", afterInvalid)
	}

	customRaw := rewriteProtocolPackVersion(t, protocolpack.DefaultZip(), "hot-test-2")
	custom, err := protocolpack.LoadZip(customRaw)
	if err != nil {
		t.Fatalf("load custom protocol pack: %v", err)
	}
	uploaded := requestProtocolPackUpload(t, srv, adminPath, "custom protocols.zip", customRaw, adminCookie, csrf)
	if uploaded.Code != http.StatusOK {
		t.Fatalf("upload custom pack status = %d body=%s", uploaded.Code, uploaded.Body.String())
	}
	uploadedData := protocolPackEnvelopeData(t, uploaded)
	uploadedConfigured := uploadedData["configured"].(map[string]any)
	if uploadedData["source"] != "custom" || uploadedConfigured["version"] != "hot-test-2" || uploadedConfigured["sha256"] != custom.Metadata().SHA256 {
		t.Fatalf("unexpected custom pack state: %#v", uploadedData)
	}

	heartbeatBody, err := json.Marshal(map[string]any{"protocol_pack": portalPackMetadata(custom)})
	if err != nil {
		t.Fatal(err)
	}
	heartbeat := httptest.NewRequest(http.MethodPost, "/api/node/heartbeat", bytes.NewReader(heartbeatBody))
	heartbeat.Header.Set("Authorization", "Bearer "+nodeToken)
	heartbeat.Header.Set("Content-Type", "application/json")
	heartbeatRecorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(heartbeatRecorder, heartbeat)
	if heartbeatRecorder.Code != http.StatusOK {
		t.Fatalf("protocol heartbeat status = %d body=%s", heartbeatRecorder.Code, heartbeatRecorder.Body.String())
	}

	inSync := protocolPackEnvelopeData(t, requestJSONWithSession(t, srv, http.MethodGet, adminPath, "", adminCookie, csrf))
	if inSync["in_sync"] != true {
		t.Fatalf("expected active pack to be in sync: %#v", inSync)
	}
	active := inSync["active"].(map[string]any)
	if active["sha256"] != custom.Metadata().SHA256 || active["version"] != "hot-test-2" {
		t.Fatalf("unexpected active pack report: %#v", active)
	}

	reset := requestJSONWithSession(t, srv, http.MethodDelete, adminPath, "", adminCookie, csrf)
	if reset.Code != http.StatusOK {
		t.Fatalf("reset protocol pack status = %d body=%s", reset.Code, reset.Body.String())
	}
	resetData := protocolPackEnvelopeData(t, reset)
	if resetData["source"] != "builtin" || resetData["in_sync"] != false {
		t.Fatalf("unexpected reset state: %#v", resetData)
	}
}

func requestNodeProtocolPack(t *testing.T, srv *Server, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/node/limbo/protocol-pack", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func requestProtocolPackUpload(t *testing.T, srv *Server, path, filename string, raw []byte, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set(csrfHeader, csrf)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func protocolPackEnvelopeData(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("protocol pack response status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope api.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data.(map[string]any)
}

func rewriteProtocolPackVersion(t *testing.T, raw []byte, version string) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, file := range reader.File {
		handle, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(handle)
		if closeErr := handle.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			t.Fatal(err)
		}
		if file.Name == "manifest.json" {
			var manifest protocolpack.Manifest
			if err := json.Unmarshal(content, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest.Version = version
			content, err = json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
		}
		entry, err := writer.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func assertStringListContains(t *testing.T, raw any, values ...string) {
	t.Helper()
	seen := make(map[string]bool)
	for _, item := range raw.([]any) {
		seen[item.(string)] = true
	}
	for _, value := range values {
		if !seen[value] {
			t.Fatalf("version list %#v does not contain %q", raw, value)
		}
	}
}

func assertStringListExcludes(t *testing.T, raw any, values ...string) {
	t.Helper()
	seen := make(map[string]bool)
	for _, item := range raw.([]any) {
		seen[item.(string)] = true
	}
	for _, value := range values {
		if seen[value] {
			t.Fatalf("version list %#v unexpectedly contains %q", raw, value)
		}
	}
}
