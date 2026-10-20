package identity

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
	"strings"
)

func ImportPassword(hash, method string) ([]byte, error) {
	if strings.HasPrefix(hash, "$2") {
		if _, err := bcrypt.Cost([]byte(hash)); err == nil {
			return []byte(hash), nil
		}
	}
	if strings.HasPrefix(hash, "$argon2") {
		if _, _, _, _, _, ok := parseArgon(hash); ok {
			return []byte(hash), nil
		}
	}
	method = strings.ToLower(method)
	expected := 0
	switch strings.TrimPrefix(method, "salted2") {
	case "md5":
		expected = 32
	case "sha256":
		expected = 64
	case "sha512":
		expected = 128
	}
	if expected != 0 && len(hash) == expected {
		if _, err := hex.DecodeString(hash); err == nil {
			protected, err := bcrypt.GenerateFromPassword(legacyDigestInput(strings.ToLower(hash)), bcrypt.DefaultCost)
			if err != nil {
				return nil, err
			}
			return []byte("$bs$" + method + "$" + string(protected)), nil
		}
	}
	return nil, ErrInvalid
}

func parseArgon(hash string) (uint32, uint32, uint8, []byte, []byte, bool) {
	p := strings.Split(hash, "$")
	if len(p) != 6 || (p[1] != "argon2i" && p[1] != "argon2id") || p[2] != "v=19" {
		return 0, 0, 0, nil, nil, false
	}
	var memory, iterations uint32
	var parallel uint8
	n, err := fmt.Sscanf(p[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallel)
	salt, e1 := base64.RawStdEncoding.Strict().DecodeString(p[4])
	key, e2 := base64.RawStdEncoding.Strict().DecodeString(p[5])
	ok := n == 3 && err == nil && p[3] == fmt.Sprintf("m=%d,t=%d,p=%d", memory, iterations, parallel) && memory >= 8*uint32(parallel) && memory <= 262144 && iterations >= 1 && iterations <= 16 && parallel >= 1 && parallel <= 8 && e1 == nil && e2 == nil && len(salt) >= 8 && len(salt) <= 64 && len(key) >= 16 && len(key) <= 64
	return memory, iterations, parallel, salt, key, ok
}

func verifyPassword(hash []byte, password, salt string) bool {
	text := string(hash)
	if strings.HasPrefix(text, "$2") {
		return bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
	}
	if strings.HasPrefix(text, "$argon2") {
		m, t, p, s, k, ok := parseArgon(text)
		if !ok {
			return false
		}
		var actual []byte
		if strings.HasPrefix(text, "$argon2id$") {
			actual = argon2.IDKey([]byte(password), s, t, m, p, uint32(len(k)))
		} else {
			actual = argon2.Key([]byte(password), s, t, m, p, uint32(len(k)))
		}
		return subtle.ConstantTimeCompare(actual, k) == 1
	}
	parts := strings.SplitN(text, "$", 4)
	if len(parts) != 4 || parts[1] != "bs" {
		return false
	}
	algorithm := parts[2]
	salted := strings.HasPrefix(algorithm, "salted2")
	algorithm = strings.TrimPrefix(algorithm, "salted2")
	digest := func(value string) string {
		switch algorithm {
		case "md5":
			b := md5.Sum([]byte(value))
			return hex.EncodeToString(b[:])
		case "sha256":
			b := sha256.Sum256([]byte(value))
			return hex.EncodeToString(b[:])
		case "sha512":
			b := sha512.Sum512([]byte(value))
			return hex.EncodeToString(b[:])
		}
		return ""
	}
	actual := digest(password)
	if salted {
		if salt == "" {
			return false
		}
		actual = digest(actual + salt)
	}
	return actual != "" && bcrypt.CompareHashAndPassword([]byte(parts[3]), legacyDigestInput(actual)) == nil
}

// 快速旧摘要不能裸存；固定长度预哈希后用 bcrypt 保护，首次登录再改为直接 bcrypt 密码。
func legacyDigestInput(digest string) []byte { sum := sha256.Sum256([]byte(digest)); return sum[:] }
