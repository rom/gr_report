package auth

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

var fast = Hasher{MemoryKiB: 64, Time: 1}

// legacyHash builds a hash in the format used before Argon2id.
func legacyHash(t testing.TB, pw string, iter int) string {
	t.Helper()
	salt := []byte("0123456789abcdef")
	key, err := pbkdf2.Key(sha256.New, pw, salt, iter, keyLen)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iter, enc.EncodeToString(salt), enc.EncodeToString(key))
}

func TestHashVerify(t *testing.T) {
	h, err := fast.Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=64,t=1,p=1$") || strings.Count(h, "$") != 5 {
		t.Errorf("hash format: %q", h)
	}
	h2, _ := fast.Hash("correct horse battery")
	if h == h2 {
		t.Error("salt not random")
	}
	if !Verify(h, "correct horse battery") {
		t.Error("correct password rejected")
	}
	if Verify(h, "correct horse batterx") || Verify(h, "") {
		t.Error("wrong password accepted")
	}
	if Verify(h, strings.Repeat("x", MaxPasswordLen+1)) {
		t.Error("oversized password accepted")
	}
	salt, key := "MDEyMzQ1Njc4OWFiY2RlZg", strings.Repeat("A", 43)
	for _, bad := range []string{"", "x", "$argon2id$v=19$m=64,t=1,p=1$" + salt, "$argon2i$v=19$m=64,t=1,p=1$" + salt + "$" + key,
		"$argon2id$v=16$m=64,t=1,p=1$" + salt + "$" + key,
		"$argon2id$v=19$m=4,t=1,p=1$" + salt + "$" + key,       // too little memory
		"$argon2id$v=19$m=4194304,t=1,p=1$" + salt + "$" + key, // 4 GiB: refused, not allocated
		"$argon2id$v=19$m=64,t=0,p=1$" + salt + "$" + key,
		"$argon2id$v=19$m=64,t=1000,p=1$" + salt + "$" + key,
		"$argon2id$v=19$m=64,t=1,p=0$" + salt + "$" + key,
		"$argon2id$v=19$m=64,t=1,p=200$" + salt + "$" + key,
		"$argon2id$v=19$m=64,t=1,x=1$" + salt + "$" + key,
		"$argon2id$v=19$m=64,t=-1,p=1$" + salt + "$" + key,
		"$argon2id$v=19$m=64,t=1,p=1$!!$" + key,
		"$argon2id$v=19$m=64,t=1,p=1$AAAA$" + key,
		"$argon2id$v=19$m=64,t=1,p=1$" + salt + "$AAAA",
		"md5$1000$" + salt + "$AAAA", "pbkdf2-sha256$1000$abc",
		"pbkdf2-sha256$5$" + salt + "$" + key, "pbkdf2-sha256$99999999999$" + salt + "$" + key,
		"pbkdf2-sha256$1000$!!$" + key, "pbkdf2-sha256$1000$AAAA$" + key, "pbkdf2-sha256$1000$" + salt + "$AAAA"} {
		if verify(bad, "pw") || wellFormed(bad) {
			t.Errorf("malformed hash %q accepted", bad)
		}
	}
	if Verify("", "gr_report-dummy-password") {
		t.Error("the dummy hash must never authenticate anyone")
	}
	for _, h := range []Hasher{{Time: 99}, {Threads: 99}, {MemoryKiB: 4}, {MemoryKiB: 1 << 30}, {MemoryKiB: 16, Threads: 4}} {
		if _, err := h.Hash("x"); err == nil {
			t.Errorf("parameters %+v accepted", h)
		}
	}
	// The default is the OWASP recommended Argon2id configuration.
	if !strings.HasPrefix(dummyHash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("default parameters: %q", dummyHash)
	}
}

// Known-answer test from the Argon2 reference implementation
// (argon2id, password "password", salt "somesalt", t=2, m=64 MiB, p=1,
// 32-byte tag, digest 09316115d5cf24ed5a15a31a3ba326e5cf32edc24702987c
// 02b6566f61913cf7): the PHC string for it must verify here.
func TestArgon2ReferenceVector(t *testing.T) {
	const phc = "$argon2id$v=19$m=65536,t=2,p=1$c29tZXNhbHQ$CTFhFdXPJO1aFaMaO6Mm5c8y7cJHAph8ArZWb2GRPPc"
	if !verify(phc, "password") {
		t.Error("reference vector does not verify")
	}
	if verify(phc, "Password") {
		t.Error("reference vector verifies a wrong password")
	}
}

