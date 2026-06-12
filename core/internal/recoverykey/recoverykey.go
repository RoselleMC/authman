package recoverykey

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
)

const (
	AlgorithmRSAOAEP256 = "rsa-oaep-sha256"
	EnvelopePrefix      = AlgorithmRSAOAEP256 + ":"
)

type KeyPair struct {
	PublicPEM   string
	PrivatePEM  string
	Fingerprint string
	Algorithm   string
	SizeBits    int
}

func Generate() (KeyPair, error) {
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return KeyPair{}, err
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return KeyPair{}, err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return KeyPair{}, err
	}
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
	privatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}))
	fp := sha256.Sum256(publicDER)
	return KeyPair{
		PublicPEM:   publicPEM,
		PrivatePEM:  privatePEM,
		Fingerprint: hex.EncodeToString(fp[:]),
		Algorithm:   AlgorithmRSAOAEP256,
		SizeBits:    key.N.BitLen(),
	}, nil
}

func Encrypt(publicPEM string, plaintext []byte) (string, string, error) {
	block, _ := pem.Decode([]byte(publicPEM))
	if block == nil {
		return "", "", fmt.Errorf("public key PEM is invalid")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", "", err
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return "", "", fmt.Errorf("public key is not RSA")
	}
	fingerprint := FingerprintDER(block.Bytes)
	label := []byte("authman offline password recovery")
	ciphertext, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, plaintext, label)
	if err != nil {
		return "", "", err
	}
	return EnvelopePrefix + base64.StdEncoding.EncodeToString(ciphertext), fingerprint, nil
}

func FingerprintDER(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}
