package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	protocolpack "github.com/RoselleMC/authman/limbo/protocol/pack"
)

func TestPortalProtocolPackHotSyncKeepsLastGoodSnapshot(t *testing.T) {
	customRaw := rewritePortalProtocolPack(t, protocolpack.DefaultZip(), "hot-sync-test")
	custom, err := protocolpack.LoadZip(customRaw)
	if err != nil {
		t.Fatal(err)
	}
	served := customRaw
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/node/limbo/protocol-pack" {
			t.Errorf("unexpected protocol pack path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer node-secret" {
			t.Errorf("unexpected authorization header %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(served)
	}))
	defer core.Close()

	store, err := protocolpack.NewDefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.ProtocolPack()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{CoreURL: core.URL, NodeToken: "node-secret", NodeName: "pack-test"}
	p := &portal{
		cfg:       cfg,
		client:    newCoreClient(cfg),
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		protocols: store,
	}

	desired := protocolPackSync{
		Source:       "custom",
		Configured:   protocolPackInfo{SHA256: custom.Metadata().SHA256},
		DownloadPath: "/api/node/limbo/protocol-pack",
	}
	if err := p.syncProtocolPack(context.Background(), desired); err != nil {
		t.Fatalf("sync valid protocol pack: %v", err)
	}
	after, err := store.ProtocolPack()
	if err != nil {
		t.Fatal(err)
	}
	if after == before || after.Metadata().Version != "hot-sync-test" {
		t.Fatalf("active protocol pack was not replaced: before=%+v after=%+v", before.Metadata(), after.Metadata())
	}
	if before.Metadata().Version != "builtin-1" {
		t.Fatalf("old connection snapshot changed: %+v", before.Metadata())
	}
	if report := p.protocolPackReport(); report.SHA256 != custom.Metadata().SHA256 || report.LastError != "" {
		t.Fatalf("unexpected successful report: %+v", report)
	}
	served = []byte("not a zip")
	bad := desired
	bad.Configured.SHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := p.syncProtocolPack(context.Background(), bad); err == nil {
		t.Fatal("expected invalid downloaded pack to fail")
	}
	afterFailure, err := store.ProtocolPack()
	if err != nil {
		t.Fatal(err)
	}
	if afterFailure != after {
		t.Fatal("failed protocol pack update changed the active snapshot")
	}
	if report := p.protocolPackReport(); report.SHA256 != custom.Metadata().SHA256 || report.LastError == "" {
		t.Fatalf("failed update was not reported while retaining active pack: %+v", report)
	}
}

func rewritePortalProtocolPack(t *testing.T, raw []byte, version string) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte, len(reader.File))
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
		files[file.Name] = content
	}
	var manifest protocolpack.Manifest
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Version = version
	files["manifest.json"], err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetModTime(time.Unix(0, 0).UTC())
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
