package pack

import (
	_ "embed"
	"sync"
)

//go:embed default_protocols.zip
var embeddedDefaultZip []byte

var (
	defaultOnce sync.Once
	defaultPack *Pack
	defaultErr  error
)

// Default returns the protocol pack embedded in this Authman build.
func Default() (*Pack, error) {
	defaultOnce.Do(func() {
		defaultPack, defaultErr = LoadZip(embeddedDefaultZip)
	})
	return defaultPack, defaultErr
}

// DefaultZip returns a copy of the distributable embedded protocol artifact.
func DefaultZip() []byte {
	return append([]byte(nil), embeddedDefaultZip...)
}

func NewDefaultStore() (*Store, error) {
	loaded, err := Default()
	if err != nil {
		return nil, err
	}
	return NewStore(loaded)
}
