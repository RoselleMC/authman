package minimessage

import (
	"bytes"
	"strings"
	"testing"

	"go.minekube.com/common/minecraft/component"
	"go.minekube.com/common/minecraft/component/codec"
)

func TestParseStyleClickHoverAndNewline(t *testing.T) {
	got, err := Parse(`<red><bold>Hello <click:run_command:/hub><hover:show_text:'<green>Go'>hub</hover></click><newline><#00ff00>RGB`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	raw := marshalJSON(t, got)
	for _, want := range []string{
		`"color":"#ff5555"`,
		`"bold":true`,
		`"click_event"`,
		`"run_command"`,
		`"hover_event"`,
		`"show_text"`,
		`"\n"`,
		`"#00ff00"`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("component json missing %s in %s", want, raw)
		}
	}
}

func TestParseGradientAndTranslation(t *testing.T) {
	got, err := Parse(`<gradient:red:blue>abc</gradient> <lang:block.minecraft.stone>`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	raw := marshalJSON(t, got)
	for _, want := range []string{
		`"color":"#ff5555"`,
		`"color":"#5555ff"`,
		`"translate":"block.minecraft.stone"`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("component json missing %s in %s", want, raw)
		}
	}
}

func TestParseHoverItemEntityAndFontDefaultNamespace(t *testing.T) {
	got, err := Parse(`<font:default><hover:show_item:diamond:2>item</hover> <hover:show_entity:player:00000000-0000-0000-0000-000000000002:'<gold>Score2'>entity</hover></font>`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	raw := marshalJSON(t, got)
	for _, want := range []string{
		`"font":"minecraft:default"`,
		`"show_item"`,
		`"id":"minecraft:diamond"`,
		`"count":2`,
		`"show_entity"`,
		`"uuid":"00000000-0000-0000-0000-000000000002"`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("component json missing %s in %s", want, raw)
		}
	}
}

func marshalJSON(t *testing.T, c component.Component) string {
	t.Helper()
	var out bytes.Buffer
	encoder := codec.Json{StdJson: true, NoDownsampleColor: true}
	if err := encoder.Marshal(&out, c); err != nil {
		t.Fatalf("marshal component: %v", err)
	}
	return out.String()
}
