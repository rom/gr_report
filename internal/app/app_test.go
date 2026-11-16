package app

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rom/gr_report/internal/auth"
	"github.com/rom/gr_report/internal/i18n"
	"github.com/rom/gr_report/internal/sandbox"
	"github.com/rom/gr_report/internal/store"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestParseConfig(t *testing.T) {
	tls := []string{"-tls-cert", "c.pem", "-tls-key", "k.pem"}
	cfg, err := ParseConfig(tls, env(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8443" || cfg.DataDir != "./data" || cfg.Sandbox != sandbox.ModeAuto || cfg.InsecureHTTP || cfg.DevSeed || cfg.Lang != i18n.EN || !cfg.Chat || cfg.ChatOversight {
		t.Errorf("defaults = %+v", cfg)
	}

	// Environment provides defaults, flags win.
	e := env(map[string]string{"GR_REPORT_ADDR": "127.0.0.1:9000", "GR_REPORT_DATA": "/var/lib/x", "GR_REPORT_TLS_CERT": "c", "GR_REPORT_TLS_KEY": "k", "GR_REPORT_SANDBOX": "strict"})
	cfg, err = ParseConfig(nil, e, io.Discard)
	if err != nil || cfg.Addr != "127.0.0.1:9000" || cfg.DataDir != "/var/lib/x" || cfg.TLSCert != "c" || cfg.Sandbox != sandbox.ModeStrict {
		t.Errorf("env config = %+v, %v", cfg, err)
	}
	cfg, err = ParseConfig([]string{"-addr", "[::1]:7000", "-port", "7443", "-sandbox", "off"}, e, io.Discard)
	if err != nil || cfg.Addr != "[::1]:7443" || cfg.Sandbox != sandbox.ModeOff {
		t.Errorf("flag override = %+v, %v", cfg, err)
	}
	cfg, err = ParseConfig(tls, env(map[string]string{"GR_REPORT_PORT": "10443"}), io.Discard)
	if err != nil || cfg.Addr != ":10443" {
		t.Errorf("GR_REPORT_PORT = %+v, %v", cfg, err)
	}
	cfg, err = ParseConfig([]string{"-insecure-http", "-dev-seed", "-port", "8080"}, env(nil), io.Discard)
	if err != nil || !cfg.InsecureHTTP || !cfg.DevSeed || cfg.Addr != ":8080" {
		t.Errorf("dev config = %+v, %v", cfg, err)
	}
	if cfg, err = ParseConfig(nil, env(map[string]string{"GR_REPORT_INSECURE_HTTP": "1"}), io.Discard); err != nil || !cfg.InsecureHTTP {
		t.Errorf("GR_REPORT_INSECURE_HTTP = %+v, %v", cfg, err)
	}
	if cfg, err = ParseConfig(append([]string{"-lang", "sv"}, tls...), env(nil), io.Discard); err != nil || cfg.Lang != i18n.SV {
		t.Errorf("-lang sv = %+v, %v", cfg, err)
	}
	if cfg, err = ParseConfig(tls, env(map[string]string{"GR_REPORT_LANG": "sv"}), io.Discard); err != nil || cfg.Lang != i18n.SV {
		t.Errorf("GR_REPORT_LANG = %+v, %v", cfg, err)
	}
	if cfg, err = ParseConfig(append([]string{"-chat=false"}, tls...), env(nil), io.Discard); err != nil || cfg.Chat {
		t.Errorf("-chat=false = %+v, %v", cfg, err)
	}
	if cfg, err = ParseConfig(tls, env(map[string]string{"GR_REPORT_CHAT": "off"}), io.Discard); err != nil || cfg.Chat {
		t.Errorf("GR_REPORT_CHAT=off = %+v, %v", cfg, err)
	}
	if cfg, err = ParseConfig(append([]string{"-chat-oversight"}, tls...), env(nil), io.Discard); err != nil || !cfg.ChatOversight {
		t.Errorf("-chat-oversight = %+v, %v", cfg, err)
	}
	if cfg, err = ParseConfig(tls, env(map[string]string{"GR_REPORT_CHAT_OVERSIGHT": "on"}), io.Discard); err != nil || !cfg.ChatOversight {
		t.Errorf("GR_REPORT_CHAT_OVERSIGHT=on = %+v, %v", cfg, err)
	}
	if cfg, err = ParseConfig([]string{"-version"}, env(nil), io.Discard); err != nil || !cfg.ShowVersion {
		t.Errorf("-version = %+v, %v", cfg, err)
	}

	bad := [][]string{
		{},                     // TLS is mandatory by default
		{"-tls-cert", "c.pem"}, // key missing
		{"-insecure-http", "-tls-cert", "c", "-tls-key", "k"},
		append([]string{"-dev-seed"}, tls...), // demo accounts never with TLS/production
		append([]string{"-addr", "nonsense"}, tls...),
		append([]string{"-port", "http"}, tls...),
		append([]string{"-port", "70000"}, tls...),
		append([]string{"-sandbox", "maybe"}, tls...),
		append([]string{"-lang", "de"}, tls...),
		append([]string{"-data", ""}, tls...),
		append([]string{"extra"}, tls...),
		{"-no-such-flag"},
	}
	for _, args := range bad {
		if _, err := ParseConfig(args, env(nil), io.Discard); err == nil {
			t.Errorf("ParseConfig(%q) = nil", args)
		}
	}
	var usage bytes.Buffer
	if _, err := ParseConfig([]string{"-h"}, env(nil), &usage); !errors.Is(err, ErrHelp) || !strings.Contains(usage.String(), "-tls-cert") {
		t.Errorf("-h: %v, %q", err, usage.String())
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func writeCert(t *testing.T, dir, cn string) (certFile, keyFile string, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, DNSNames: []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool = x509.NewCertPool()
	pool.AddCert(cert)
	return certFile, keyFile, pool
}

// start runs the app in the background and returns its base URL and a
// function that stops it and returns Run's error.
func start(t *testing.T, a *App, scheme string) (string, func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan net.Addr, 1)
	a.Ready = func(addr net.Addr) { ready <- addr }
	a.Hasher = auth.Hasher{MemoryKiB: 64, Time: 1}
	errc := make(chan error, 1)
	go func() { errc <- a.Run(ctx) }()
	stopped := false
	stop := func() error {
		if stopped {
			return nil
		}
		stopped = true
		cancel()
		select {
		case err := <-errc:
			return err
		case <-time.After(15 * time.Second):
			t.Fatal("Run did not return")
			return nil
		}
	}
	t.Cleanup(func() { _ = stop() })
	select {
	case addr := <-ready:
		return scheme + "://" + addr.String(), stop
	case err := <-errc:
		t.Fatalf("Run: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("server did not start")
	}
	return "", nil
}

func TestRunHTTPDevSeed(t *testing.T) {
	dir := t.TempDir()
	var logs syncBuffer
	a := &App{Cfg: Config{Addr: "127.0.0.1:0", DataDir: dir, InsecureHTTP: true, DevSeed: true, Sandbox: sandbox.ModeOff, Lang: i18n.SV, Chat: true, ChatOversight: true},
		Log: slog.New(slog.NewTextHandler(&logs, nil))}
	base, stop := start(t, a, "http")

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	resp, err := c.PostForm(base+"/login", url.Values{"username": {"leader"}, "password": {DevPassword}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	// The configured default language applies when the browser asks for none.
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Rapportkö") || !strings.Contains(string(body), `<html lang="sv"`) || !strings.Contains(string(body), `href="/chat"`) {
		t.Errorf("dev login: %d\n%s", resp.StatusCode, body)
	}
	if err := stop(); err != nil {
		t.Errorf("Run returned %v", err)
	}
	for _, want := range []string{"plain HTTP", "publicly known password", "listening", "shutting down", "sandbox disabled", "chat oversight is on"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log lacks %q", want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, InitialPasswordFile)); err == nil {
		t.Error("initial password file written in dev-seed mode")
	}

	// A second start must not create accounts or templates again.
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	users, templates := len(st.Users()), len(st.Templates(true))
	if users != 4 || templates != 2 {
		t.Errorf("seeded %d users, %d templates", users, templates)
	}
	if tps := st.Templates(true); tps[0].Name != "Lägesrapport" {
		t.Errorf("starter templates not in the configured language: %q", tps[0].Name)
	}
	if _, err := os.Stat(filepath.Join(dir, store.DBName)); err != nil {
		t.Errorf("database file: %v", err)
	}
	if err := a.Bootstrap(st); err != nil {
		t.Fatal(err)
	}
	if len(st.Users()) != users || len(st.Templates(true)) != templates {
		t.Error("Bootstrap is not idempotent")
	}
}

func TestRunTLSBootstrapAndReload(t *testing.T) {
	dir, tlsDir := t.TempDir(), t.TempDir()
	certFile, keyFile, pool := writeCert(t, tlsDir, "first")
	reload := make(chan struct{})
	var logs syncBuffer
	a := &App{Cfg: Config{Addr: "127.0.0.1:0", DataDir: dir, TLSCert: certFile, TLSKey: keyFile, Sandbox: sandbox.ModeOff},
		Log: slog.New(slog.NewTextHandler(&logs, nil)), Reload: reload}
	base, stop := start(t, a, "https")

	// First start: a random admin password in a private file.
	raw, err := os.ReadFile(filepath.Join(dir, InitialPasswordFile))
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, InitialPasswordFile)); fi.Mode().Perm() != 0o600 {
		t.Errorf("password file mode %v", fi.Mode().Perm())
	}
	m := regexp.MustCompile(`password: (\S{16})\n`).FindSubmatch(raw)
	if m == nil {
		t.Fatalf("password file: %q", raw)
	}
	if strings.Contains(logs.String(), string(m[1])) {
		t.Error("initial password written to the log")
	}

	var peerCN string
	newClient := func(pool *x509.CertPool) *http.Client {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Jar: jar, Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs: pool, MinVersion: tls.VersionTLS12,
			VerifyConnection: func(cs tls.ConnectionState) error { peerCN = cs.PeerCertificates[0].Subject.CommonName; return nil },
		}}}
	}
	c := newClient(pool)
	resp, err := c.PostForm(base+"/login", url.Values{"username": {"admin"}, "password": {string(m[1])}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.Request.URL.Path != "/password" || !strings.Contains(string(body), "one-time password") {
		t.Errorf("admin first login at %s", resp.Request.URL.Path)
	}
	if resp.Header.Get("Strict-Transport-Security") == "" {
		t.Error("no HSTS over TLS")
	}
	if u, _ := url.Parse(base); len(c.Jar.Cookies(u)) != 1 || !strings.HasPrefix(c.Jar.Cookies(u)[0].Name, "__Host-") {
		t.Errorf("cookies: %+v", c.Jar.Cookies(u))
	}
	if peerCN != "first" {
		t.Errorf("served certificate %q", peerCN)
	}

	// Old protocol versions are refused.
	old := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS11}}}
	if _, err := old.Get(base + "/healthz"); err == nil {
		t.Error("TLS 1.1 accepted")
	}
	if _, err := http.Get("http://" + strings.TrimPrefix(base, "https://") + "/healthz"); err == nil {
		// Go answers plain HTTP on a TLS port with a 400.
		t.Log("plain HTTP request on TLS port answered")
	}

	// A broken renewal keeps the old certificate ...
	if err := os.WriteFile(certFile, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	reload <- struct{}{}
	waitLog(t, &logs, "reload failed")
	if _, err := newClient(pool).Get(base + "/healthz"); err != nil || peerCN != "first" {
		t.Errorf("old certificate not kept: %v, %q", err, peerCN)
	}
	// ... and a good one is picked up without restart.
	_, _, pool2 := writeCert(t, tlsDir, "second")
	reload <- struct{}{}
	waitLog(t, &logs, "certificate reloaded")
	if _, err := newClient(pool2).Get(base + "/healthz"); err != nil || peerCN != "second" {
		t.Errorf("new certificate not served: %v, %q", err, peerCN)
	}

	if err := stop(); err != nil {
		t.Errorf("Run returned %v", err)
	}
}

