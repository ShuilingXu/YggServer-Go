package identity

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
	"testing"
)

func TestImportedPasswordLogin(t *testing.T) {
	password := "old-password"
	salt := "site-pepper"
	sum := md5.Sum([]byte(password))
	salted := md5.Sum([]byte(fmt.Sprintf("%x", sum) + salt))
	sha := sha256.Sum256([]byte(password))
	saltedSHA := sha256.Sum256([]byte(fmt.Sprintf("%x", sha) + salt))
	wide := sha512.Sum512([]byte(password))
	saltedWide := sha512.Sum512([]byte(fmt.Sprintf("%x", wide) + salt))
	b, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	aSalt := []byte("12345678salt")
	a := argon2.IDKey([]byte(password), aSalt, 1, 8192, 1, 32)
	argon := fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(aSalt), base64.RawStdEncoding.EncodeToString(a))
	for _, tc := range []struct{ method, hash string }{{"MD5", fmt.Sprintf("%x", sum)}, {"SALTED2MD5", fmt.Sprintf("%x", salted)}, {"SHA256", fmt.Sprintf("%x", sha)}, {"SALTED2SHA256", fmt.Sprintf("%x", saltedSHA)}, {"SHA512", fmt.Sprintf("%x", wide)}, {"SALTED2SHA512", fmt.Sprintf("%x", saltedWide)}, {"BCRYPT", string(b)}, {"PHP_PASSWORD_HASH", argon}} {
		t.Run(tc.method, func(t *testing.T) {
			hash, err := ImportPassword(tc.hash, tc.method)
			if err != nil {
				t.Fatal(err)
			}
			if tc.method != "BCRYPT" && tc.method != "PHP_PASSWORD_HASH" {
				if bytes.Contains(hash, []byte(tc.hash)) {
					t.Fatal("fast legacy digest stored without bcrypt protection")
				}
			}
			if !verifyPassword(hash, password, salt) {
				t.Fatal("imported account cannot log in")
			}
			if verifyPassword(hash, "wrong", salt) {
				t.Fatal("wrong password accepted")
			}
		})
	}
	if verifyPassword([]byte("$bs$salted2md5$"+fmt.Sprintf("%x", salted)), password, "") {
		t.Fatal("missing pepper accepted")
	}
	if _, err := ImportPassword("not-a-hash", "BCRYPT"); err == nil {
		t.Fatal("invalid password hash imported")
	}
	if _, err := ImportPassword("$argon2id$v=19$m=999999999,t=1,p=1$MTIzNDU2Nzg$c29tZWhhc2g", "ARGON2I"); err == nil {
		t.Fatal("unbounded argon memory accepted")
	}
}
