package limbgo

import (
	"context"
	"errors"
	"testing"

	"go.minekube.com/common/minecraft/component"
)

func TestProtocolRangePolicy(t *testing.T) {
	policy := ProtocolRangePolicy(770, 774, &component.Text{Content: "Use 1.21.5-1.21.11"})
	if err := policy.AllowProtocol(context.Background(), ProtocolRequest{ProtocolVersion: 774}); err != nil {
		t.Fatalf("allow protocol 774: %v", err)
	}
	err := policy.AllowProtocol(context.Background(), ProtocolRequest{ProtocolVersion: 769})
	if !errors.Is(err, ErrProtocolRejected) {
		t.Fatalf("reject protocol 769 error = %v, want protocol rejected", err)
	}
	reason, ok := ProtocolRejection(err)
	if !ok {
		t.Fatalf("protocol rejection not extractable")
	}
	text, ok := reason.(*component.Text)
	if !ok || text.Content != "Use 1.21.5-1.21.11" {
		t.Fatalf("rejection reason = %#v", reason)
	}
}
