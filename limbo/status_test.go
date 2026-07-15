package limbgo

import (
	"encoding/json"
	"strings"
	"testing"

	"go.minekube.com/common/minecraft/component"
)

func TestMarshalStatusJSONRichMOTDAndFeatures(t *testing.T) {
	secure := true
	previews := false
	prevents := true
	raw, err := MarshalStatusJSON(Status{
		VersionName:   "limbgo test",
		Protocol:      999,
		Description:   &component.Text{Content: "Hello", Extra: []component.Component{&component.Text{Content: " MOTD"}}},
		MaxPlayers:    100,
		OnlinePlayers: 2,
		SamplePlayers: []StatusSamplePlayer{
			{Name: "Score2", ID: "00000000-0000-0000-0000-000000000001"},
		},
		Favicon:             "data:image/png;base64,AAAA",
		EnforcesSecureChat:  &secure,
		PreviewsChat:        &previews,
		PreventsChatReports: &prevents,
		Raw:                 map[string]any{"custom": "value"},
	}, 774)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		`"name":"limbgo test"`,
		`"protocol":999`,
		`"text":"Hello"`,
		`"sample"`,
		`"favicon":"data:image/png;base64,AAAA"`,
		`"enforcesSecureChat":true`,
		`"previewsChat":false`,
		`"preventsChatReports":true`,
		`"custom":"value"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status json missing %s in %s", want, text)
		}
	}
}

func TestFileStatusConfigMiniMessageComponent(t *testing.T) {
	cfg := FileStatusConfig{MOTDMiniMessage: "<red><bold>limbgo", MaxPlayers: 5}
	motd, err := cfg.Component()
	if err != nil {
		t.Fatalf("component: %v", err)
	}
	raw, err := MarshalComponentJSON(774, motd)
	if err != nil {
		t.Fatalf("marshal component: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal component: %v", err)
	}
	if decoded["bold"] != true {
		t.Fatalf("bold = %v, want true in %s", decoded["bold"], raw)
	}
}

func TestMarshalComponentJSONPreservesRGBGradient(t *testing.T) {
	component, err := ParseMiniMessage("<gradient:#123456:#abcdef>abc</gradient>")
	if err != nil {
		t.Fatalf("parse gradient: %v", err)
	}
	raw, err := MarshalComponentJSON(774, component)
	if err != nil {
		t.Fatalf("marshal component: %v", err)
	}
	text := string(raw)
	for _, want := range []string{`"#123456"`, `"#5e80a2"`, `"#abcdef"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("component json missing %s in %s", want, text)
		}
	}
}
