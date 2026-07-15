package limbgo

import "errors"

var (
	ErrMissingProtocolRouter = errors.New("limbgo: missing protocol router")
	ErrMissingWorldProvider  = errors.New("limbgo: missing world provider")
	ErrMissingSpawnResolver  = errors.New("limbgo: missing spawn resolver")
	ErrInvalidLogin          = errors.New("limbgo: invalid login")
	ErrSessionUnavailable    = errors.New("limbgo: session verifier unavailable")
	ErrProtocolRejected      = errors.New("limbgo: protocol rejected")
	ErrMissingWorld          = errors.New("limbgo: missing world")
	ErrUnsupportedCapability = errors.New("limbgo: unsupported session capability")
	ErrInvalidSessionControl = errors.New("limbgo: invalid session control")
	ErrWorldNotFound         = errors.New("limbgo: world not found")
	ErrInvalidSchematic      = errors.New("limbgo: invalid schematic")
)
