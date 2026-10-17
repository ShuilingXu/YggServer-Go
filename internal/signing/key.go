package signing

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

func Generate(path string) error {
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return fmt.Errorf("generate signing key: %w", err)
	}
	data, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("encode signing key: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create signing key file")
	}
	_, writeErr := f.Write(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: data}))
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write signing key file")
	}
	if closeErr != nil {
		return fmt.Errorf("close signing key file")
	}
	return nil
}

func Load(path string) (*rsa.PrivateKey, error) {
	if path == "" {
		return nil, fmt.Errorf("YGG_SIGNING_KEY_FILE is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read signing key file")
	}
	block, rest := pem.Decode(data)
	if block == nil || len(rest) != 0 {
		return nil, fmt.Errorf("invalid signing key pem")
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		var parsed any
		parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		key, _ = parsed.(*rsa.PrivateKey)
	default:
		return nil, fmt.Errorf("unsupported signing key pem")
	}
	if err != nil || key == nil {
		return nil, fmt.Errorf("invalid rsa private key")
	}
	if key.N.BitLen() < 4096 {
		return nil, fmt.Errorf("rsa signing key must be at least 4096 bits")
	}
	if err := key.Validate(); err != nil {
		return nil, fmt.Errorf("invalid rsa private key")
	}
	return key, nil
}

func PublicPEM(key *rsa.PrivateKey) (string, error) {
	data, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", fmt.Errorf("encode signing public key: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: data})), nil
}
