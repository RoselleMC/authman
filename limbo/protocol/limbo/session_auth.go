package limbo

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"fmt"
	"math/big"
	"net"

	"github.com/RoselleMC/authman/limbo"
	"github.com/RoselleMC/authman/limbo/internal/protocol/wire"
	"github.com/RoselleMC/authman/limbo/protocol/packetid"
)

type onlineSessionAuthLayout struct {
	requestAuthenticate bool
	responseVerifyFlag  bool
}

func sessionAuthLayoutFor(protocol int32, modern bool, cfg modernProtocolConfig) onlineSessionAuthLayout {
	if modern {
		return onlineSessionAuthLayout{
			requestAuthenticate: cfg.encryptionRequestAuthenticate,
			responseVerifyFlag:  cfg.encryptionResponseVerifyFlag,
		}
	}
	return onlineSessionAuthLayout{
		requestAuthenticate: protocol >= protocol766,
		responseVerifyFlag:  protocol == protocol759 || protocol == protocol760,
	}
}

func performOnlineSessionAuth(ctx context.Context, conn net.Conn, reader *bufio.Reader, layout onlineSessionAuthLayout, packetProtocol int32, req limbgo.LoginRequest, verifier limbgo.SessionVerifier, serverID string, packetIDs *packetid.Table) (net.Conn, *bufio.Reader, limbgo.VerifiedProfile, error) {
	if verifier == nil {
		return nil, nil, limbgo.VerifiedProfile{}, fmt.Errorf("%w: missing session verifier", limbgo.ErrInvalidLogin)
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return nil, nil, limbgo.VerifiedProfile{}, err
	}
	publicKey, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, nil, limbgo.VerifiedProfile{}, err
	}
	verifyToken := make([]byte, 16)
	if _, err := rand.Read(verifyToken); err != nil {
		return nil, nil, limbgo.VerifiedProfile{}, err
	}
	if err := writeEncryptionRequest(conn, layout, packetProtocol, serverID, publicKey, verifyToken, packetIDs); err != nil {
		return nil, nil, limbgo.VerifiedProfile{}, err
	}
	response, err := wire.ReadPacket(reader, 0)
	if err != nil {
		return nil, nil, limbgo.VerifiedProfile{}, err
	}
	responseID, ok := resolvePacketID(packetIDs, packetProtocol, packetid.StateLogin, packetid.ToServer, "encryption_begin")
	if !ok || response.ID != responseID {
		return nil, nil, limbgo.VerifiedProfile{}, fmt.Errorf("expected encryption_begin packet %d, got %d", responseID, response.ID)
	}
	sharedSecret, responseToken, err := readEncryptionResponse(layout, response.Data, privateKey)
	if err != nil {
		return nil, nil, limbgo.VerifiedProfile{}, err
	}
	if !bytes.Equal(responseToken, verifyToken) {
		return nil, nil, limbgo.VerifiedProfile{}, fmt.Errorf("%w: online-mode verify token mismatch", limbgo.ErrInvalidLogin)
	}
	proof := limbgo.SessionProof{
		Username:        req.Username,
		ServerID:        minecraftSessionServerID(serverID, sharedSecret, publicKey),
		RemoteIP:        remoteIP(req.RemoteAddr),
		ProtocolVersion: req.ProtocolVersion,
		RequestedHost:   req.RequestedHost,
	}
	encryptedConn, err := newEncryptedConn(conn, sharedSecret)
	if err != nil {
		return nil, nil, limbgo.VerifiedProfile{}, err
	}
	encryptedReader := bufio.NewReader(encryptedConn)
	profile, err := verifier.VerifySession(ctx, proof)
	if err != nil {
		return encryptedConn, encryptedReader, limbgo.VerifiedProfile{}, err
	}
	if profile.UUID == "" || profile.Name == "" || !profile.Verified {
		return encryptedConn, encryptedReader, limbgo.VerifiedProfile{}, fmt.Errorf("%w: verifier returned unverified or incomplete profile", limbgo.ErrInvalidLogin)
	}
	return encryptedConn, encryptedReader, profile, nil
}

func writeEncryptionRequest(conn net.Conn, layout onlineSessionAuthLayout, packetProtocol int32, serverID string, publicKey []byte, verifyToken []byte, packetIDs *packetid.Table) error {
	id, ok := resolvePacketID(packetIDs, packetProtocol, packetid.StateLogin, packetid.ToClient, "encryption_begin")
	if !ok {
		return fmt.Errorf("missing encryption_begin packet id for protocol %d", packetProtocol)
	}
	var data bytes.Buffer
	if err := wire.WriteString(&data, serverID); err != nil {
		return err
	}
	if err := writeLoginByteArray(&data, publicKey); err != nil {
		return err
	}
	if err := writeLoginByteArray(&data, verifyToken); err != nil {
		return err
	}
	if layout.requestAuthenticate {
		if err := wire.WriteBool(&data, true); err != nil {
			return err
		}
	}
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
}

