package node

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RoselleMC/authman/core/internal/auth"
)

type Node struct {
	ID                  string
	ServerID            string
	Mode                string
	Name                string
	TokenHash           string
	TokenFingerprint    string
	InstanceFingerprint string
	PluginVersion       string
	VelocityVersion     string
	Disabled            bool
	RuntimeConfig       map[string]any
	CreatedAt           time.Time
	LastHeartbeatAt     *time.Time
}

type Registration struct {
	Mode                string
	Kind                string
	Name                string
	ServerID            string
	InstanceFingerprint string
	AccessFingerprint   string
	PluginVersion       string
	VelocityVersion     string
}

type Registry struct {
	mu     sync.RWMutex
	nextID int
	nodes  map[string]Node
}

func NewRegistry() *Registry {
	return &Registry{nodes: make(map[string]Node)}
}

func (r *Registry) Create(ctx context.Context, name string, now time.Time) (Node, string, error) {
	return r.CreateKind(ctx, name, "downstream_velocity", now)
}

func (r *Registry) CreateKind(ctx context.Context, name string, kind string, now time.Time) (Node, string, error) {
	return r.CreateKindForServer(ctx, name, kind, "", now)
}

func (r *Registry) CreateKindForServer(ctx context.Context, name string, kind string, serverID string, now time.Time) (Node, string, error) {
	if name == "" {
		return Node{}, "", fmt.Errorf("node name is required")
	}
	kind = NormalizeKind(kind)
	if strings.TrimSpace(serverID) == "" {
		serverID = "default"
	}
	token, err := auth.NewOpaqueToken(32)
	if err != nil {
		return Node{}, "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if kind == "downstream_velocity" {
		for _, existing := range r.nodes {
			if !existing.Disabled && NormalizeKind(existing.Mode) == "downstream_velocity" && existing.ServerID == serverID {
				return Node{}, "", fmt.Errorf("downstream server already has a node")
			}
		}
	}
	r.nextID++
	node := Node{
		ID:               fmt.Sprintf("node-%d", r.nextID),
		ServerID:         serverID,
		Mode:             kind,
		Name:             name,
		TokenHash:        auth.HashToken("node", token),
		TokenFingerprint: auth.TokenFingerprint(token),
		RuntimeConfig:    map[string]any{},
		CreatedAt:        now.UTC(),
	}
	r.nodes[node.ID] = node
	return node, token, nil
}

func (r *Registry) Authenticate(ctx context.Context, token string) (Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, node := range r.nodes {
		if node.Disabled {
			continue
		}
		if auth.ConstantTimeTokenEqual("node", token, node.TokenHash) {
			return node, nil
		}
	}
	return Node{}, fmt.Errorf("node token is invalid")
}

func (r *Registry) Rotate(ctx context.Context, id string, now time.Time) (Node, string, error) {
	token, err := auth.NewOpaqueToken(32)
	if err != nil {
		return Node{}, "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[id]
	if !ok {
		return Node{}, "", fmt.Errorf("node not found")
	}
	if node.Disabled {
		return Node{}, "", fmt.Errorf("node is revoked")
	}
	node.TokenHash = auth.HashToken("node", token)
	node.TokenFingerprint = auth.TokenFingerprint(token)
	r.nodes[id] = node
	return node, token, nil
}

func (r *Registry) Heartbeat(ctx context.Context, token string, now time.Time) (Node, error) {
	node, err := r.Authenticate(ctx, token)
	if err != nil {
		return Node{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now = now.UTC()
	node.LastHeartbeatAt = &now
	r.nodes[node.ID] = node
	return node, nil
}

func (r *Registry) Register(ctx context.Context, registration Registration, now time.Time) (Node, error) {
	if registration.InstanceFingerprint == "" {
		return Node{}, fmt.Errorf("instance fingerprint is required")
	}
	name := registration.Name
	if name == "" {
		name = "node-" + registration.InstanceFingerprint[:min(8, len(registration.InstanceFingerprint))]
	}
	kind := NormalizeKind(registration.Kind)
	if registration.Kind == "" {
		kind = NormalizeKind(registration.Mode)
	}
	serverID := registration.ServerID
	if serverID == "" {
		serverID = "default"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now = now.UTC()
	for id, node := range r.nodes {
		if node.InstanceFingerprint == registration.InstanceFingerprint {
			if node.Disabled {
				return Node{}, fmt.Errorf("node is revoked")
			}
			node.Name = name
			node.Mode = kind
			node.ServerID = serverID
			node.TokenFingerprint = registration.AccessFingerprint
			node.PluginVersion = registration.PluginVersion
			node.VelocityVersion = registration.VelocityVersion
			node.LastHeartbeatAt = &now
			r.nodes[id] = node
			return node, nil
		}
	}
	if kind == "downstream_velocity" {
		for _, existing := range r.nodes {
			if !existing.Disabled && NormalizeKind(existing.Mode) == "downstream_velocity" && existing.ServerID == serverID {
				return Node{}, fmt.Errorf("downstream server already has a node")
			}
		}
	}
	r.nextID++
	node := Node{
		ID:                  fmt.Sprintf("node-%d", r.nextID),
		ServerID:            serverID,
		Mode:                kind,
		Name:                name,
		TokenFingerprint:    registration.AccessFingerprint,
		InstanceFingerprint: registration.InstanceFingerprint,
		PluginVersion:       registration.PluginVersion,
		VelocityVersion:     registration.VelocityVersion,
		RuntimeConfig:       map[string]any{},
		CreatedAt:           now,
		LastHeartbeatAt:     &now,
	}
	r.nodes[node.ID] = node
	return node, nil
}

func NormalizeMode(value string) string {
	return NormalizeKind(value)
}

func NormalizeKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "limbo", "limbo_portal", "portal":
		return "limbo_portal"
	case "downstream", "downstream_velocity", "velocity", "gate":
		return "downstream_velocity"
	default:
		return "downstream_velocity"
	}
}

func IsLimboPortal(value string) bool {
	return NormalizeKind(value) == "limbo_portal"
}

func (r *Registry) List(ctx context.Context) []Node {
	r.mu.RLock()
	defer r.mu.RUnlock()
	nodes := make([]Node, 0, len(r.nodes))
	for _, node := range r.nodes {
		nodes = append(nodes, cloneNode(node))
	}
	return nodes
}

func (r *Registry) Get(ctx context.Context, id string) (Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	node, ok := r.nodes[id]
	if !ok {
		return Node{}, fmt.Errorf("node not found")
	}
	return cloneNode(node), nil
}

func (r *Registry) Update(ctx context.Context, id string, name string, runtime map[string]any) (Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[id]
	if !ok {
		return Node{}, fmt.Errorf("node not found")
	}
	if strings.TrimSpace(name) != "" {
		node.Name = strings.TrimSpace(name)
	}
	node.RuntimeConfig = CloneRuntimeConfig(runtime)
	r.nodes[id] = node
	return cloneNode(node), nil
}

func (r *Registry) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[id]
	if !ok {
		return fmt.Errorf("node not found")
	}
	node.Disabled = true
	node.TokenHash = ""
	node.TokenFingerprint = ""
	r.nodes[id] = node
	return nil
}

func CloneRuntimeConfig(value map[string]any) map[string]any {
	if len(value) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

func cloneNode(n Node) Node {
	n.RuntimeConfig = CloneRuntimeConfig(n.RuntimeConfig)
	return n
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
