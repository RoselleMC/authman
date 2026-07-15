package dialog

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RoselleMC/authman/limbo/minimessage"
)

func TestDialogComponentJSONPreservesRGBGradient(t *testing.T) {
	title, err := minimessage.Parse("<gradient:#123456:#abcdef>abc</gradient>")
	if err != nil {
		t.Fatalf("parse gradient: %v", err)
	}
	body, err := minimessage.Parse("<gradient:#654321:#fedcba>xyz</gradient>")
	if err != nil {
		t.Fatalf("parse body gradient: %v", err)
	}
	raw, err := json.Marshal(Notice(Common{
		Title: title,
		Body: []Raw{
			PlainMessage(body, 220),
		},
	}, Button(Text("OK"), Custom("limbgo:ok", nil))))
	if err != nil {
		t.Fatalf("marshal dialog: %v", err)
	}
	text := string(raw)
	for _, want := range []string{`"#123456"`, `"#5e80a2"`, `"#abcdef"`, `"#654321"`, `"#b18f6d"`, `"#fedcba"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("dialog json missing %s in %s", want, text)
		}
	}
}
