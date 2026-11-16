// Package app wires the parts of gr_report together: configuration,
// storage, first-run bootstrap, TLS, the sandbox and the HTTP server.
package app

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/rom/gr_report/internal/i18n"
	"github.com/rom/gr_report/internal/sandbox"
)

// Version is set at build time with -ldflags "-X ...app.Version=...".
var Version = "dev"

// Config is the effective runtime configuration.
//
// Every setting lives in the YAML configuration file (see FileConfig). A
// setting can be overridden by an environment variable and, above that, by
// a command line flag:
//
//	built-in default < configuration file < environment < flag
type Config struct {
	// File is the configuration file that was read ("" for none).
	File         string
	Addr         string
	DataDir      string
	TLSCert      string
	TLSKey       string
	InsecureHTTP bool
	DevSeed      bool
	Sandbox      sandbox.Mode
	// Lang is the default user interface language, used when a browser
	// states no preference, and the language of the starter templates.
	Lang i18n.Lang
	// Chat enables the chat between the participants.
	Chat bool
	// ChatOversight lets the exercise leaders read chat between groups.
	ChatOversight bool
	// ChatOversightToggle lets the exercise leaders switch oversight on and
	// off in the user interface; ChatOversight is then the initial state.
	ChatOversightToggle bool

	ShowVersion bool
	PrintConfig bool
}

// FileConfig is the schema of the YAML configuration file. Pointers tell a
// setting that is absent from one that is set to its zero value.
type FileConfig struct {
	Listen   *string `yaml:"listen,omitempty"`
	DataDir  *string `yaml:"data_dir,omitempty"`
	Language *string `yaml:"language,omitempty"`
	Sandbox  *string `yaml:"sandbox,omitempty"`
	TLS      struct {
		Cert *string `yaml:"cert,omitempty"`
		Key  *string `yaml:"key,omitempty"`
	} `yaml:"tls,omitempty"`
	Chat struct {
		Enabled         *bool `yaml:"enabled,omitempty"`
		Oversight       *bool `yaml:"oversight,omitempty"`
		OversightToggle *bool `yaml:"oversight_toggle,omitempty"`
	} `yaml:"chat,omitempty"`
	Development struct {
		InsecureHTTP *bool `yaml:"insecure_http,omitempty"`
		DemoAccounts *bool `yaml:"demo_accounts,omitempty"`
	} `yaml:"development,omitempty"`
}

// maxConfigBytes bounds the size of a configuration file.
const maxConfigBytes = 1 << 20

// ParseFileConfig parses the content of a configuration file. Unknown
// keys are errors, so a misspelt setting is noticed instead of ignored.
func ParseFileConfig(data []byte) (FileConfig, error) {
	var fc FileConfig
	if len(data) > maxConfigBytes {
		return fc, errors.New("file too large")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&fc); err != nil {
		if errors.Is(err, io.EOF) {
			return fc, nil // empty file: all defaults
		}
		return fc, err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return fc, errors.New("more than one YAML document")
	}
	return fc, nil
}

func loadFileConfig(path string) (FileConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileConfig{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return FileConfig{}, err
	}
	fc, err := ParseFileConfig(data)
	if err != nil {
		return fc, fmt.Errorf("%s: %w", path, err)
	}
	return fc, nil
}

// YAML renders the configuration in the format of the configuration file.
func (c Config) YAML() string {
	var fc FileConfig
	lang, mode := string(c.Lang), string(c.Sandbox)
	fc.Listen, fc.DataDir, fc.Language, fc.Sandbox = &c.Addr, &c.DataDir, &lang, &mode
	fc.TLS.Cert, fc.TLS.Key = &c.TLSCert, &c.TLSKey
	fc.Chat.Enabled, fc.Chat.Oversight, fc.Chat.OversightToggle = &c.Chat, &c.ChatOversight, &c.ChatOversightToggle
	fc.Development.InsecureHTTP, fc.Development.DemoAccounts = &c.InsecureHTTP, &c.DevSeed
	out, err := yaml.Marshal(fc)
	if err != nil {
		panic(err) // plain strings and booleans always marshal
	}
	return string(out)
}

// ErrHelp is returned when -h was given; usage has been printed.
var ErrHelp = flag.ErrHelp

