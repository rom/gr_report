// Package auth implements password hashing, server-side sessions and login
// throttling.
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

	"golang.org/x/crypto/argon2"

	"github.com/rom/gr_report/internal/i18n"
)

// Password policy.
const (
	MinPasswordLen = 10
	MaxPasswordLen = 200
)

// Argon2id parameters for new hashes: the OWASP recommended configuration
// (19 MiB memory, 2 passes, 1 lane). See docs/adr/0015-argon2id.md for why
// this and not a larger setting.
const (
	DefaultMemoryKiB = 19 * 1024
	DefaultTime      = 2
	DefaultThreads   = 1
)

const (
	saltLen = 16
	keyLen  = 32

	// Bounds for parameters read back from a stored hash. They keep a
	// damaged or malicious database row from exhausting memory or CPU.
	minMemoryKiB = 8
	maxMemoryKiB = 256 * 1024
	maxTime      = 16
	maxThreads   = 16

	// Hashes created before Argon2id was introduced. They still verify
	// and are replaced at the next successful login.
	pbkdf2Scheme        = "pbkdf2-sha256"
	pbkdf2MinIterations = 1_000
	pbkdf2MaxIterations = 10_000_000
)

// MaxConcurrent bounds the number of simultaneous key derivations, and with
// it the memory a burst of logins can claim (MaxConcurrent × memory cost).
const MaxConcurrent = 4

var slots = make(chan struct{}, MaxConcurrent)

// Hasher hashes passwords with Argon2id. The zero value uses the defaults;
// tests lower the cost.
type Hasher struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
}

func (h Hasher) params() (m, t uint32, p uint8) {
	m, t, p = h.MemoryKiB, h.Time, h.Threads
	if m == 0 {
		m = DefaultMemoryKiB
	}
	if t == 0 {
		t = DefaultTime
	}
	if p == 0 {
		p = DefaultThreads
	}
	return m, t, p
}

func validParams(m, t uint32, p uint8) bool {
	return p >= 1 && p <= maxThreads && t >= 1 && t <= maxTime &&
		m >= minMemoryKiB*uint32(p) && m <= maxMemoryKiB
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

func derive(pw string, salt []byte, m, t uint32, p uint8) []byte {
	slots <- struct{}{}
	defer func() { <-slots }()
	return argon2.IDKey([]byte(pw), salt, t, m, p, keyLen)
}

// Hash returns the hash of pw in the PHC string format:
// $argon2id$v=19$m=<KiB>,t=<passes>,p=<lanes>$<salt b64>$<key b64>.
func (h Hasher) Hash(pw string) (string, error) {
	m, t, p := h.params()
	if !validParams(m, t, p) {
		return "", errors.New("auth: Argon2 parameters out of range")
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, m, t, p,
		enc.EncodeToString(salt), enc.EncodeToString(derive(pw, salt, m, t, p))), nil
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

// NeedsRehash reports whether a stored hash should be replaced after a
// successful login: it uses the old scheme or weaker parameters than h.
func (h Hasher) NeedsRehash(encoded string) bool {
	a, ok := parseArgon2(encoded)
	if !ok {
		return true
	}
	m, t, p := h.params()
	return a.m < m || a.t < t || a.p != p
}

type argon2Hash struct {
	m, t      uint32
	p         uint8
	salt, key []byte
}

func parseArgon2(encoded string) (argon2Hash, bool) {
	var a argon2Hash
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return a, false
	}
	for _, kv := range strings.Split(parts[3], ",") {
		k, v, _ := strings.Cut(kv, "=")
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return a, false
		}
		switch k {
		case "m":
			a.m = uint32(n)
		case "t":
			a.t = uint32(n)
		case "p":
			if n > maxThreads {
				return a, false
			}
			a.p = uint8(n)
		default:
			return a, false
		}
	}
	if !validParams(a.m, a.t, a.p) {
		return a, false
	}
	enc := base64.RawStdEncoding
	var err1, err2 error
	a.salt, err1 = enc.DecodeString(parts[4])
	a.key, err2 = enc.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(a.salt) < 8 || len(a.salt) > 64 || len(a.key) != keyLen {
		return a, false
	}
	return a, true
}

func parsePBKDF2(encoded string) (iter int, salt, key []byte, ok bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != pbkdf2Scheme {
		return 0, nil, nil, false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < pbkdf2MinIterations || iter > pbkdf2MaxIterations {
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
	if _, ok := parseArgon2(encoded); ok {
		return true
	}
	_, _, _, ok := parsePBKDF2(encoded)
	return ok
}

func verify(encoded, pw string) bool {
	if len(pw) > MaxPasswordLen {
		return false
	}
	if a, ok := parseArgon2(encoded); ok {
		return subtle.ConstantTimeCompare(derive(pw, a.salt, a.m, a.t, a.p), a.key) == 1
	}
	iter, salt, want, ok := parsePBKDF2(encoded)
	if !ok {
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
