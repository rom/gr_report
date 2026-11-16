package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"sync"
	"time"
)

// Session lifetime defaults.
const (
	DefaultIdleTimeout = 2 * time.Hour
	DefaultMaxLifetime = 12 * time.Hour
	DefaultMaxSessions = 10000
)

// Session is the server-side state of a logged-in browser.
type Session struct {
	UserID   string
	CSRF     string
	Flash    string
	Created  time.Time
	LastSeen time.Time
}

// Sessions is an in-memory session table keyed by the SHA-256 of the cookie
// token, so a memory disclosure does not directly reveal usable tokens.
type Sessions struct {
	Idle     time.Duration
	Lifetime time.Duration
	Max      int
	Now      func() time.Time

	mu sync.Mutex
	m  map[[32]byte]*Session
}

// NewSessions returns a session table with default limits.
func NewSessions() *Sessions {
	return &Sessions{
		Idle: DefaultIdleTimeout, Lifetime: DefaultMaxLifetime, Max: DefaultMaxSessions,
		Now: time.Now, m: map[[32]byte]*Session{},
	}
}

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("auth: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func (s *Sessions) expired(se *Session, now time.Time) bool {
	return now.Sub(se.LastSeen) > s.Idle || now.Sub(se.Created) > s.Lifetime
}

// Create starts a session for userID and returns the cookie token.
func (s *Sessions) Create(userID string) string {
	token := randomToken()
	now := s.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.m) >= s.Max {
		for k, se := range s.m {
			if s.expired(se, now) {
				delete(s.m, k)
			}
		}
	}
	for len(s.m) >= s.Max { // still full: evict the least recently used
		var oldest [32]byte
		var t time.Time
		first := true
		for k, se := range s.m {
			if first || se.LastSeen.Before(t) {
				oldest, t, first = k, se.LastSeen, false
			}
		}
		delete(s.m, oldest)
	}
	s.m[sha256.Sum256([]byte(token))] = &Session{UserID: userID, CSRF: randomToken(), Created: now, LastSeen: now}
	return token
}

// Get returns a snapshot of the session for token and refreshes its idle
// timer.
func (s *Sessions) Get(token string) (Session, bool) {
	if token == "" || len(token) > 128 {
		return Session{}, false
	}
	key := sha256.Sum256([]byte(token))
	now := s.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	se, ok := s.m[key]
	if !ok {
		return Session{}, false
	}
	if s.expired(se, now) {
		delete(s.m, key)
		return Session{}, false
	}
	se.LastSeen = now
	return *se, true
}

// SetFlash stores a one-shot message shown on the next page.
func (s *Sessions) SetFlash(token, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if se, ok := s.m[sha256.Sum256([]byte(token))]; ok {
		se.Flash = msg
	}
}

// TakeFlash returns and clears the flash message.
func (s *Sessions) TakeFlash(token string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	se, ok := s.m[sha256.Sum256([]byte(token))]
	if !ok {
		return ""
	}
	msg := se.Flash
	se.Flash = ""
	return msg
}

// Delete ends the session for token.
func (s *Sessions) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, sha256.Sum256([]byte(token)))
}

// DeleteUser ends all sessions of a user (password change, disable).
func (s *Sessions) DeleteUser(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, se := range s.m {
		if se.UserID == userID {
			delete(s.m, k)
		}
	}
}

// Len returns the number of stored sessions.
func (s *Sessions) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}

// EqualToken compares two tokens in constant time.
func EqualToken(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// Limiter throttles failed logins per key (username and client address).
// After Burst failures the key is locked, with the lock time doubling on
// every further failure up to MaxLock.
type Limiter struct {
	Burst    int
	BaseLock time.Duration
	MaxLock  time.Duration
	Forget   time.Duration
	MaxKeys  int
	Now      func() time.Time

	mu sync.Mutex
	m  map[string]*attempt
}

type attempt struct {
	failures    int
	last        time.Time
	lockedUntil time.Time
}

// NewLimiter returns a limiter with default settings.
func NewLimiter() *Limiter {
	return &Limiter{Burst: 5, BaseLock: 30 * time.Second, MaxLock: 15 * time.Minute,
		Forget: time.Hour, MaxKeys: 50000, Now: time.Now, m: map[string]*attempt{}}
}

// Allowed reports whether a login attempt for key may proceed and, if not,
// how long the caller has to wait.
func (l *Limiter) Allowed(key string) (bool, time.Duration) {
	now := l.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.m[key]
	if !ok {
		return true, 0
	}
	if now.Before(a.lockedUntil) {
		return false, a.lockedUntil.Sub(now)
	}
	return true, 0
}

// Fail records a failed attempt.
func (l *Limiter) Fail(key string) {
	now := l.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.m[key]
	if ok && now.Sub(a.last) > l.Forget {
		a.failures = 0
	}
	if !ok {
		if len(l.m) >= l.MaxKeys {
			for k, v := range l.m {
				if now.Sub(v.last) > l.Forget {
					delete(l.m, k)
				}
			}
		}
		if len(l.m) >= l.MaxKeys {
			// Under a flood of distinct keys, drop arbitrary entries
			// rather than grow without bound.
			for k := range l.m {
				delete(l.m, k)
				if len(l.m) < l.MaxKeys {
					break
				}
			}
		}
		a = &attempt{}
		l.m[key] = a
	}
	a.failures++
	a.last = now
	if a.failures >= l.Burst {
		lock := l.BaseLock
		for i := l.Burst; i < a.failures && lock < l.MaxLock; i++ {
			lock *= 2
		}
		if lock > l.MaxLock {
			lock = l.MaxLock
		}
		a.lockedUntil = now.Add(lock)
	}
}

// Reset clears the failure history after a successful login.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, key)
}
