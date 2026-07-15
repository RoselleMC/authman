package limbgo

import (
	"context"
	"errors"
	"fmt"
	"net"

	"go.minekube.com/common/minecraft/component"
)

// ProtocolRequest is available immediately after the Minecraft handshake.
type ProtocolRequest struct {
	ProtocolVersion int
	RemoteAddr      net.Addr
	RequestedHost   string
	ProtocolKnown   bool
	PortalDialog    bool
}

// ProtocolPolicy decides whether a login connection may continue for the
// requested Minecraft protocol.
type ProtocolPolicy interface {
	AllowProtocol(ctx context.Context, req ProtocolRequest) error
}

// ProtocolPolicyFunc adapts a function to ProtocolPolicy.
type ProtocolPolicyFunc func(context.Context, ProtocolRequest) error

// AllowProtocol implements ProtocolPolicy.
func (fn ProtocolPolicyFunc) AllowProtocol(ctx context.Context, req ProtocolRequest) error {
	return fn(ctx, req)
}

// ProtocolRejectError rejects a connection and carries the client-facing login
// disconnect reason.
type ProtocolRejectError struct {
	Reason component.Component
}

// Error implements error.
func (e ProtocolRejectError) Error() string {
	return ErrProtocolRejected.Error()
}

// Unwrap returns the sentinel rejection error.
func (e ProtocolRejectError) Unwrap() error {
	return ErrProtocolRejected
}

// RejectProtocol returns an error that asks protocol routers to send a rich
// login disconnect reason to the client.
func RejectProtocol(reason component.Component) error {
	if reason == nil {
		reason = &component.Text{Content: "Unsupported client version"}
	}
	return ProtocolRejectError{Reason: reason}
}

// RejectProtocolText is a convenience wrapper for plain-text disconnects.
func RejectProtocolText(message string) error {
	return RejectProtocol(&component.Text{Content: message})
}

// ProtocolRejection extracts a client-facing rejection reason from err.
func ProtocolRejection(err error) (component.Component, bool) {
	var rejection ProtocolRejectError
	if errors.As(err, &rejection) {
		return rejection.Reason, true
	}
	return nil, false
}

// ProtocolRangePolicy allows only an inclusive protocol range.
func ProtocolRangePolicy(minProtocol int, maxProtocol int, reason component.Component) ProtocolPolicy {
	return ProtocolPolicyFunc(func(_ context.Context, req ProtocolRequest) error {
		if req.ProtocolVersion >= minProtocol && req.ProtocolVersion <= maxProtocol {
			return nil
		}
		if reason != nil {
			return RejectProtocol(reason)
		}
		return RejectProtocolText(fmt.Sprintf("Unsupported client protocol %d", req.ProtocolVersion))
	})
}
