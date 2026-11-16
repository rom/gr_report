package auth

import (
	"strings"
	"testing"
	"time"
)

var fast = Hasher{Iterations: minIterations}

func TestHashVerify(t *testing.T) {
	h, err := fast.Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "pbkdf2-sha256$1000$") {
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
	for _, bad := range []string{"", "x", "pbkdf2-sha256$1000$abc", "md5$1000$AAAAAAAAAAAAAAAAAAAAAA$AAAA",
		"pbkdf2-sha256$5$AAAAAAAAAAAAAAAAAAAAAA$" + strings.Repeat("A", 43),
		"pbkdf2-sha256$99999999999$AAAAAAAAAAAAAAAAAAAAAA$" + strings.Repeat("A", 43),
		"pbkdf2-sha256$1000$!!$" + strings.Repeat("A", 43),
		"pbkdf2-sha256$1000$AAAA$" + strings.Repeat("A", 43),
		"pbkdf2-sha256$1000$AAAAAAAAAAAAAAAAAAAAAA$AAAA"} {
		if verify(bad, "pw") || wellFormed(bad) {
			t.Errorf("malformed hash %q accepted", bad)
		}
	}
	if Verify("", "gr_report-dummy-password") {
		t.Error("the dummy hash must never authenticate anyone")
	}
	if _, err := (Hasher{Iterations: 5}).Hash("x"); err == nil {
		t.Error("too few iterations accepted")
	}
	if !wellFormed(dummyHash) || !strings.Contains(dummyHash, "$600000$") {
		t.Errorf("default work factor: %q", dummyHash)
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

// FuzzVerify checks that malformed stored hashes never panic and never
// authenticate.
func FuzzVerify(f *testing.F) {
	good, _ := fast.Hash("password-1")
	f.Add(good, "password-1")
	f.Add("pbkdf2-sha256$1000$$", "")
	f.Add("pbkdf2-sha256$-1$AAAA$AAAA", "x")
	f.Fuzz(func(t *testing.T, encoded, pw string) {
		if strings.Count(encoded, "$") == 3 {
			// Keep the fuzzer fast: skip huge but valid work factors.
			if it, _, _, ok := parse(encoded); ok && it > 5000 {
				return
			}
		}
		ok := verify(encoded, pw)
		if ok && !wellFormed(encoded) {
			t.Errorf("malformed hash %q verified", encoded)
		}
		if ok && encoded != good {
			// Only reachable when the fuzzer derived a matching key.
			if _, _, _, wf := parse(encoded); !wf {
				t.Error("verified without a parsable hash")
			}
		}
	})
}
