package limbgo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestYggdrasilVerifierUsesCustomBaseURL(t *testing.T) {
	var gotUsername, gotServerID, gotIP string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session/minecraft/hasJoined" {
			t.Fatalf("path = %q, want /session/minecraft/hasJoined", r.URL.Path)
		}
		gotUsername = r.URL.Query().Get("username")
		gotServerID = r.URL.Query().Get("serverId")
		gotIP = r.URL.Query().Get("ip")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   "123456781234123412341234567890ab",
			"name": "VerifiedName",
			"properties": []map[string]string{
				{"name": "textures", "value": "texture-value", "signature": "texture-signature"},
			},
		})
	}))
	defer server.Close()

	verifier := NewYggdrasilVerifier(YggdrasilVerifierConfig{BaseURL: server.URL, HTTPClient: server.Client()})
	profile, err := verifier.VerifySession(context.Background(), SessionProof{
		Username: "ClaimedName",
		ServerID: "server-hash",
		RemoteIP: "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("verify session: %v", err)
	}
	if gotUsername != "ClaimedName" || gotServerID != "server-hash" || gotIP != "127.0.0.1" {
		t.Fatalf("query username=%q serverId=%q ip=%q", gotUsername, gotServerID, gotIP)
	}
	if profile.UUID != "12345678-1234-1234-1234-1234567890ab" {
		t.Fatalf("uuid = %q", profile.UUID)
	}
	if profile.Name != "VerifiedName" || !profile.Verified || profile.Source != AuthSourceCustomYggdrasil {
		t.Fatalf("profile = %+v", profile)
	}
	if len(profile.Properties) != 1 || profile.Properties[0].Signature != "texture-signature" {
		t.Fatalf("properties = %+v", profile.Properties)
	}
}

func TestOfflineLoginPlayerMarksUnverifiedClaim(t *testing.T) {
	player := OfflineLoginPlayer(LoginRequest{Username: "Steve", ProtocolVersion: 774, RequestedHost: "login.example"})
	if player.LoginMode != LoginModeOffline || player.Verified || player.AuthSource != AuthSourceOffline {
		t.Fatalf("offline player auth metadata = %+v", player)
	}
	if player.UUID != OfflineUUID("Steve") {
		t.Fatalf("offline uuid = %q", player.UUID)
	}
	if player.RequestedHost != "login.example" {
		t.Fatalf("requested host = %q", player.RequestedHost)
	}
}

func TestOfflineLoginPlayerWithProfileOverridesRuntimeIdentity(t *testing.T) {
	player := OfflineLoginPlayerWithProfile(LoginRequest{
		Username:        "PremiumClaim",
		ProtocolVersion: 774,
		RequestedHost:   "login.example",
	}, LoginProfile{
		Name: "OfflineRuntime",
		UUID: "00000000-0000-0000-0000-000000000123",
		Properties: []ProfileProperty{{
			Name:      "textures",
			Value:     "texture-value",
			Signature: "texture-signature",
		}},
	})
	if player.LoginMode != LoginModeOffline || player.Verified || player.AuthSource != AuthSourceOffline {
		t.Fatalf("offline player auth metadata = %+v", player)
	}
	if player.Name != "OfflineRuntime" || player.UUID != "00000000-0000-0000-0000-000000000123" {
		t.Fatalf("offline player identity = %+v", player)
	}
	if player.ProtocolVersion != 774 || player.RequestedHost != "login.example" {
		t.Fatalf("offline player request metadata = %+v", player)
	}
	if player.Properties["textures"] != "texture-value" {
		t.Fatalf("offline player property map = %+v", player.Properties)
	}
	if len(player.ProfileProperties) != 1 || player.ProfileProperties[0].Signature != "texture-signature" {
		t.Fatalf("offline player profile properties = %+v", player.ProfileProperties)
	}
}

func TestOfflineUUIDMatchesVanillaAlgorithm(t *testing.T) {
	tests := map[string]string{
		"Steve":      "5627dd98-e6be-3c21-b8a8-e92344183641",
		"Alex":       "36532b5e-c442-3dbb-a24c-c7e55d0f979a",
		"TestPlayer": "bb77495a-a740-3169-a238-69654c8bd2c1",
	}
	for username, want := range tests {
		if got := OfflineUUID(username); got != want {
			t.Fatalf("OfflineUUID(%q) = %q, want %q", username, got, want)
		}
	}
}
