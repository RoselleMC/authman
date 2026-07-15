package limbgo

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultProxyProtocolHeaderTimeout = 5 * time.Second

var (
	errNoProxyProtocol          = errors.New("limbgo: no proxy protocol header")
	errUntrustedProxyProtocol   = errors.New("limbgo: untrusted proxy protocol header")
	proxyProtocolV2Signature    = []byte{'\r', '\n', '\r', '\n', 0, '\r', '\n', 'Q', 'U', 'I', 'T', '\n'}
	proxyProtocolV1MaxLineBytes = 107
)

// ProxyProtocolConfig controls optional HAProxy PROXY protocol handling.
//
// Enable this when limbgo is behind a trusted TCP proxy such as Gate lite with
// route.proxyProtocol enabled. TrustedProxies should normally contain the proxy
// container, host, or subnet address so public clients cannot spoof RemoteAddr.
// When RestrictTrustedProxies is true, an empty TrustedProxies list rejects all
// upstreams instead of falling back to trusting every upstream.
type ProxyProtocolConfig struct {
	Enabled                bool
	Required               bool
	TrustedProxies         []string
	RestrictTrustedProxies bool
	ReadHeaderTimeout      time.Duration
}

type proxyProtocolRuntime struct {
	enabled        bool
	required       bool
	trusted        []net.IPNet
	headerTimeout  time.Duration
	hasTrustedList bool
}

// WrapProxyProtocolConn wraps conn so RemoteAddr returns the address from a
// trusted PROXY header. If cfg is disabled, conn is returned unchanged.
func WrapProxyProtocolConn(conn net.Conn, cfg ProxyProtocolConfig) (net.Conn, error) {
	runtime, err := newProxyProtocolRuntime(cfg)
	if err != nil {
		return nil, err
	}
	return runtime.wrap(conn)
}

func newProxyProtocolRuntime(cfg ProxyProtocolConfig) (proxyProtocolRuntime, error) {
	runtime := proxyProtocolRuntime{
		enabled:       cfg.Enabled || cfg.Required,
		required:      cfg.Required,
		headerTimeout: cfg.ReadHeaderTimeout,
	}
	if !runtime.enabled {
		return runtime, nil
	}
	if runtime.headerTimeout == 0 {
		runtime.headerTimeout = defaultProxyProtocolHeaderTimeout
	}
	if runtime.headerTimeout < 0 {
		runtime.headerTimeout = 0
	}
	for _, trusted := range cfg.TrustedProxies {
		network, err := parseTrustedProxy(trusted)
		if err != nil {
			return proxyProtocolRuntime{}, err
		}
		runtime.trusted = append(runtime.trusted, network)
	}
	runtime.hasTrustedList = cfg.RestrictTrustedProxies || len(runtime.trusted) > 0
	return runtime, nil
}

func (runtime proxyProtocolRuntime) wrap(conn net.Conn) (net.Conn, error) {
	if !runtime.enabled {
		return conn, nil
	}
	trusted := !runtime.hasTrustedList || runtime.trusts(conn.RemoteAddr())
	if runtime.required && !trusted {
		return nil, fmt.Errorf("%w: upstream %s", errUntrustedProxyProtocol, conn.RemoteAddr())
	}
	return &proxyProtocolConn{
		Conn:          conn,
		reader:        bufio.NewReader(conn),
		required:      runtime.required,
		trusted:       trusted,
		headerTimeout: runtime.headerTimeout,
	}, nil
}

func (runtime proxyProtocolRuntime) trusts(addr net.Addr) bool {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, trusted := range runtime.trusted {
		if trusted.Contains(ip) {
			return true
		}
	}
	return false
}

type proxyProtocolConn struct {
	net.Conn
	reader        *bufio.Reader
	required      bool
	trusted       bool
	headerTimeout time.Duration

	once          sync.Once
	headerErr     error
	sourceAddr    net.Addr
	destAddr      net.Addr
	readDeadline  time.Time
	writeDeadline time.Time
}

