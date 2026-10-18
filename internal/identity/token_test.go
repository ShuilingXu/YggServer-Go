package identity

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/ShuilingXu/YggServer-Go/internal/model"
)

func TestTokenAuthorization(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	client, wrong := "client", "wrong"
	for _, tc := range []struct {
		name              string
		state             model.AccountState
		stored, requested model.TokenKind
		expired, refresh  bool
		client            *string
		allowed           bool
	}{
		{"valid", 0, 1, 1, false, false, &client, true},
		{"optional client", 0, 1, 1, false, false, nil, true},
		{"wrong client", 0, 1, 1, false, false, &wrong, false},
		{"expired access", 0, 1, 1, true, false, &client, false},
		{"refresh window", 0, 1, 1, true, true, &client, true},
		{"banned", 1, 1, 1, false, false, nil, false},
		{"deleted", 2, 1, 1, false, true, nil, false},
		{"web token in game", 0, 0, 1, false, false, nil, false},
		{"game token in web", 0, 1, 0, false, false, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expires := now.Add(time.Hour)
			if tc.expired {
				expires = now
			}
			token := Token{Account: Account{State: tc.state}, Kind: tc.stored, ClientHash: Hash(client), ExpiresAt: expires, RefreshUntil: now.Add(24 * time.Hour)}
			if err := token.Check(now, tc.requested, tc.client, tc.refresh); (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v, want %v", err == nil, tc.allowed)
			}
			if err := token.Check(token.RefreshUntil, tc.requested, tc.client, true); !errors.Is(err, ErrCredentials) {
				t.Fatal("refresh must fail at its expiry boundary")
			}
		})
	}
}

func TestClientTokenEncryption(t *testing.T) {
	access, client := "random-access-token-for-test", "arbitrary client token"
	sealed, err := sealClient(access, client)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte(client)) {
		t.Fatal("client token stored in plaintext")
	}
	clear, err := openClient(access, sealed)
	if err != nil || clear != client {
		t.Fatal("refresh cannot recover original clientToken")
	}
	if _, err := openClient("wrong-access-token", sealed); !errors.Is(err, ErrCredentials) {
		t.Fatal("different accessToken can decrypt clientToken")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := openClient(access, sealed); !errors.Is(err, ErrCredentials) {
		t.Fatal("modified ciphertext accepted")
	}
}

func TestOfflinePlayerIdentity(t *testing.T) {
	if got := OfflineUUID("Notch"); got != "b50ad385829d3141a2167e7d7539ba7f" {
		t.Fatalf("offline UUID=%s", got)
	}
}
