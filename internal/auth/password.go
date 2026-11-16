// Package auth implements password hashing, server-side sessions and login
// throttling using only the standard library.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rom/gr_report/internal/i18n"
)

// Password policy.
const (
	MinPasswordLen = 10
	MaxPasswordLen = 200
)

const (
	hashScheme = "pbkdf2-sha256"
	saltLen    = 16
	keyLen     = 32
	// DefaultIterations follows the OWASP recommendation for
	// PBKDF2-HMAC-SHA256.
	DefaultIterations = 600_000
	minIterations     = 1_000
	maxIterations     = 10_000_000
)

// Hasher hashes passwords with PBKDF2-HMAC-SHA256.
type Hasher struct {
	// Iterations is the work factor for new hashes. Tests lower it.
	Iterations int
}

// CheckPolicy validates a new password against the password policy.
func CheckPolicy(pw string) error {
	if !utf8.ValidString(pw) {
		return i18n.Errorf("password must be valid text")
	}
	n := utf8.RuneCountInString(pw)
	if n < MinPasswordLen {
		return i18n.Errorf("password must be at least %d characters", MinPasswordLen)
	}
	if len(pw) > MaxPasswordLen {
		return i18n.Errorf("password must be at most %d bytes", MaxPasswordLen)
	}
	return nil
}

// Hash returns an encoded hash of pw:
// pbkdf2-sha256$<iterations>$<salt b64>$<key b64>.
func (h Hasher) Hash(pw string) (string, error) {
	iter := h.Iterations
	if iter == 0 {
		iter = DefaultIterations
	}
	if iter < minIterations || iter > maxIterations {
		return "", errors.New("auth: iterations out of range")
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, iter, keyLen)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("%s$%d$%s$%s", hashScheme, iter, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// dummyHash is verified against when the user does not exist so that the
// response time does not reveal whether a username is valid.
var dummyHash, _ = Hasher{}.Hash("gr_report-dummy-password")

// Verify reports whether pw matches the encoded hash. An empty or malformed
// hash never matches, but still costs a full key derivation.
func Verify(encoded, pw string) bool {
	ok := verify(encoded, pw)
	if !ok && !wellFormed(encoded) {
		verify(dummyHash, pw)
	}
	return ok
}

func parse(encoded string) (iter int, salt, key []byte, ok bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != hashScheme {
		return 0, nil, nil, false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < minIterations || iter > maxIterations {
		return 0, nil, nil, false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	key, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(salt) < 8 || len(salt) > 64 || len(key) != keyLen {
		return 0, nil, nil, false
	}
	return iter, salt, key, true
}

func wellFormed(encoded string) bool {
	_, _, _, ok := parse(encoded)
	return ok
}

func verify(encoded, pw string) bool {
	iter, salt, want, ok := parse(encoded)
	if !ok || len(pw) > MaxPasswordLen {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, keyLen)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

const pwAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// GeneratePassword returns a random 16 character password without
// look-alike characters (about 93 bits of entropy).
func GeneratePassword() string {
	const n = 16
	out := make([]byte, 0, n)
	buf := make([]byte, 1)
	limit := byte(256 - 256%len(pwAlphabet))
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			panic("auth: crypto/rand failed: " + err.Error())
		}
		if buf[0] >= limit {
			continue // avoid modulo bias
		}
		out = append(out, pwAlphabet[int(buf[0])%len(pwAlphabet)])
	}
	return string(out)
}
