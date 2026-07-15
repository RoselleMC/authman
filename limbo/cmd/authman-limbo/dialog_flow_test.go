package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RoselleMC/authman/internal/playermsg"
	"github.com/RoselleMC/authman/limbo"
	"github.com/RoselleMC/authman/limbo/dialog"
	"go.minekube.com/common/minecraft/component"
)

func TestShowLoginDialogNotFoundOfflinePlayerShowsRegister(t *testing.T) {
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/node/players/resolve" {
			t.Fatalf("unexpected core path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": nil,
			"error": map[string]any{
				"code":    "player.not_found",
				"message": "player not found",
			},
		})
	}))
	defer core.Close()

	p := &portal{
		cfg:            config{CoreURL: core.URL, NodeToken: "node-secret"},
		client:         newCoreClient(config{CoreURL: core.URL, NodeToken: "node-secret"}),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		messages:       map[string]string{},
		dialogs:        map[string]playermsg.DialogDoc{},
		targetNames:    map[string]string{},
		targetMessages: map[string]playerMessagesPayload{},
		authed:         map[limbgo.PlayerSession]authedSession{},
		verifiedJoins:  map[string][]verifiedJoinState{},
	}
	session := &testPlayerSession{
		player: limbgo.Player{
			Name:            "OfflineNew",
			UUID:            limbgo.OfflineUUID("OfflineNew"),
			ProtocolVersion: 771,
			LoginMode:       limbgo.LoginModeOffline,
			AuthSource:      limbgo.AuthSourceOffline,
			Verified:        false,
		},
		capabilities: limbgo.SessionCapabilities{Dialog: true},
	}

	if err := p.showLoginDialog(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if session.dialog == nil {
		t.Fatal("no dialog was shown")
	}
	keys := dialogInputKeys(t, session.dialog)
	if !keys["password"] || !keys["confirm_password"] {
		t.Fatalf("expected register dialog inputs, got %#v", keys)
	}
}

func dialogInputKeys(t *testing.T, d dialog.Dialog) map[string]bool {
	t.Helper()
	raw, err := d.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	inputs, _ := payload["inputs"].([]any)
	for _, input := range inputs {
		item, _ := input.(map[string]any)
		key, _ := item["key"].(string)
		if key != "" {
			out[key] = true
		}
	}
	return out
}

type testPlayerSession struct {
	player       limbgo.Player
	capabilities limbgo.SessionCapabilities
	dialog       dialog.Dialog
}

func (s *testPlayerSession) Player() limbgo.Player { return s.player }

func (s *testPlayerSession) Capabilities() limbgo.SessionCapabilities { return s.capabilities }

func (s *testPlayerSession) SendMessage(context.Context, component.Component) error { return nil }

func (s *testPlayerSession) SendActionBar(context.Context, component.Component) error { return nil }

func (s *testPlayerSession) ShowTitle(context.Context, limbgo.Title) error { return nil }

func (s *testPlayerSession) ClearTitle(context.Context, bool) error { return nil }

func (s *testPlayerSession) ShowDialog(_ context.Context, d dialog.Dialog) error {
	s.dialog = d
	return nil
}

func (s *testPlayerSession) ClearDialog(context.Context) error { return nil }

func (s *testPlayerSession) AddResourcePack(context.Context, limbgo.ResourcePack) error { return nil }

func (s *testPlayerSession) RemoveResourcePack(context.Context, string) error { return nil }

func (s *testPlayerSession) StoreCookie(context.Context, string, []byte) error { return nil }

func (s *testPlayerSession) Transfer(context.Context, string, int) error { return nil }

func (s *testPlayerSession) Disconnect(context.Context, component.Component) error { return nil }
