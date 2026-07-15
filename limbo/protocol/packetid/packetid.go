package packetid

import (
	"fmt"
	"sort"
	"sync"
)

// State is a Minecraft protocol state.
type State string

const (
	StateLogin         State = "login"
	StateConfiguration State = "configuration"
	StatePlay          State = "play"
)

// Direction is a packet direction relative to the server.
type Direction string

const (
	ToClient Direction = "toClient"
	ToServer Direction = "toServer"
)

// Entry is one generated packet ID mapping.
type Entry struct {
	State     State     `json:"state"`
	Direction Direction `json:"direction"`
	Name      string    `json:"name"`
	ID        int32     `json:"id"`
}

// VersionPackets contains generated packet IDs for one protocol version.
type VersionPackets struct {
	MinecraftVersion string  `json:"minecraft_version"`
	Protocol         int32   `json:"protocol"`
	Entries          []Entry `json:"entries"`
}

type packetKey struct {
	state     State
	direction Direction
	name      string
}

type packetIDKey struct {
	state     State
	direction Direction
	id        int32
}

// Table is an immutable, pre-indexed packet ID table. A table is safe to share
// between every connection that was accepted against the same protocol pack.
type Table struct {
	versions map[int32]VersionPackets
	ids      map[int32]map[packetKey]int32
}

var (
	defaultTableOnce sync.Once
	defaultTable     *Table
	defaultTableErr  error
)

// NewTable validates and compiles packet mappings into constant-time lookups.
func NewTable(versions []VersionPackets) (*Table, error) {
	if len(versions) == 0 {
		return nil, fmt.Errorf("packet table is empty")
	}
	table := &Table{
		versions: make(map[int32]VersionPackets, len(versions)),
		ids:      make(map[int32]map[packetKey]int32, len(versions)),
	}
	for _, version := range versions {
		if version.Protocol <= 0 {
			return nil, fmt.Errorf("packet table has invalid protocol %d", version.Protocol)
		}
		if _, exists := table.versions[version.Protocol]; exists {
			return nil, fmt.Errorf("packet table has duplicate protocol %d", version.Protocol)
		}
		if len(version.Entries) == 0 {
			return nil, fmt.Errorf("packet table protocol %d has no entries", version.Protocol)
		}
		entries := append([]Entry(nil), version.Entries...)
		lookup := make(map[packetKey]int32, len(entries))
		seenIDs := make(map[packetIDKey]string, len(entries))
		for _, entry := range entries {
			if err := validateEntry(version.Protocol, entry); err != nil {
				return nil, err
			}
			key := packetKey{state: entry.State, direction: entry.Direction, name: entry.Name}
			if _, exists := lookup[key]; exists {
				return nil, fmt.Errorf("packet table protocol %d has duplicate %s/%s packet %q", version.Protocol, entry.State, entry.Direction, entry.Name)
			}
			idKey := packetIDKey{state: entry.State, direction: entry.Direction, id: entry.ID}
			if existing, exists := seenIDs[idKey]; exists {
				return nil, fmt.Errorf("packet table protocol %d reuses %s/%s id %d for %q and %q", version.Protocol, entry.State, entry.Direction, entry.ID, existing, entry.Name)
			}
			lookup[key] = entry.ID
			seenIDs[idKey] = entry.Name
		}
		version.Entries = entries
		table.versions[version.Protocol] = version
		table.ids[version.Protocol] = lookup
	}
	return table, nil
}

func validateEntry(protocol int32, entry Entry) error {
	switch entry.State {
	case StateLogin, StateConfiguration, StatePlay:
	default:
		return fmt.Errorf("packet table protocol %d packet %q has invalid state %q", protocol, entry.Name, entry.State)
	}
	switch entry.Direction {
	case ToClient, ToServer:
	default:
		return fmt.Errorf("packet table protocol %d packet %q has invalid direction %q", protocol, entry.Name, entry.Direction)
	}
	if entry.Name == "" {
		return fmt.Errorf("packet table protocol %d has an empty packet name", protocol)
	}
	if entry.ID < 0 {
		return fmt.Errorf("packet table protocol %d packet %q has negative id %d", protocol, entry.Name, entry.ID)
	}
	return nil
}

// NewDefaultTable compiles the packet data generated into this binary.
func NewDefaultTable() (*Table, error) {
	return NewTable(Export())
}

// DefaultTable returns the cached table generated into this binary.
func DefaultTable() (*Table, error) {
	defaultTableOnce.Do(func() {
		defaultTable, defaultTableErr = NewDefaultTable()
	})
	return defaultTable, defaultTableErr
}

// Export returns a deterministic deep copy of the generated packet mappings.
func Export() []VersionPackets {
	protocols := make([]int32, 0, len(byProtocol))
	for protocol := range byProtocol {
		protocols = append(protocols, protocol)
	}
	sort.Slice(protocols, func(i, j int) bool { return protocols[i] < protocols[j] })
	out := make([]VersionPackets, 0, len(protocols))
	for _, protocol := range protocols {
		version := byProtocol[protocol]
		version.Entries = append([]Entry(nil), version.Entries...)
		out = append(out, version)
	}
	return out
}

// Lookup returns packet mappings for protocol.
func (t *Table) Lookup(protocol int32) (VersionPackets, bool) {
	if t == nil {
		return VersionPackets{}, false
	}
	packets, ok := t.versions[protocol]
	if !ok {
		return VersionPackets{}, false
	}
	packets.Entries = append([]Entry(nil), packets.Entries...)
	return packets, true
}

// ID resolves one packet ID in this immutable table.
func (t *Table) ID(protocol int32, state State, direction Direction, name string) (int32, bool) {
	if t == nil {
		return 0, false
	}
	ids, ok := t.ids[protocol]
	if !ok {
		return 0, false
	}
	id, ok := ids[packetKey{state: state, direction: direction, name: name}]
	return id, ok
}

// Protocols returns all protocols with packet mappings in ascending order.
func (t *Table) Protocols() []int32 {
	if t == nil {
		return nil
	}
	protocols := make([]int32, 0, len(t.versions))
	for protocol := range t.versions {
		protocols = append(protocols, protocol)
	}
	sort.Slice(protocols, func(i, j int) bool { return protocols[i] < protocols[j] })
	return protocols
}

// Lookup returns packet mappings for protocol.
func Lookup(protocol int32) (VersionPackets, bool) {
	packets, ok := byProtocol[protocol]
	if !ok {
		alias, aliasOK := aliasProtocol(protocol)
		if !aliasOK {
			return VersionPackets{}, false
		}
		packets, ok = byProtocol[alias]
	}
	return packets, ok
}

// ID resolves one packet ID.
func ID(protocol int32, state State, direction Direction, name string) (int32, bool) {
	packets, ok := Lookup(protocol)
	if !ok {
		return 0, false
	}
	for _, entry := range packets.Entries {
		if entry.State == state && entry.Direction == direction && entry.Name == name {
			return entry.ID, true
		}
	}
	return 0, false
}

func aliasProtocol(protocol int32) (int32, bool) {
	switch protocol {
	case 316:
		return 315, true
	case 753, 754:
		return 751, true
	default:
		return 0, false
	}
}