func (conn *proxyProtocolConn) Read(p []byte) (int, error) {
	if err := conn.ensureHeader(); err != nil {
		return 0, err
	}
	return conn.reader.Read(p)
}

func (conn *proxyProtocolConn) RemoteAddr() net.Addr {
	_ = conn.ensureHeader()
	if conn.headerErr == nil && conn.sourceAddr != nil {
		return conn.sourceAddr
	}
	return conn.Conn.RemoteAddr()
}

func (conn *proxyProtocolConn) LocalAddr() net.Addr {
	_ = conn.ensureHeader()
	if conn.headerErr == nil && conn.destAddr != nil {
		return conn.destAddr
	}
	return conn.Conn.LocalAddr()
}

func (conn *proxyProtocolConn) SetDeadline(t time.Time) error {
	conn.readDeadline = t
	conn.writeDeadline = t
	return conn.Conn.SetDeadline(t)
}

func (conn *proxyProtocolConn) SetReadDeadline(t time.Time) error {
	conn.readDeadline = t
	return conn.Conn.SetReadDeadline(t)
}

func (conn *proxyProtocolConn) SetWriteDeadline(t time.Time) error {
	conn.writeDeadline = t
	return conn.Conn.SetWriteDeadline(t)
}

func (conn *proxyProtocolConn) ensureHeader() error {
	conn.once.Do(func() {
		conn.headerErr = conn.readProxyHeader()
	})
	if conn.headerErr != nil {
		return conn.headerErr
	}
	return nil
}

func (conn *proxyProtocolConn) readProxyHeader() error {
	if conn.headerTimeout > 0 {
		if err := conn.Conn.SetReadDeadline(time.Now().Add(conn.headerTimeout)); err != nil {
			return err
		}
		defer func() {
			_ = conn.Conn.SetReadDeadline(conn.readDeadline)
		}()
	}

	source, dest, ok, err := readProxyProtocolHeader(conn.reader)
	if err != nil {
		return err
	}
	if !ok {
		if conn.required {
			return errNoProxyProtocol
		}
		return nil
	}
	if !conn.trusted {
		return errUntrustedProxyProtocol
	}
	conn.sourceAddr = source
	conn.destAddr = dest
	return nil
}

func readProxyProtocolHeader(reader *bufio.Reader) (net.Addr, net.Addr, bool, error) {
	first, err := reader.Peek(1)
	if err != nil {
		return nil, nil, false, err
	}
	switch first[0] {
	case 'P':
		return readProxyProtocolV1Header(reader)
	case '\r':
		return readProxyProtocolV2Header(reader)
	default:
		return nil, nil, false, nil
	}
}

func readProxyProtocolV1Header(reader *bufio.Reader) (net.Addr, net.Addr, bool, error) {
	prefix, err := reader.Peek(len("PROXY "))
	if err != nil {
		return nil, nil, false, err
	}
	if string(prefix) != "PROXY " {
		return nil, nil, false, nil
	}

	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, nil, false, err
	}
	if len(line) > proxyProtocolV1MaxLineBytes {
		return nil, nil, false, fmt.Errorf("limbgo: proxy protocol v1 header too long")
	}
	if !strings.HasSuffix(line, "\r\n") {
		return nil, nil, false, fmt.Errorf("limbgo: proxy protocol v1 header missing CRLF")
	}

	fields := strings.Fields(strings.TrimSuffix(line, "\r\n"))
	if len(fields) < 2 || fields[0] != "PROXY" {
		return nil, nil, false, fmt.Errorf("limbgo: malformed proxy protocol v1 header")
	}
	if fields[1] == "UNKNOWN" {
		return nil, nil, true, nil
	}
	if len(fields) != 6 {
		return nil, nil, false, fmt.Errorf("limbgo: malformed proxy protocol v1 address fields")
	}
	if fields[1] != "TCP4" && fields[1] != "TCP6" {
		return nil, nil, false, fmt.Errorf("limbgo: unsupported proxy protocol v1 family %q", fields[1])
	}

	source, err := parseProxyTCPAddr(fields[2], fields[4])
	if err != nil {
		return nil, nil, false, err
	}
	dest, err := parseProxyTCPAddr(fields[3], fields[5])
	if err != nil {
		return nil, nil, false, err
	}
	return source, dest, true, nil
}

