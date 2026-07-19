package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
		preJoinResolve: map[string][]preJoinResolveState{},
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

func TestConfigurationFastPathTransfersExistingPremiumSingleProfile(t *testing.T) {
	requests := map[string]int{}
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		var data any
		switch r.URL.Path {
		case "/api/node/players/resolve":
			data = map[string]any{
				"player":         map[string]any{"uuid": "11111111-1111-1111-1111-111111111111", "kind": "premium", "protocol_name": "PremiumPlayer"},
				"passport":       map[string]any{"id": "passport-premium", "username": "PremiumPlayer", "kind": "premium", "locked": false},
				"profiles":       []map[string]any{{"id": "profile-main", "uuid": "11111111-1111-1111-1111-111111111111", "protocol_name": "PremiumPlayer", "primary": true}},
				"profile_policy": map[string]any{"max_profiles": 3, "can_create": true, "auto_join_single_profile": true},
				"auth":           map[string]any{"required": false, "kind": "premium", "locked": false, "username": "PremiumPlayer", "remembered": false},
			}
		case "/api/node/limbo/targets/resolve":
			data = map[string]any{"target": map[string]any{"server_id": "server-main", "display_name": "Main", "transfer_host": "play.example.test", "transfer_port": 25565}}
		case "/api/node/limbo/transfer-grants":
			data = map[string]any{"token": "grant-token", "target": map[string]any{"server_id": "server-main", "display_name": "Main", "transfer_host": "play.example.test", "transfer_port": 25565}}
		default:
			t.Fatalf("unexpected core path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "error": nil})
	}))
	defer core.Close()

	player := limbgo.Player{
		Name:            "PremiumPlayer",
		UUID:            "11111111-1111-1111-1111-111111111111",
		ProtocolVersion: 766,
		RequestedHost:   "play.example.test",
		RemoteAddr:      &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 25565},
		LoginMode:       limbgo.LoginModeOnline,
		AuthSource:      limbgo.AuthSourceMojang,
		Verified:        true,
	}
	p := newTestPortal(core.URL)
	key := verifiedJoinKey(player.UUID, remoteIP(player.RemoteAddr), player.RequestedHost)
	p.verifiedJoins[key] = []verifiedJoinState{{passportID: "passport-premium", passportPreexisting: true, expires: time.Now().Add(time.Minute)}}
	session := &testPlayerSession{
		player:       player,
		capabilities: limbgo.SessionCapabilities{StoreCookie: true, Transfer: true, Disconnect: true},
	}

	if err := p.handleConfiguration(context.Background(), session, &limbgo.ConfigurationEvent{Player: player, Protocol: 766}); err != nil {
		t.Fatal(err)
	}
	if session.cookieKey != "authman:transfer_grant" || string(session.cookieValue) != "grant-token" {
		t.Fatalf("stored cookie = %q %q", session.cookieKey, session.cookieValue)
	}
	if session.transferHost != "play.example.test" || session.transferPort != 25565 {
		t.Fatalf("transfer target = %s:%d", session.transferHost, session.transferPort)
	}
	if requests["/api/node/players/resolve"] != 1 || requests["/api/node/limbo/targets/resolve"] != 1 || requests["/api/node/limbo/transfer-grants"] != 1 {
		t.Fatalf("core requests = %#v", requests)
	}
	if _, ok := p.peekVerifiedJoin(player); ok {
		t.Fatal("verified join proof was not consumed after fast transfer")
	}
}

