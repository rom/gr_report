package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rom/gr_report/internal/i18n"
	"github.com/rom/gr_report/internal/sandbox"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gr_report.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const fullConfig = `
listen: "127.0.0.1:9443"
data_dir: /srv/gr
tls:
  cert: /etc/x/cert.pem
  key: /etc/x/key.pem
language: sv
chat:
  enabled: false
  oversight: true
sandbox: strict
development:
  insecure_http: false
  demo_accounts: false
`

func TestConfigFile(t *testing.T) {
	path := writeConfig(t, fullConfig)
	cfg, err := ParseConfig([]string{"-config", path}, env(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{File: path, Addr: "127.0.0.1:9443", DataDir: "/srv/gr", TLSCert: "/etc/x/cert.pem", TLSKey: "/etc/x/key.pem",
		Lang: i18n.SV, Chat: false, ChatOversight: true, Sandbox: sandbox.ModeStrict}
	if cfg != want {
		t.Errorf("config =\n%+v\nwant\n%+v", cfg, want)
	}
	// The file can also be named by the environment.
	if cfg2, err := ParseConfig(nil, env(map[string]string{"GR_REPORT_CONFIG": path}), io.Discard); err != nil || cfg2 != want {
		t.Errorf("GR_REPORT_CONFIG: %+v, %v", cfg2, err)
	}
}

func TestConfigPrecedence(t *testing.T) {
	path := writeConfig(t, fullConfig)
	// Environment beats the file ...
	e := env(map[string]string{"GR_REPORT_ADDR": ":7001", "GR_REPORT_LANG": "en", "GR_REPORT_CHAT": "on", "GR_REPORT_CHAT_OVERSIGHT": "off",
		"GR_REPORT_SANDBOX": "auto", "GR_REPORT_DATA": "/env/data"})
	cfg, err := ParseConfig([]string{"-config", path}, e, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":7001" || cfg.Lang != i18n.EN || !cfg.Chat || cfg.ChatOversight || cfg.Sandbox != sandbox.ModeAuto || cfg.DataDir != "/env/data" {
		t.Errorf("environment did not override the file: %+v", cfg)
	}
	if cfg.TLSCert != "/etc/x/cert.pem" {
		t.Errorf("settings not in the environment must come from the file: %+v", cfg)
	}
	// ... and flags beat the environment, also when set to a zero value.
	cfg, err = ParseConfig([]string{"-config", path, "-addr", ":7002", "-lang", "sv", "-chat=false", "-chat-oversight", "-sandbox", "off", "-data", "/flag/data"}, e, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":7002" || cfg.Lang != i18n.SV || cfg.Chat || !cfg.ChatOversight || cfg.Sandbox != sandbox.ModeOff || cfg.DataDir != "/flag/data" {
		t.Errorf("flags did not override the environment: %+v", cfg)
	}
	// -port replaces only the port, whatever the source of the address.
	if cfg, err = ParseConfig([]string{"-config", path, "-port", "8000"}, env(nil), io.Discard); err != nil || cfg.Addr != "127.0.0.1:8000" {
		t.Errorf("-port with file address: %+v, %v", cfg, err)
	}
	// A flag can switch off what the file switches on.
	on := writeConfig(t, "development:\n  insecure_http: true\n  demo_accounts: true\n")
	if cfg, err = ParseConfig([]string{"-config", on}, env(nil), io.Discard); err != nil || !cfg.InsecureHTTP || !cfg.DevSeed {
		t.Errorf("development settings from file: %+v, %v", cfg, err)
	}
	if cfg, err = ParseConfig([]string{"-config", on, "-dev-seed=false"}, env(nil), io.Discard); err != nil || cfg.DevSeed {
		t.Errorf("-dev-seed=false: %+v, %v", cfg, err)
	}
}

func TestConfigDefaultsAndRelativePaths(t *testing.T) {
	// An empty file, or one with only comments, means all defaults.
	for _, content := range []string{"", "# nothing here\n", "---\n", "tls:\n"} {
		path := writeConfig(t, content)
		cfg, err := ParseConfig([]string{"-config", path, "-insecure-http"}, env(nil), io.Discard)
		if err != nil || cfg.Addr != ":8443" || cfg.DataDir != "./data" || !cfg.Chat || cfg.ChatOversight || cfg.Lang != i18n.EN || cfg.Sandbox != sandbox.ModeAuto {
			t.Errorf("content %q: %+v, %v", content, cfg, err)
		}
	}
	// Relative paths in the file are relative to the file.
	path := writeConfig(t, "data_dir: data\ntls:\n  cert: tls/cert.pem\n  key: ../key.pem\n")
	dir := filepath.Dir(path)
	cfg, err := ParseConfig([]string{"-config", path}, env(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != filepath.Join(dir, "data") || cfg.TLSCert != filepath.Join(dir, "tls/cert.pem") || cfg.TLSKey != filepath.Join(filepath.Dir(dir), "key.pem") {
		t.Errorf("relative paths: %+v", cfg)
	}
	// Paths given by flag stay as typed.
	if cfg, _ = ParseConfig([]string{"-config", path, "-data", "elsewhere"}, env(nil), io.Discard); cfg.DataDir != "elsewhere" {
		t.Errorf("flag path rewritten: %q", cfg.DataDir)
	}
}

func TestConfigFileErrors(t *testing.T) {
	bad := map[string]string{
		"unknown key":       "listen: \":1\"\nlissten: \":2\"\n",
		"unknown nested":    "chat:\n  oversite: true\n",
		"wrong type":        "chat:\n  enabled: maybe\n",
		"list for string":   "listen: [a, b]\n",
		"not a mapping":     "- a\n- b\n",
		"two documents":     "listen: \":1\"\n---\nlisten: \":2\"\n",
		"syntax":            "listen: \"unterminated\n",
		"duplicate key":     "listen: \":1\"\nlisten: \":2\"\n",
		"bad language":      "language: de\ndevelopment:\n  insecure_http: true\n",
		"bad sandbox":       "sandbox: maybe\ndevelopment:\n  insecure_http: true\n",
		"bad address":       "listen: nonsense\ndevelopment:\n  insecure_http: true\n",
		"empty data dir":    "data_dir: \"\"\ndevelopment:\n  insecure_http: true\n",
		"tls missing":       "listen: \":1\"\n",
		"tls half":          "tls:\n  cert: c.pem\n",
		"tls and insecure":  "tls:\n  cert: c\n  key: k\ndevelopment:\n  insecure_http: true\n",
		"demo without http": "tls:\n  cert: c\n  key: k\ndevelopment:\n  demo_accounts: true\n",
	}
	for name, content := range bad {
		if _, err := ParseConfig([]string{"-config", writeConfig(t, content)}, env(nil), io.Discard); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	_, err := ParseConfig([]string{"-config", writeConfig(t, "lissten: x\n")}, env(nil), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "lissten") || !strings.Contains(err.Error(), "gr_report.yaml") {
		t.Errorf("error does not name the file and the key: %v", err)
	}
	if _, err := ParseConfig([]string{"-config", "/nonexistent/gr_report.yaml"}, env(nil), io.Discard); err == nil {
		t.Error("missing file accepted")
	}
	if _, err := ParseConfig([]string{"-config", t.TempDir()}, env(nil), io.Discard); err == nil {
		t.Error("directory accepted as file")
	}
	if _, err := ParseFileConfig(bytes.Repeat([]byte("# pad\n"), maxConfigBytes/6+1)); err == nil {
		t.Error("oversized file accepted")
	}
	if _, err := ParseConfig([]string{"-insecure-http"}, env(map[string]string{"GR_REPORT_CHAT": "perhaps"}), io.Discard); err == nil || !strings.Contains(err.Error(), "GR_REPORT_CHAT") {
		t.Errorf("bad boolean in environment: %v", err)
	}
	for v, want := range map[string]bool{"1": true, "TRUE": true, "on": true, "yes": true, "0": false, "false": false, "Off": false, "no": false} {
		cfg, err := ParseConfig([]string{"-insecure-http"}, env(map[string]string{"GR_REPORT_CHAT": v}), io.Discard)
		if err != nil || cfg.Chat != want {
			t.Errorf("GR_REPORT_CHAT=%s: %v, %v", v, cfg.Chat, err)
		}
	}
}

// The rendered configuration is a valid configuration file that yields the
// same configuration again.
func TestConfigYAMLRoundTrip(t *testing.T) {
	path := writeConfig(t, fullConfig)
	cfg, err := ParseConfig([]string{"-config", path, "-print-config"}, env(nil), io.Discard)
	if err != nil || !cfg.PrintConfig {
		t.Fatal(cfg, err)
	}
	out := cfg.YAML()
	for _, want := range []string{`listen: 127.0.0.1:9443`, "data_dir: /srv/gr", "language: sv", "sandbox: strict", "oversight: true", "enabled: false", "insecure_http: false", "cert: /etc/x/cert.pem"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered configuration lacks %q:\n%s", want, out)
		}
	}
	again, err := ParseConfig([]string{"-config", writeConfig(t, out)}, env(nil), io.Discard)
	if err != nil {
		t.Fatalf("rendered configuration does not parse: %v\n%s", err, out)
	}
	again.File, cfg.File, cfg.PrintConfig = "", "", false
	if again != cfg {
		t.Errorf("round trip changed the configuration:\n%+v\n%+v", cfg, again)
	}
}

// The file shipped for installation must be valid, complete and say what
// the documentation claims.
func TestShippedConfigFile(t *testing.T) {
	path := filepath.Join("..", "..", "deploy", "gr_report.yaml")
	cfg, err := ParseConfig([]string{"-config", path}, env(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8443" || cfg.DataDir != "/var/lib/gr_report" || cfg.TLSCert != "/etc/gr_report/tls/cert.pem" || cfg.TLSKey != "/etc/gr_report/tls/key.pem" ||
		cfg.Sandbox != sandbox.ModeStrict || cfg.Lang != i18n.EN || !cfg.Chat || cfg.ChatOversight || cfg.InsecureHTTP || cfg.DevSeed {
		t.Errorf("shipped configuration: %+v", cfg)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fc, err := ParseFileConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	// Every setting is present in the shipped file.
	if fc.Listen == nil || fc.DataDir == nil || fc.Language == nil || fc.Sandbox == nil || fc.TLS.Cert == nil || fc.TLS.Key == nil ||
		fc.Chat.Enabled == nil || fc.Chat.Oversight == nil || fc.Development.InsecureHTTP == nil || fc.Development.DemoAccounts == nil {
		t.Errorf("a setting is missing from the shipped file: %+v", fc)
	}
}

// FuzzConfigFile feeds arbitrary bytes to the configuration parser. It must
// not panic, and whatever it accepts must render and parse to the same.
func FuzzConfigFile(f *testing.F) {
	f.Add([]byte(fullConfig))
	f.Add([]byte("listen: \":1\"\nlissten: x\n"))
	f.Add([]byte("a: &a [*a, *a]\n"))
	f.Add([]byte("chat: {enabled: yes, oversight: !!bool \"no\"}\n"))
	f.Add([]byte("\x00\xff---\n...\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		fc, err := ParseFileConfig(data)
		if err != nil {
			return
		}
		cfg := Config{Sandbox: sandbox.ModeAuto, Lang: i18n.EN}
		if fc.Listen != nil {
			cfg.Addr = *fc.Listen
		}
		if fc.DataDir != nil {
			cfg.DataDir = *fc.DataDir
		}
		if fc.Chat.Enabled != nil {
			cfg.Chat = *fc.Chat.Enabled
		}
		fc2, err := ParseFileConfig([]byte(cfg.YAML()))
		if err != nil {
			t.Fatalf("rendered configuration does not parse: %v\n%s", err, cfg.YAML())
		}
		if *fc2.Listen != cfg.Addr || *fc2.DataDir != cfg.DataDir || *fc2.Chat.Enabled != cfg.Chat {
			t.Fatalf("round trip changed values: %q %q", *fc2.Listen, cfg.Addr)
		}
	})
}
