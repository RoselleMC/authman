package versions

import "testing"

func TestLookupProtocol(t *testing.T) {
	cases := map[int32]string{
		47:  "1.8.8",
		340: "1.12.2",
		754: "1.16.5",
		767: "1.21.1",
		774: "1.21.11",
		775: "26.1.2",
	}

	for protocol, wantLatest := range cases {
		matches := LookupProtocol(protocol)
		if len(matches) == 0 {
			t.Fatalf("protocol %d has no generated versions", protocol)
		}
		if got := matches[len(matches)-1].MinecraftVersion; got != wantLatest {
			t.Fatalf("protocol %d latest = %s, want %s", protocol, got, wantLatest)
		}
	}
}

func TestLatestRelease(t *testing.T) {
	latest, ok := LatestRelease()
	if !ok {
		t.Fatal("no latest release")
	}
	if latest.MinecraftVersion != "26.1.2" || latest.Protocol != 775 {
		t.Fatalf("latest = %+v, want 26.1.2 protocol 775", latest)
	}
}

func TestGeneratedVersionsExcludeSnapshots(t *testing.T) {
	for _, version := range All() {
		if version.Protocol >= 1073741824 {
			t.Fatalf("snapshot protocol leaked into release index: %+v", version)
		}
	}
}
