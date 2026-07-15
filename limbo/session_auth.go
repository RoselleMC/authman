package limbgo

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultYggdrasilSessionServer = "https://sessionserver.mojang.com"
	AuthSourceOffline             = "offline"
	AuthSourceMojang              = "mojang"
	AuthSourceCustomYggdrasil     = "custom-yggdrasil"
	AuthSourceApplication         = "application"
)

// LoginMode describes the session authentication mode for one connection.
type LoginMode string

const (
	LoginModeOffline LoginMode = "offline"
	LoginModeOnline  LoginMode = "online"
	LoginModeHybrid  LoginMode = "hybrid"
)

// LoginRequest is the claimed login information available before online-mode
// session proof is requested.
type LoginRequest struct {
	Username        string
	ClaimedUUID     string
	ProtocolVersion int
	RemoteAddr      net.Addr
	RequestedHost   string
}

// LoginPolicy decides whether a connection should use offline or online
// verification. Use it for hybrid deployments.
type LoginPolicy interface {
	ResolveLoginMode(ctx context.Context, req LoginRequest) (LoginMode, error)
}

// LoginPolicyFunc adapts a function to LoginPolicy.
type LoginPolicyFunc func(context.Context, LoginRequest) (LoginMode, error)

// ResolveLoginMode implements LoginPolicy.
func (fn LoginPolicyFunc) ResolveLoginMode(ctx context.Context, req LoginRequest) (LoginMode, error) {
	return fn(ctx, req)
}

// LoginDecision is the pre-encryption authentication decision for a login.
type LoginDecision struct {
	Mode LoginMode

	// Profile optionally overrides the runtime identity for offline mode.
	// Nil keeps the default OfflineLoginPlayer(req) behavior.
	Profile *LoginProfile
}

// LoginProfile is an application-provided runtime identity for offline-mode
// logins.
type LoginProfile struct {
	Name       string
	UUID       string
	Properties []ProfileProperty
}

// LoginPolicyV2 decides the login mode and optional offline runtime identity
// before online-mode session proof is requested.
type LoginPolicyV2 interface {
	ResolveLogin(ctx context.Context, req LoginRequest) (LoginDecision, error)
}

// LoginPolicyV2Func adapts a function to LoginPolicyV2.
type LoginPolicyV2Func func(context.Context, LoginRequest) (LoginDecision, error)

// ResolveLogin implements LoginPolicyV2.
func (fn LoginPolicyV2Func) ResolveLogin(ctx context.Context, req LoginRequest) (LoginDecision, error) {
	return fn(ctx, req)
}

// SessionProof is the proof produced by Minecraft online-mode encryption login.
type SessionProof struct {
	Username        string
	ServerID        string
	RemoteIP        string
	ProtocolVersion int
	RequestedHost   string
}

// SessionVerifier verifies online-mode session proof against Mojang,
// custom-Yggdrasil, or application policy.
type SessionVerifier interface {
	VerifySession(ctx context.Context, proof SessionProof) (VerifiedProfile, error)
}

// SessionVerifierFunc adapts a function to SessionVerifier.
type SessionVerifierFunc func(context.Context, SessionProof) (VerifiedProfile, error)

// VerifySession implements SessionVerifier.
func (fn SessionVerifierFunc) VerifySession(ctx context.Context, proof SessionProof) (VerifiedProfile, error) {
	return fn(ctx, proof)
}

// YggdrasilVerifierConfig configures the built-in sessionserver verifier.
type YggdrasilVerifierConfig struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewYggdrasilVerifier returns a verifier for Mojang or a custom Yggdrasil
// sessionserver. Empty BaseURL means Mojang official.
func NewYggdrasilVerifier(cfg YggdrasilVerifierConfig) SessionVerifier {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	source := AuthSourceCustomYggdrasil
	if baseURL == "" {
		baseURL = DefaultYggdrasilSessionServer
		source = AuthSourceMojang
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return yggdrasilVerifier{baseURL: baseURL, client: client, source: source}
}

type yggdrasilVerifier struct {
	baseURL string
	client  *http.Client
	source  string
}

func (v yggdrasilVerifier) VerifySession(ctx context.Context, proof SessionProof) (VerifiedProfile, error) {
	endpoint, err := url.Parse(v.baseURL + "/session/minecraft/hasJoined")
	if err != nil {
		return VerifiedProfile{}, err
	}
	query := endpoint.Query()
	query.Set("username", proof.Username)
	query.Set("serverId", proof.ServerID)
	if proof.RemoteIP != "" {
		query.Set("ip", proof.RemoteIP)
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return VerifiedProfile{}, err
	}
	res, err := v.client.Do(req)
	if err != nil {
		return VerifiedProfile{}, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusNotFound {
		return VerifiedProfile{}, fmt.Errorf("%w: session not joined", ErrInvalidLogin)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return VerifiedProfile{}, fmt.Errorf("%w: sessionserver status %d", ErrSessionUnavailable, res.StatusCode)
	}
	var payload yggdrasilHasJoinedResponse
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return VerifiedProfile{}, err
	}
	profile := VerifiedProfile{
		UUID:       formatUndashedUUID(payload.ID),
		Name:       payload.Name,
		Properties: payload.Properties,
		Source:     v.source,
		Verified:   true,
	}
	if profile.UUID == "" || profile.Name == "" {
		return VerifiedProfile{}, fmt.Errorf("%w: incomplete sessionserver profile", ErrInvalidLogin)
	}
	return profile, nil
}

type yggdrasilHasJoinedResponse struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Properties []ProfileProperty `json:"properties"`
}