func TestLegacyHashesAndRehash(t *testing.T) {
	old := legacyHash(t, "password-from-before", 1000)
	if !Verify(old, "password-from-before") || Verify(old, "wrong") {
		t.Error("PBKDF2 hashes must keep verifying")
	}
	if !fast.NeedsRehash(old) || !fast.NeedsRehash("") {
		t.Error("legacy or empty hash not flagged for rehash")
	}
	cur, _ := fast.Hash("pw")
	if fast.NeedsRehash(cur) {
		t.Error("current hash flagged for rehash")
	}
	// Raising the cost flags hashes made with the lower cost, and a
	// stronger stored hash is left alone.
	stronger := Hasher{MemoryKiB: 128, Time: 2}
	if !stronger.NeedsRehash(cur) || !(Hasher{MemoryKiB: 64, Time: 2}).NeedsRehash(cur) {
		t.Error("weaker parameters not flagged")
	}
	strong, _ := stronger.Hash("pw")
	if fast.NeedsRehash(strong) {
		t.Error("stronger hash flagged")
	}
	if !(Hasher{MemoryKiB: 64, Time: 1, Threads: 2}).NeedsRehash(cur) {
		t.Error("different lane count not flagged")
	}
}

// Many simultaneous verifications must all complete: the limit on
// concurrent derivations queues, it does not reject.
func TestConcurrentVerify(t *testing.T) {
	h, _ := fast.Hash("pw-concurrent")
	var wg sync.WaitGroup
	fail := make(chan string, 4*MaxConcurrent)
	for i := 0; i < 4*MaxConcurrent; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if Verify(h, "pw-concurrent") != true || Verify(h, "nope") {
				fail <- fmt.Sprint("goroutine ", i)
			}
		}(i)
	}
	wg.Wait()
	close(fail)
	for f := range fail {
		t.Error("wrong result in", f)
	}
	if cap(slots) != MaxConcurrent || len(slots) != 0 {
		t.Errorf("slots: cap %d, in use %d", cap(slots), len(slots))
	}
}

func TestPolicy(t *testing.T) {
	if err := CheckPolicy("0123456789"); err != nil {
		t.Error(err)
	}
	if err := CheckPolicy("åäöåäöåäöå"); err != nil {
		t.Error("length must count characters:", err)
	}
	for _, bad := range []string{"short", "\xff\xfe0123456789", strings.Repeat("x", MaxPasswordLen+1)} {
		if CheckPolicy(bad) == nil {
			t.Errorf("CheckPolicy(%q) = nil", bad)
		}
	}
}

func TestGeneratePassword(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		p := GeneratePassword()
		if len(p) != 16 || seen[p] || strings.Trim(p, pwAlphabet) != "" || CheckPolicy(p) != nil {
			t.Fatalf("bad password %q", p)
		}
		seen[p] = true
	}
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestSessions(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	s := NewSessions()
	s.Now = c.now
	tok := s.Create("u1")
	tok2 := s.Create("u1")
	if tok == tok2 || len(tok) < 40 {
		t.Fatal("tokens not unique/long")
	}
	se, ok := s.Get(tok)
	if !ok || se.UserID != "u1" || len(se.CSRF) < 40 || se.CSRF == tok {
		t.Fatalf("Get = %+v, %v", se, ok)
	}
	for _, bad := range []string{"", "nope", strings.Repeat("x", 200)} {
		if _, ok := s.Get(bad); ok {
			t.Errorf("Get(%q) ok", bad)
		}
	}

	s.SetFlash(tok, "hello")
	s.SetFlash("unknown", "ignored")
	if s.TakeFlash(tok) != "hello" || s.TakeFlash(tok) != "" || s.TakeFlash("unknown") != "" {
		t.Error("flash is not one-shot")
	}

	// Activity extends the idle timer but not the absolute lifetime.
	for i := 0; i < 7; i++ {
		c.advance(s.Idle - time.Minute)
		if _, ok := s.Get(tok); !ok && c.t.Sub(se.Created) <= s.Lifetime {
			t.Fatalf("active session expired after %v", c.t.Sub(se.Created))
		}
	}
	if _, ok := s.Get(tok); ok {
		t.Error("session outlived its absolute lifetime")
	}
	if _, ok := s.Get(tok2); ok {
		t.Error("idle session not expired")
	}
	if s.Len() != 0 {
		t.Errorf("expired sessions kept: %d", s.Len())
	}

	a, b := s.Create("u1"), s.Create("u2")
	s.Delete(a)
	if _, ok := s.Get(a); ok {
		t.Error("deleted session alive")
	}
	s.Create("u2")
	s.DeleteUser("u2")
	if _, ok := s.Get(b); ok || s.Len() != 0 {
		t.Error("DeleteUser left sessions")
	}
}

