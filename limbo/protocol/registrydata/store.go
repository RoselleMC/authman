package registrydata

import (
	"fmt"
	"os"
	"sync"
)

// Store is a hot-swappable registry data source.
//
// Existing connections keep the Data snapshot they already received. Updating
// the store affects later connections without requiring the limbo server to be
// restarted.
type Store struct {
	mu   sync.RWMutex
	data *Data
}

func NewStore(data *Data) (*Store, error) {
	if data == nil {
		return nil, fmt.Errorf("registry data is nil")
	}
	return &Store{data: data}, nil
}

func NewDefaultStore() (*Store, error) {
	data, err := Default()
	if err != nil {
		return nil, err
	}
	return NewStore(data)
}

func NewStoreFromZip(raw []byte) (*Store, error) {
	data, err := LoadZipBytes(raw)
	if err != nil {
		return nil, err
	}
	return NewStore(data)
}

func NewStoreFromZipFile(path string) (*Store, error) {
	data, err := LoadZipFile(path)
	if err != nil {
		return nil, err
	}
	return NewStore(data)
}

func (s *Store) RegistryData() (*Data, error) {
	if s == nil {
		return nil, fmt.Errorf("registry data store is nil")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.data == nil {
		return nil, fmt.Errorf("registry data store is empty")
	}
	return s.data, nil
}

func (s *Store) Update(data *Data) error {
	if s == nil {
		return fmt.Errorf("registry data store is nil")
	}
	if data == nil {
		return fmt.Errorf("registry data is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = data
	return nil
}

func (s *Store) UpdateZip(raw []byte) error {
	data, err := LoadZipBytes(raw)
	if err != nil {
		return err
	}
	return s.Update(data)
}

func (s *Store) UpdateZipFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return s.UpdateZip(raw)
}
