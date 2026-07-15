package pack

import (
	"fmt"
	"sync/atomic"
)

// Source provides one immutable pack snapshot to a newly accepted connection.
type Source interface {
	ProtocolPack() (*Pack, error)
}

// Store atomically swaps protocol packs. Existing connections retain their
// previous *Pack while later connections observe the new one.
type Store struct {
	current atomic.Pointer[Pack]
}

func NewStore(initial *Pack) (*Store, error) {
	if initial == nil {
		return nil, fmt.Errorf("protocol pack is nil")
	}
	store := &Store{}
	store.current.Store(initial)
	return store, nil
}

func NewStoreFromZip(raw []byte) (*Store, error) {
	loaded, err := LoadZip(raw)
	if err != nil {
		return nil, err
	}
	return NewStore(loaded)
}

func (s *Store) ProtocolPack() (*Pack, error) {
	if s == nil {
		return nil, fmt.Errorf("protocol pack store is nil")
	}
	loaded := s.current.Load()
	if loaded == nil {
		return nil, fmt.Errorf("protocol pack store is empty")
	}
	return loaded, nil
}

func (s *Store) UpdateZip(raw []byte) (Metadata, error) {
	if s == nil {
		return Metadata{}, fmt.Errorf("protocol pack store is nil")
	}
	loaded, err := LoadZip(raw)
	if err != nil {
		return Metadata{}, err
	}
	return s.Update(loaded)
}

// Update atomically activates an already validated immutable pack.
func (s *Store) Update(loaded *Pack) (Metadata, error) {
	if s == nil {
		return Metadata{}, fmt.Errorf("protocol pack store is nil")
	}
	if loaded == nil {
		return Metadata{}, fmt.Errorf("protocol pack is nil")
	}
	s.current.Store(loaded)
	return loaded.Metadata(), nil
}
