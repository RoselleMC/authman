package blockstate

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/RoselleMC/authman/limbo"
)

// Table is an immutable protocol-to-default-block-state lookup.
type Table struct {
	byProtocol map[int32]map[string]uint32
}

var (
	defaultTableOnce sync.Once
	defaultTable     *Table
	defaultTableErr  error
)

// NewTable validates and copies block-state mappings.
func NewTable(values map[int32]map[string]uint32) (*Table, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("block-state table is empty")
	}
	out := &Table{byProtocol: make(map[int32]map[string]uint32, len(values))}
	for protocol, blocks := range values {
		if protocol <= 0 {
			return nil, fmt.Errorf("block-state table has invalid protocol %d", protocol)
		}
		if len(blocks) == 0 {
			return nil, fmt.Errorf("block-state table protocol %d is empty", protocol)
		}
		air, ok := blocks["air"]
		if !ok || air != 0 {
			return nil, fmt.Errorf("block-state table protocol %d must define air as state 0", protocol)
		}
		copyBlocks := make(map[string]uint32, len(blocks))
		for name, state := range blocks {
			name = strings.TrimPrefix(strings.TrimSpace(name), "minecraft:")
			if name == "" {
				return nil, fmt.Errorf("block-state table protocol %d has an empty block name", protocol)
			}
			if _, exists := copyBlocks[name]; exists {
				return nil, fmt.Errorf("block-state table protocol %d has duplicate block %q", protocol, name)
			}
			copyBlocks[name] = state
		}
		out.byProtocol[protocol] = copyBlocks
	}
	return out, nil
}

// NewDefaultTable compiles the generated block-state mappings.
func NewDefaultTable() (*Table, error) {
	return NewTable(Export())
}

// DefaultTable returns the cached generated table.
func DefaultTable() (*Table, error) {
	defaultTableOnce.Do(func() {
		defaultTable, defaultTableErr = NewDefaultTable()
	})
	return defaultTable, defaultTableErr
}

// Export returns a deep copy of all generated mappings.
func Export() map[int32]map[string]uint32 {
	out := make(map[int32]map[string]uint32, len(byProtocol))
	for protocol, blocks := range byProtocol {
		copyBlocks := make(map[string]uint32, len(blocks))
		for name, state := range blocks {
			copyBlocks[name] = state
		}
		out[protocol] = copyBlocks
	}
	return out
}

// Protocols returns all protocols in ascending order.
func (t *Table) Protocols() []int32 {
	if t == nil {
		return nil
	}
	protocols := make([]int32, 0, len(t.byProtocol))
	for protocol := range t.byProtocol {
		protocols = append(protocols, protocol)
	}
	sort.Slice(protocols, func(i, j int) bool { return protocols[i] < protocols[j] })
	return protocols
}

// DefaultState resolves a block state against this table.
func (t *Table) DefaultState(protocol int32, state limbgo.BlockState) (uint32, bool) {
	if t == nil {
		return 0, false
	}
	blocks, ok := t.byProtocol[protocol]
	if !ok {
		return 0, false
	}
	name := strings.TrimPrefix(state.Name, "minecraft:")
	value, ok := blocks[name]
	return value, ok
}

// DefaultState returns the protocol-specific default block-state ID for state.
func DefaultState(protocol int32, state limbgo.BlockState) (uint32, bool) {
	blocks, ok := byProtocol[protocol]
	if !ok {
		alias, aliasOK := aliasProtocol(protocol)
		if !aliasOK {
			return 0, false
		}
		blocks, ok = byProtocol[alias]
	}
	if !ok {
		return 0, false
	}
	name := strings.TrimPrefix(state.Name, "minecraft:")
	value, ok := blocks[name]
	return value, ok
}

func aliasProtocol(protocol int32) (int32, bool) {
	switch protocol {
	case 775:
		return 774, true
	default:
		return 0, false
	}
}