func readProxyProtocolV2Header(reader *bufio.Reader) (net.Addr, net.Addr, bool, error) {
	signature, err := reader.Peek(len(proxyProtocolV2Signature))
	if err != nil {
		return nil, nil, false, err
	}
	if string(signature) != string(proxyProtocolV2Signature) {
		return nil, nil, false, nil
	}

	header, err := reader.Peek(16)
	if err != nil {
		return nil, nil, false, err
	}
	versionCommand := header[12]
	if versionCommand>>4 != 2 {
		return nil, nil, false, fmt.Errorf("limbgo: unsupported proxy protocol version %d", versionCommand>>4)
	}
	command := versionCommand & 0x0f
	familyProtocol := header[13]
	length := int(binary.BigEndian.Uint16(header[14:16]))
	if _, err := reader.Discard(16); err != nil {
		return nil, nil, false, err
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, nil, false, err
	}
	if command == 0 {
		return nil, nil, true, nil
	}
	if command != 1 {
		return nil, nil, false, fmt.Errorf("limbgo: unsupported proxy protocol v2 command %d", command)
	}

	switch familyProtocol {
	case 0x11:
		if len(payload) < 12 {
			return nil, nil, false, fmt.Errorf("limbgo: truncated proxy protocol v2 IPv4 address")
		}
		source := &net.TCPAddr{
			IP:   net.IPv4(payload[0], payload[1], payload[2], payload[3]),
			Port: int(binary.BigEndian.Uint16(payload[8:10])),
		}
		dest := &net.TCPAddr{
			IP:   net.IPv4(payload[4], payload[5], payload[6], payload[7]),
			Port: int(binary.BigEndian.Uint16(payload[10:12])),
		}
		return source, dest, true, nil
	case 0x21:
		if len(payload) < 36 {
			return nil, nil, false, fmt.Errorf("limbgo: truncated proxy protocol v2 IPv6 address")
		}
		source := &net.TCPAddr{
			IP:   append(net.IP(nil), payload[0:16]...),
			Port: int(binary.BigEndian.Uint16(payload[32:34])),
		}
		dest := &net.TCPAddr{
			IP:   append(net.IP(nil), payload[16:32]...),
			Port: int(binary.BigEndian.Uint16(payload[34:36])),
		}
		return source, dest, true, nil
	case 0x00:
		return nil, nil, true, nil
	default:
		return nil, nil, false, fmt.Errorf("limbgo: unsupported proxy protocol v2 family/protocol 0x%02x", familyProtocol)
	}
}

func parseProxyTCPAddr(host string, portText string) (*net.TCPAddr, error) {
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, fmt.Errorf("limbgo: invalid proxy protocol IP %q", host)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 0 || port > 65535 {
		return nil, fmt.Errorf("limbgo: invalid proxy protocol port %q", portText)
	}
	return &net.TCPAddr{IP: ip, Port: port}, nil
}

func parseTrustedProxy(value string) (net.IPNet, error) {
	if _, network, err := net.ParseCIDR(value); err == nil {
		return *network, nil
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return net.IPNet{}, fmt.Errorf("limbgo: invalid trusted proxy %q", value)
	}
	if v4 := ip.To4(); v4 != nil {
		return net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)}, nil
	}
	return net.IPNet{IP: ip.To16(), Mask: net.CIDRMask(128, 128)}, nil
}
