package pack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/RoselleMC/authman/limbo/protocol/packetid"
)

func TestDefaultPackMetadata(t *testing.T) {
	loaded, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	metadata := loaded.Metadata()
	if metadata.Name != "authman-default" || metadata.Version != "builtin-1" {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
	if len(metadata.Protocols) != 19 || metadata.Protocols[0] != 757 || metadata.Protocols[len(metadata.Protocols)-1] != 775 {
		t.Fatalf("unexpected protocols: %v", metadata.Protocols)
	}
	if !contains(metadata.MinecraftVersions, "1.21.6") || !contains(metadata.MinecraftVersions, "1.21.11") || !contains(metadata.MinecraftVersions, "26.1.2") {
		t.Fatalf("missing release names: %v", metadata.MinecraftVersions)
	}
	if len(metadata.SHA256) != 64 {
		t.Fatalf("unexpected sha256: %q", metadata.SHA256)
	}
	portal, ok := loaded.Protocol(774)
	if !ok || !portal.Layout.PortalDialog || !portal.Layout.ComponentPayloadNBT || !portal.Layout.EncryptionRequestAuthenticate {
		t.Fatalf("protocol 774 is missing portal runtime semantics: %+v", portal.Layout)
	}
	preDialog, ok := loaded.Protocol(770)
	if !ok || preDialog.Layout.PortalDialog {
		t.Fatalf("protocol 770 unexpectedly enables portal dialog: %+v", preDialog.Layout)
	}
}

func TestLoadZipAcceptsFutureProtocolUsingDeclaredAliases(t *testing.T) {
	files := readTestZip(t, DefaultZip())
	var manifest Manifest
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	var future ProtocolDescriptor
	for _, descriptor := range manifest.Protocols {
		if descriptor.Protocol == 774 {
			future = descriptor
			break
		}
	}
	if future.Protocol == 0 {
		t.Fatal("protocol 774 descriptor missing")
	}
	future.Protocol = 999
	future.MinecraftVersions = []string{"future-test"}
	future.PacketIDProtocol = 774
	future.DataProtocol = 774
	future.RegistryDataProtocol = 774
	future.BlockStateProtocol = 774
	manifest.Protocols = append(manifest.Protocols, future)
	files["manifest.json"] = marshalTestJSON(t, manifest)

	loaded, err := LoadZip(writeTestZip(t, files))
	if err != nil {
		t.Fatalf("load future aliased protocol: %v", err)
	}
	descriptor, ok := loaded.Protocol(999)
	if !ok || !descriptor.Layout.PortalDialog || descriptor.PacketIDProtocol != 774 {
		t.Fatalf("future protocol was not compiled from manifest: %+v", descriptor)
	}
}

func TestLoadZipRequiresDialogPacketsByCapability(t *testing.T) {
	files := readTestZip(t, DefaultZip())
	var manifest Manifest
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	for index := range manifest.Protocols {
		if manifest.Protocols[index].Protocol == 770 {
			manifest.Protocols[index].Layout.PortalDialog = true
		}
	}
	files["manifest.json"] = marshalTestJSON(t, manifest)
	if _, err := LoadZip(writeTestZip(t, files)); err == nil {
		t.Fatal("expected portal_dialog layout without dialog packets to be rejected")
	}
}

func TestLoadZipRejectsDialogWithoutCurrentComponentSemantics(t *testing.T) {
	files := readTestZip(t, DefaultZip())
	var manifest Manifest
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	for index := range manifest.Protocols {
		if manifest.Protocols[index].Protocol == 771 {
			manifest.Protocols[index].Layout.ComponentPayloadNBT = false
		}
	}
	files["manifest.json"] = marshalTestJSON(t, manifest)
	if _, err := LoadZip(writeTestZip(t, files)); err == nil {
		t.Fatal("expected portal_dialog without NBT component payloads to be rejected")
	}
}

func TestLoadZipRejectsUnknownManifestField(t *testing.T) {
	files := readTestZip(t, DefaultZip())
	var manifest map[string]any
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["surprise"] = true
	files["manifest.json"] = marshalTestJSON(t, manifest)
	if _, err := LoadZip(writeTestZip(t, files)); err == nil {
		t.Fatal("expected unknown manifest field to be rejected")
	}
}

func TestLoadZipRejectsMissingRequiredDialogPacket(t *testing.T) {
	files := readTestZip(t, DefaultZip())
	var versions []packetid.VersionPackets
	if err := json.Unmarshal(files["packets.json"], &versions); err != nil {
		t.Fatal(err)
	}
	for versionIndex := range versions {
		if versions[versionIndex].Protocol != 775 {
			continue
		}
		entries := versions[versionIndex].Entries[:0]
		for _, entry := range versions[versionIndex].Entries {
			if entry.State == packetid.StatePlay && entry.Direction == packetid.ToClient && entry.Name == "show_dialog" {
				continue
			}
			entries = append(entries, entry)
		}
		versions[versionIndex].Entries = entries
	}
	files["packets.json"] = marshalTestJSON(t, versions)
	if _, err := LoadZip(writeTestZip(t, files)); err == nil {
		t.Fatal("expected missing show_dialog packet to be rejected")
	}
}

func TestStoreUpdateIsAtomic(t *testing.T) {
	store, err := NewDefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.ProtocolPack()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateZip([]byte("not a zip")); err == nil {
		t.Fatal("expected invalid update to fail")
	}
	afterFailure, err := store.ProtocolPack()
	if err != nil {
		t.Fatal(err)
	}
	if afterFailure != before {
		t.Fatal("failed update changed the active snapshot")
	}

	files := readTestZip(t, DefaultZip())
	var manifest Manifest
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Version = "test-hot-update"
	files["manifest.json"] = marshalTestJSON(t, manifest)
	metadata, err := store.UpdateZip(writeTestZip(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Version != "test-hot-update" {
		t.Fatalf("unexpected update metadata: %+v", metadata)
	}
	afterSuccess, err := store.ProtocolPack()
	if err != nil {
		t.Fatal(err)
	}
	if afterSuccess == before || before.Metadata().Version != "builtin-1" {
		t.Fatal("existing snapshot did not remain immutable")
	}
}

func readTestZip(t *testing.T, raw []byte) map[string][]byte {
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
		data, err := io.ReadAll(handle)
		if err != nil {
			t.Fatal(err)
		}
		if err := handle.Close(); err != nil {
			t.Fatal(err)
		}
		files[file.Name] = data
	}
	return files
}

func writeTestZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
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

func marshalTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