func TestConfigurationFastPathKeepsFirstPremiumJoinInLimbo(t *testing.T) {
	requests := map[string]int{}
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		if r.URL.Path == "/api/node/limbo/targets/resolve" {
			data := map[string]any{"target": map[string]any{"server_id": "server-main", "display_name": "Main", "transfer_host": "play.example.test", "transfer_port": 25565}}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "error": nil})
			return
		}
		if r.URL.Path != "/api/node/players/resolve" {
			t.Fatalf("first premium join unexpectedly requested %s", r.URL.Path)
		}
		data := map[string]any{
			"player":         map[string]any{"uuid": "22222222-2222-2222-2222-222222222222", "kind": "premium", "protocol_name": "NewPremium"},
			"passport":       map[string]any{"id": "passport-new", "username": "NewPremium", "kind": "premium", "locked": false},
			"profiles":       []map[string]any{{"id": "profile-main", "uuid": "22222222-2222-2222-2222-222222222222", "protocol_name": "NewPremium", "primary": true}},
			"profile_policy": map[string]any{"max_profiles": 3, "can_create": true, "auto_join_single_profile": true},
			"auth":           map[string]any{"required": false, "kind": "premium", "locked": false, "username": "NewPremium", "remembered": false},
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "error": nil})
	}))
	defer core.Close()

	player := limbgo.Player{
		Name: "NewPremium", UUID: "22222222-2222-2222-2222-222222222222", ProtocolVersion: 766,
		RequestedHost: "play.example.test", RemoteAddr: &net.TCPAddr{IP: net.ParseIP("192.0.2.20"), Port: 25565},
		LoginMode: limbgo.LoginModeOnline, AuthSource: limbgo.AuthSourceMojang, Verified: true,
	}
	p := newTestPortal(core.URL)
	key := verifiedJoinKey(player.UUID, remoteIP(player.RemoteAddr), player.RequestedHost)
	p.verifiedJoins[key] = []verifiedJoinState{{passportID: "passport-new", passportPreexisting: false, expires: time.Now().Add(time.Minute)}}
	session := &testPlayerSession{player: player, capabilities: limbgo.SessionCapabilities{StoreCookie: true, Transfer: true, Disconnect: true}}

	if err := p.handleConfiguration(context.Background(), session, &limbgo.ConfigurationEvent{Player: player, Protocol: 766}); err != nil {
		t.Fatal(err)
	}
	if session.transferHost != "" || session.cookieKey != "" {
		t.Fatalf("first premium join used fast transfer: cookie=%q target=%q", session.cookieKey, session.transferHost)
	}
	if _, ok := p.peekVerifiedJoin(player); !ok {
		t.Fatal("first premium join proof must remain for the normal dialog path")
	}
	playSession := &testPlayerSession{player: player, capabilities: limbgo.SessionCapabilities{Dialog: true}}
	if err := p.showLoginDialog(context.Background(), playSession); err != nil {
		t.Fatal(err)
	}
	if playSession.dialog == nil {
		t.Fatal("first premium join did not show its one-time login dialog")
	}
	if requests["/api/node/players/resolve"] != 1 {
		t.Fatalf("player resolve requests = %d, want configuration result reused", requests["/api/node/players/resolve"])
	}
	if _, ok := p.peekVerifiedJoin(player); ok {
		t.Fatal("normal dialog path did not consume the verified join proof")
	}
}

func newTestPortal(coreURL string) *portal {
	cfg := config{CoreURL: coreURL, NodeToken: "node-secret", SourceID: "limbo-test"}
	return &portal{
		cfg: cfg, client: newCoreClient(cfg), logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		messages: map[string]string{}, dialogs: map[string]playermsg.DialogDoc{}, targetNames: map[string]string{},
		targetMessages: map[string]playerMessagesPayload{}, authed: map[limbgo.PlayerSession]authedSession{}, verifiedJoins: map[string][]verifiedJoinState{},
		preJoinResolve: map[string][]preJoinResolveState{},
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
	cookieKey    string
	cookieValue  []byte
	transferHost string
	transferPort int
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

func (s *testPlayerSession) StoreCookie(_ context.Context, key string, value []byte) error {
	s.cookieKey = key
	s.cookieValue = append([]byte(nil), value...)
	return nil
}

func (s *testPlayerSession) Transfer(_ context.Context, host string, port int) error {
	s.transferHost = host
	s.transferPort = port
	return nil
}

func (s *testPlayerSession) Disconnect(context.Context, component.Component) error { return nil }
