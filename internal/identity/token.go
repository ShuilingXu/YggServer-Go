package identity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"time"

	"github.com/ShuilingXu/YggServer-Go/internal/model"
)

func (t Token) Check(now time.Time, kind model.TokenKind, client *string, refresh bool) error {
	deadline := t.ExpiresAt
	if refresh {
		deadline = t.RefreshUntil
	}
	if t.Account.State != model.AccountActive || t.Kind != kind || !now.Before(deadline) {
		return ErrCredentials
	}
	if client != nil && subtle.ConstantTimeCompare([]byte(t.ClientHash), []byte(Hash(*client))) != 1 {
		return ErrCredentials
	}
	return nil
}

func clientCipher(access string) (cipher.AEAD, error) {
	key := sha256.Sum256([]byte("YggServer-Go/clientToken/v1\x00" + access))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("initialize client token cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

func sealClient(access, client string) ([]byte, error) {
	aead, err := clientCipher(access)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate client token nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, []byte(client), nil), nil
}

func openClient(access string, data []byte) (string, error) {
	aead, err := clientCipher(access)
	if err != nil {
		return "", err
	}
	if len(data) < aead.NonceSize() {
		return "", ErrCredentials
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], nil)
	if err != nil {
		return "", ErrCredentials
	}
	return string(plain), nil
}