func resolvePacketID(packetIDs *packetid.Table, protocol int32, state packetid.State, direction packetid.Direction, name string) (int32, bool) {
	if packetIDs != nil {
		return packetIDs.ID(protocol, state, direction, name)
	}
	return packetid.ID(protocol, state, direction, name)
}

func readEncryptionResponse(layout onlineSessionAuthLayout, data []byte, privateKey *rsa.PrivateKey) ([]byte, []byte, error) {
	reader := bytes.NewReader(data)
	encryptedSecret, err := readLoginByteArray(reader)
	if err != nil {
		return nil, nil, err
	}
	var encryptedToken []byte
	if layout.responseVerifyFlag {
		hasVerifyToken, err := reader.ReadByte()
		if err != nil {
			return nil, nil, err
		}
		if hasVerifyToken == 0 {
			return nil, nil, fmt.Errorf("%w: signed encryption response is not supported", limbgo.ErrUnsupportedCapability)
		}
		encryptedToken, err = readLoginByteArray(reader)
		if err != nil {
			return nil, nil, err
		}
	} else {
		encryptedToken, err = readLoginByteArray(reader)
		if err != nil {
			return nil, nil, err
		}
	}
	if reader.Len() != 0 {
		return nil, nil, fmt.Errorf("%w: encryption response has %d trailing bytes", limbgo.ErrInvalidLogin, reader.Len())
	}
	secret, err := rsa.DecryptPKCS1v15(rand.Reader, privateKey, encryptedSecret)
	if err != nil {
		return nil, nil, err
	}
	token, err := rsa.DecryptPKCS1v15(rand.Reader, privateKey, encryptedToken)
	if err != nil {
		return nil, nil, err
	}
	return secret, token, nil
}

func writeLoginByteArray(data *bytes.Buffer, value []byte) error {
	if err := wire.WriteVarInt(data, int32(len(value))); err != nil {
		return err
	}
	_, err := data.Write(value)
	return err
}

func readLoginByteArray(reader *bytes.Reader) ([]byte, error) {
	length, err := wire.ReadVarInt(reader)
	if err != nil {
		return nil, err
	}
	if length < 0 || int(length) > reader.Len() {
		return nil, fmt.Errorf("minecraft byte array length %d outside remaining %d", length, reader.Len())
	}
	value := make([]byte, int(length))
	_, err = reader.Read(value)
	return value, err
}

func minecraftSessionServerID(serverID string, sharedSecret []byte, publicKey []byte) string {
	hash := sha1.New()
	_, _ = hash.Write([]byte(serverID))
	_, _ = hash.Write(sharedSecret)
	_, _ = hash.Write(publicKey)
	sum := hash.Sum(nil)
	value := new(big.Int).SetBytes(sum)
	if sum[0]&0x80 != 0 {
		value.Sub(value, new(big.Int).Lsh(big.NewInt(1), uint(len(sum)*8)))
	}
	return value.Text(16)
}

func remoteIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

type encryptedConn struct {
	net.Conn
	reader cipher.Stream
	writer cipher.Stream
}

func newEncryptedConn(conn net.Conn, sharedSecret []byte) (net.Conn, error) {
	block, err := aes.NewCipher(sharedSecret)
	if err != nil {
		return nil, err
	}
	return &encryptedConn{
		Conn:   conn,
		reader: newCFB8(block, sharedSecret, false),
		writer: newCFB8(block, sharedSecret, true),
	}, nil
}

func (c *encryptedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.reader.XORKeyStream(p[:n], p[:n])
	}
	return n, err
}

func (c *encryptedConn) Write(p []byte) (int, error) {
	out := make([]byte, len(p))
	c.writer.XORKeyStream(out, p)
	return c.Conn.Write(out)
}

type cfb8 struct {
	block   cipher.Block
	iv      []byte
	encrypt bool
	tmp     []byte
}

func newCFB8(block cipher.Block, iv []byte, encrypt bool) cipher.Stream {
	state := make([]byte, len(iv))
	copy(state, iv)
	return &cfb8{
		block:   block,
		iv:      state,
		encrypt: encrypt,
		tmp:     make([]byte, block.BlockSize()),
	}
}

func (s *cfb8) XORKeyStream(dst, src []byte) {
	for i, value := range src {
		s.block.Encrypt(s.tmp, s.iv)
		out := value ^ s.tmp[0]
		dst[i] = out
		copy(s.iv, s.iv[1:])
		if s.encrypt {
			s.iv[len(s.iv)-1] = out
		} else {
			s.iv[len(s.iv)-1] = value
		}
	}
}