func waitLog(t *testing.T, logs *syncBuffer, want string) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if strings.Contains(logs.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("log never contained %q:\n%s", want, logs.String())
}

func TestRunErrors(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	run := func(cfg Config) error {
		cfg.Sandbox = sandbox.ModeOff
		a := &App{Cfg: cfg, Log: log, Hasher: auth.Hasher{MemoryKiB: 64, Time: 1}}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return a.Run(ctx)
	}
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, nil, 0o600)
	if err := run(Config{Addr: "127.0.0.1:0", DataDir: file, InsecureHTTP: true}); err == nil {
		t.Error("data directory that is a file accepted")
	}
	if err := run(Config{Addr: "127.0.0.1:0", DataDir: t.TempDir(), TLSCert: "/nonexistent/c.pem", TLSKey: "/nonexistent/k.pem"}); err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Errorf("missing certificate: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := run(Config{Addr: ln.Addr().String(), DataDir: t.TempDir(), InsecureHTTP: true}); err == nil || !strings.Contains(err.Error(), "listen") {
		t.Errorf("busy port: %v", err)
	}
	// The data directory is locked while in use.
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := run(Config{Addr: "127.0.0.1:0", DataDir: dir, InsecureHTTP: true}); !errors.Is(err, store.ErrLocked) {
		t.Errorf("locked data directory: %v", err)
	}
}