func parseBool(what, v string) (bool, error) {
	switch strings.ToLower(v) {
	case "1", "true", "on", "yes":
		return true, nil
	case "0", "false", "off", "no":
		return false, nil
	}
	return false, fmt.Errorf("%s: %q is not on or off", what, v)
}

// ParseConfig determines the configuration from the command line, the
// environment and the configuration file named by -config or
// GR_REPORT_CONFIG.
func ParseConfig(args []string, getenv func(string) string, out io.Writer) (Config, error) {
	var cfg Config
	var f struct {
		config, addr, port, data, cert, key, mode, lang string
		insecure, seed, chat, oversight, toggle         bool
	}
	fs := flag.NewFlagSet("gr_report", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() {
		fmt.Fprintln(out, "Usage: gr_report [-config FILE] [flags]")
		fmt.Fprintln(out, "\nAll settings can be stored in a YAML configuration file (see docs/operations.md).")
		fmt.Fprintln(out, "Environment variables override the file, flags override both.\n\nFlags:")
		fs.PrintDefaults()
	}
	fs.StringVar(&f.config, "config", "", "YAML configuration `file` (env GR_REPORT_CONFIG)")
	fs.StringVar(&f.addr, "addr", "", "listen `address` as host:port; default :8443 (file: listen, env GR_REPORT_ADDR)")
	fs.StringVar(&f.port, "port", "", "listen `port`; replaces the port of the listen address (env GR_REPORT_PORT)")
	fs.StringVar(&f.data, "data", "", "data `directory`; default ./data (file: data_dir, env GR_REPORT_DATA)")
	fs.StringVar(&f.cert, "tls-cert", "", "TLS certificate chain, PEM `file` (file: tls.cert, env GR_REPORT_TLS_CERT)")
	fs.StringVar(&f.key, "tls-key", "", "TLS private key, PEM `file` (file: tls.key, env GR_REPORT_TLS_KEY)")
	fs.BoolVar(&f.insecure, "insecure-http", false, "serve plain HTTP without TLS; development only (file: development.insecure_http, env GR_REPORT_INSECURE_HTTP)")
	fs.BoolVar(&f.seed, "dev-seed", false, "create demo accounts with a known password on first start; needs insecure HTTP (file: development.demo_accounts)")
	fs.StringVar(&f.mode, "sandbox", "", "in-process sandbox `mode`: auto, strict or off; default auto (file: sandbox, env GR_REPORT_SANDBOX)")
	fs.StringVar(&f.lang, "lang", "", "default user interface `language`: en or sv; default en (file: language, env GR_REPORT_LANG)")
	fs.BoolVar(&f.chat, "chat", true, "chat between participants; -chat=false switches it off (file: chat.enabled, env GR_REPORT_CHAT)")
	fs.BoolVar(&f.oversight, "chat-oversight", false, "let the exercise leader read chat between groups; the groups are told (file: chat.oversight, env GR_REPORT_CHAT_OVERSIGHT)")
	fs.BoolVar(&f.toggle, "chat-oversight-toggle", true, "let the exercise leader switch oversight on and off in the user interface; -chat-oversight-toggle=false leaves it to the configuration (file: chat.oversight_toggle, env GR_REPORT_CHAT_OVERSIGHT_TOGGLE)")
	fs.BoolVar(&cfg.PrintConfig, "print-config", false, "print the effective configuration as YAML and exit")
	fs.BoolVar(&cfg.ShowVersion, "version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if fs.NArg() > 0 {
		return cfg, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if cfg.ShowVersion {
		return cfg, nil
	}
	given := map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { given[fl.Name] = true })

	// The configuration file.
	cfg.File = f.config
	if cfg.File == "" {
		cfg.File = getenv("GR_REPORT_CONFIG")
	}
	var fc FileConfig
	baseDir := ""
	if cfg.File != "" {
		abs, err := filepath.Abs(cfg.File)
		if err != nil {
			return cfg, err
		}
		cfg.File, baseDir = abs, filepath.Dir(abs)
		if fc, err = loadFileConfig(abs); err != nil {
			return cfg, fmt.Errorf("configuration file: %w", err)
		}
	}
	// A relative path in the file is relative to the file, not to the
	// directory the server happens to be started from.
	filePath := func(p *string) *string {
		if p == nil || *p == "" || filepath.IsAbs(*p) {
			return p
		}
		abs := filepath.Join(baseDir, *p)
		return &abs
	}

	// flag > environment > file > default
	str := func(flagName, flagVal, envKey string, fileVal *string, def string) string {
		switch {
		case given[flagName]:
			return flagVal
		case envKey != "" && getenv(envKey) != "":
			return getenv(envKey)
		case fileVal != nil:
			return *fileVal
		}
		return def
	}
	var boolErr error
	boolean := func(flagName string, flagVal bool, envKey string, fileVal *bool, def bool) bool {
		switch {
		case given[flagName]:
			return flagVal
		case envKey != "" && getenv(envKey) != "":
			v, err := parseBool(envKey, getenv(envKey))
			if err != nil && boolErr == nil {
				boolErr = err
			}
			return v
		case fileVal != nil:
			return *fileVal
		}
		return def
	}
	cfg.Addr = str("addr", f.addr, "GR_REPORT_ADDR", fc.Listen, ":8443")
	cfg.DataDir = str("data", f.data, "GR_REPORT_DATA", filePath(fc.DataDir), "./data")
	cfg.TLSCert = str("tls-cert", f.cert, "GR_REPORT_TLS_CERT", filePath(fc.TLS.Cert), "")
	cfg.TLSKey = str("tls-key", f.key, "GR_REPORT_TLS_KEY", filePath(fc.TLS.Key), "")
	mode := str("sandbox", f.mode, "GR_REPORT_SANDBOX", fc.Sandbox, string(sandbox.ModeAuto))
	lang := str("lang", f.lang, "GR_REPORT_LANG", fc.Language, string(i18n.EN))
	port := str("port", f.port, "GR_REPORT_PORT", nil, "")
	cfg.InsecureHTTP = boolean("insecure-http", f.insecure, "GR_REPORT_INSECURE_HTTP", fc.Development.InsecureHTTP, false)
	cfg.DevSeed = boolean("dev-seed", f.seed, "", fc.Development.DemoAccounts, false)
	cfg.Chat = boolean("chat", f.chat, "GR_REPORT_CHAT", fc.Chat.Enabled, true)
	cfg.ChatOversight = boolean("chat-oversight", f.oversight, "GR_REPORT_CHAT_OVERSIGHT", fc.Chat.Oversight, false)
	cfg.ChatOversightToggle = boolean("chat-oversight-toggle", f.toggle, "GR_REPORT_CHAT_OVERSIGHT_TOGGLE", fc.Chat.OversightToggle, true)
	if boolErr != nil {
		return cfg, boolErr
	}

	var err error
	if cfg.Sandbox, err = sandbox.ParseMode(mode); err != nil {
		return cfg, err
	}
	var ok bool
	if cfg.Lang, ok = i18n.Parse(lang); !ok {
		return cfg, fmt.Errorf("unsupported language %q (want en or sv)", lang)
	}
	host, addrPort, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return cfg, fmt.Errorf("invalid listen address %q: %w", cfg.Addr, err)
	}
	if port != "" {
		addrPort = port
	}
	if n, err := strconv.Atoi(addrPort); err != nil || n < 0 || n > 65535 {
		return cfg, fmt.Errorf("invalid port %q", addrPort)
	}
	cfg.Addr = net.JoinHostPort(host, addrPort)
	if cfg.DataDir == "" {
		return cfg, errors.New("the data directory must not be empty")
	}
	hasTLS := cfg.TLSCert != "" || cfg.TLSKey != ""
	switch {
	case cfg.InsecureHTTP && hasTLS:
		return cfg, errors.New("insecure HTTP cannot be combined with a TLS certificate or key")
	case !cfg.InsecureHTTP && (cfg.TLSCert == "" || cfg.TLSKey == ""):
		return cfg, errors.New("TLS is required: set tls.cert and tls.key (see scripts/), or development.insecure_http for local development")
	case cfg.DevSeed && !cfg.InsecureHTTP:
		return cfg, errors.New("demo accounts are only allowed together with insecure HTTP")
	}
	return cfg, nil
}
