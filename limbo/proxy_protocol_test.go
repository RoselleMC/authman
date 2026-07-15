package limbgo

import "testing"

func TestProxyProtocolRequiredImpliesEnabled(t *testing.T) {
	runtime, err := newProxyProtocolRuntime(ProxyProtocolConfig{Required: true})
	if err != nil {
		t.Fatalf("proxy protocol runtime: %v", err)
	}
	if !runtime.enabled || !runtime.required {
		t.Fatalf("runtime = %+v, want enabled and required", runtime)
	}
}

func TestProxyProtocolDisabledIgnoresTrustedProxyParsing(t *testing.T) {
	if _, err := newProxyProtocolRuntime(ProxyProtocolConfig{TrustedProxies: []string{"not-an-ip"}}); err != nil {
		t.Fatalf("disabled proxy protocol parsed trusted proxies: %v", err)
	}
}

func TestProxyProtocolEmptyTrustedProxiesTrustsAllByDefault(t *testing.T) {
	runtime, err := newProxyProtocolRuntime(ProxyProtocolConfig{Required: true})
	if err != nil {
		t.Fatalf("proxy protocol runtime: %v", err)
	}
	if runtime.hasTrustedList {
		t.Fatalf("runtime.hasTrustedList = true, want false")
	}
}

func TestProxyProtocolRestrictTrustedProxiesWithEmptyListTrustsNone(t *testing.T) {
	runtime, err := newProxyProtocolRuntime(ProxyProtocolConfig{
		Required:               true,
		RestrictTrustedProxies: true,
	})
	if err != nil {
		t.Fatalf("proxy protocol runtime: %v", err)
	}
	if !runtime.hasTrustedList {
		t.Fatalf("runtime.hasTrustedList = false, want true")
	}
	if runtime.trusts(testAddr("127.0.0.1:25565")) {
		t.Fatalf("empty restricted trusted proxy list trusted 127.0.0.1")
	}
}

type testAddr string

func (addr testAddr) Network() string { return "tcp" }

func (addr testAddr) String() string { return string(addr) }