func TestBootstrapErrors(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{Cfg: Config{DataDir: "/nonexistent-dir-for-test"}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Hasher: auth.Hasher{MemoryKiB: 64, Time: 1}}
	if err := a.Bootstrap(st); err == nil {
		t.Error("unwritable password file ignored")
	}
	if len(st.Users()) != 0 {
		t.Error("admin created although the password could not be handed over")
	}
	st.Close()
	bad := &App{Cfg: Config{DataDir: t.TempDir()}, Log: a.Log, Hasher: auth.Hasher{Time: 999}}
	st2, _ := store.Open(t.TempDir())
	defer st2.Close()
	if err := bad.Bootstrap(st2); err == nil {
		t.Error("invalid hasher accepted")
	}
	bad.Cfg.DevSeed = true
	if err := bad.Bootstrap(st2); err == nil {
		t.Error("invalid hasher accepted in dev-seed mode")
	}
}

func TestStarterTemplatesValid(t *testing.T) {
	for _, lang := range i18n.Languages {
		tps := starterTemplates(lang)
		if len(tps) != 2 || len(tps[0].Fields) != 0 || len(tps[1].Fields) != 6 {
			t.Errorf("%s: unexpected starter templates", lang)
		}
		for _, tp := range tps {
			if err := tp.Validate(); err != nil {
				t.Errorf("%s %s: %v", lang, tp.Name, err)
			}
		}
	}
	if starterTemplates(i18n.SV)[1].Name != "Lägesrapport" || starterTemplates(i18n.EN)[0].Name != "Minimal report" {
		t.Error("starter templates are not in the requested language")
	}
}