// VerifiedProfile is the authenticated profile returned by a SessionVerifier.
type VerifiedProfile struct {
	UUID       string
	Name       string
	Properties []ProfileProperty
	Source     string
	Verified   bool
}

// ProfileProperty is a Mojang/Yggdrasil profile property, usually textures.
type ProfileProperty struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Signature string `json:"signature,omitempty"`
}

// OfflineLoginPlayer returns the default claimed-only Player for offline mode.
func OfflineLoginPlayer(req LoginRequest) Player {
	return Player{
		Name:            req.Username,
		UUID:            OfflineUUID(req.Username),
		ProtocolVersion: req.ProtocolVersion,
		RemoteAddr:      req.RemoteAddr,
		RequestedHost:   req.RequestedHost,
		LoginMode:       LoginModeOffline,
		AuthSource:      AuthSourceOffline,
		Verified:        false,
		Properties:      map[string]string{},
	}
}

// OfflineLoginPlayerWithProfile returns an unverified offline-mode Player using
// an application-provided runtime identity.
func OfflineLoginPlayerWithProfile(req LoginRequest, profile LoginProfile) Player {
	name := profile.Name
	if name == "" {
		name = req.Username
	}
	uuid := profile.UUID
	if uuid == "" {
		uuid = OfflineUUID(name)
	}
	properties := cloneProfileProperties(profile.Properties)
	return Player{
		Name:              name,
		UUID:              uuid,
		ProtocolVersion:   req.ProtocolVersion,
		RemoteAddr:        req.RemoteAddr,
		RequestedHost:     req.RequestedHost,
		LoginMode:         LoginModeOffline,
		AuthSource:        AuthSourceOffline,
		Verified:          false,
		Properties:        profilePropertiesMap(properties),
		ProfileProperties: properties,
	}
}

// VerifiedLoginPlayer returns the default Player for a verified online session.
func VerifiedLoginPlayer(req LoginRequest, profile VerifiedProfile) Player {
	return Player{
		Name:              profile.Name,
		UUID:              profile.UUID,
		ProtocolVersion:   req.ProtocolVersion,
		RemoteAddr:        req.RemoteAddr,
		RequestedHost:     req.RequestedHost,
		LoginMode:         LoginModeOnline,
		AuthSource:        profile.Source,
		Verified:          profile.Verified,
		Properties:        profilePropertiesMap(profile.Properties),
		ProfileProperties: cloneProfileProperties(profile.Properties),
	}
}

// OfflineUUID returns the vanilla offline-mode UUID for a claimed username.
func OfflineUUID(username string) string {
	sum := md5.Sum([]byte("OfflinePlayer:" + username))
	sum[6] = (sum[6] & 0x0f) | 0x30
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		sum[0:4],
		sum[4:6],
		sum[6:8],
		sum[8:10],
		sum[10:16],
	)
}

func profilePropertiesMap(properties []ProfileProperty) map[string]string {
	out := make(map[string]string, len(properties))
	for _, property := range properties {
		out[property.Name] = property.Value
	}
	return out
}

func cloneProfileProperties(in []ProfileProperty) []ProfileProperty {
	out := make([]ProfileProperty, len(in))
	copy(out, in)
	return out
}

func formatUndashedUUID(value string) string {
	clean := strings.ReplaceAll(strings.ToLower(value), "-", "")
	if len(clean) != 32 {
		return value
	}
	return clean[0:8] + "-" + clean[8:12] + "-" + clean[12:16] + "-" + clean[16:20] + "-" + clean[20:32]
}