func TestSessionCap(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	s := NewSessions()
	s.Now, s.Max = c.now, 3
	old := s.Create("u1")
	c.advance(s.Idle + time.Second)
	a := s.Create("a")
	c.advance(time.Second)
	b := s.Create("b")
	c.advance(time.Second)
	cc := s.Create("c") // table full: the expired one is purged
	if _, ok := s.Get(old); ok || s.Len() != 3 {
		t.Fatalf("expired session not purged, len=%d", s.Len())
	}
	c.advance(time.Second)
	s.Get(a) // a is now the most recently used
	d := s.Create("d")
	if _, ok := s.Get(b); ok {
		t.Error("least recently used session not evicted")
	}
	for _, tok := range []string{a, cc, d} {
		if _, ok := s.Get(tok); !ok {
			t.Error("wrong session evicted")
		}
	}
}

func TestEqualToken(t *testing.T) {
	if !EqualToken("abc", "abc") || EqualToken("abc", "abd") || EqualToken("", "") || EqualToken("a", "") {
		t.Error("EqualToken")
	}
}

func TestLimiter(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	l := NewLimiter()
	l.Now = c.now
	if ok, _ := l.Allowed("k"); !ok {
		t.Fatal("fresh key blocked")
	}
	for i := 0; i < l.Burst-1; i++ {
		l.Fail("k")
		if ok, _ := l.Allowed("k"); !ok {
			t.Fatalf("blocked after %d failures", i+1)
		}
	}
	l.Fail("k")
	ok, wait := l.Allowed("k")
	if ok || wait != l.BaseLock {
		t.Fatalf("after burst: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allowed("other"); !ok {
		t.Error("unrelated key blocked")
	}
	c.advance(l.BaseLock)
	if ok, _ := l.Allowed("k"); !ok {
		t.Error("lock did not expire")
	}
	l.Fail("k")
	if _, wait := l.Allowed("k"); wait != 2*l.BaseLock {
		t.Errorf("lock did not double: %v", wait)
	}
	for i := 0; i < 20; i++ {
		l.Fail("k")
	}
	if _, wait := l.Allowed("k"); wait != l.MaxLock {
		t.Errorf("lock not capped: %v", wait)
	}
	l.Reset("k")
	if ok, _ := l.Allowed("k"); !ok {
		t.Error("Reset did not clear the lock")
	}

	// Old failures are forgotten.
	for i := 0; i < l.Burst-1; i++ {
		l.Fail("slow")
	}
	c.advance(l.Forget + time.Second)
	l.Fail("slow")
	if ok, _ := l.Allowed("slow"); !ok {
		t.Error("stale failures counted")
	}
}

func TestLimiterBounded(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	l := NewLimiter()
	l.Now, l.MaxKeys = c.now, 4
	for _, k := range []string{"a", "b", "c", "d"} {
		l.Fail(k)
	}
	c.advance(l.Forget + time.Second)
	l.Fail("e") // stale entries are purged
	if len(l.m) != 1 {
		t.Errorf("stale keys kept: %d", len(l.m))
	}
	for _, k := range []string{"f", "g", "h", "i", "j", "k"} {
		l.Fail(k)
	}
	if len(l.m) > l.MaxKeys {
		t.Errorf("limiter grew past its bound: %d", len(l.m))
	}
}

// FuzzVerify checks that malformed stored hashes never panic, never
// authenticate and never make the server allocate without bound.
func FuzzVerify(f *testing.F) {
	good, _ := fast.Hash("password-1")
	f.Add(good, "password-1")
	f.Add(legacyHash(f, "password-1", 1000), "password-1")
	f.Add("$argon2id$v=19$m=4294967295,t=4294967295,p=255$AAAAAAAAAAA$AAAA", "x")
	f.Add("pbkdf2-sha256$-1$AAAA$AAAA", "x")
	f.Add("$argon2id$v=19$m=,t=,p=$$", "")
	f.Fuzz(func(t *testing.T, encoded, pw string) {
		// Keep the fuzzer fast: skip well-formed hashes with a real cost.
		if a, ok := parseArgon2(encoded); ok {
			if a.m > maxMemoryKiB || a.t > maxTime || a.p > maxThreads {
				t.Fatalf("parameters beyond the bounds accepted: %+v", a)
			}
			if a.m > 256 || a.t > 2 {
				return
			}
		}
		if it, _, _, ok := parsePBKDF2(encoded); ok && it > 5000 {
			return
		}
		if verify(encoded, pw) && !wellFormed(encoded) {
			t.Errorf("malformed hash %q verified", encoded)
		}
		_ = fast.NeedsRehash(encoded)
	})
}
