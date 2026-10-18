package identity

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/ShuilingXu/YggServer-Go/internal/model"
)

var (
	ErrMail        = errors.New("mail delivery unavailable")
	ErrCredentials = errors.New("invalid credentials")
	ErrConflict    = errors.New("account or player already exists")
	ErrInvalid     = errors.New("invalid input")
	ErrDenied      = errors.New("access denied")
	ErrUnavailable = errors.New("identity unavailable")
	ErrRateLimited = errors.New("too many credential attempts")
	playerName     = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)
)

type Account struct {
	ID           int64              `json:"-"`
	UUID         string             `json:"id"`
	Email        string             `json:"email"`
	PasswordHash []byte             `json:"-"`
	State        model.AccountState `json:"state"`
	Permission   model.Permission   `json:"permission"`
	Verified     int16              `json:"verified"`
	CreatedAt    time.Time          `json:"createdAt"`
}

type Profile = model.Profile

type Token struct {
	Account      Account
	Hash         string
	ClientHash   string
	ClientCipher []byte
	PlayerUUID   string
	Kind         model.TokenKind
	ExpiresAt    time.Time
	RefreshUntil time.Time
}

type LoginResponse struct {
	AccessToken       string    `json:"accessToken"`
	ClientToken       string    `json:"clientToken"`
	AvailableProfiles []Profile `json:"availableProfiles"`
	SelectedProfile   *Profile  `json:"selectedProfile,omitempty"`
	User              *struct {
		ID         string `json:"id"`
		Properties []any  `json:"properties"`
	} `json:"user,omitempty"`
}

func OfflineUUID(name string) string {
	b := md5.Sum([]byte("OfflinePlayer:" + name))
	b[6] = b[6]&0x0f | 0x30
	b[8] = b[8]&0x3f | 0x80
	return hex.EncodeToString(b[:])
}

func Hash(value string) string {
	b := sha256.Sum256([]byte(value))
	return hex.EncodeToString(b[:])
}

func Email(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	a, err := mail.ParseAddress(value)
	if err != nil || a.Address != value || len(value) > 255 {
		return "", ErrInvalid
	}
	return value, nil
}

func ValidPassword(value string) bool { return len(value) >= 8 && len(value) <= 72 }
func ValidPlayer(name string) bool    { return playerName.MatchString(name) }

func PlayerUUID(name string, strategy model.UUIDStrategy) string {
	if strategy == model.UUIDRandom {
		return model.UUID()
	}
	return OfflineUUID(name)
}
