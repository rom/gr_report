// Package app wires the parts of gr_report together: configuration,
// storage, first-run bootstrap, TLS, the sandbox and the HTTP server.
package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/rom/gr_report/internal/sandbox"
)

// Version is set at build time with -ldflags "-X ...app.Version=...".
var Version = "dev"

// Config is the runtime configuration. Every flag can also be set with an
// environment variable (GR_REPORT_<FLAG>); flags win.
type Config struct {
	Addr         string
	DataDir      string
	TLSCert      string
	TLSKey       string
	InsecureHTTP bool
	DevSeed      bool
	Sandbox      sandbox.Mode
	ShowVersion  bool
}

// ErrHelp is returned when -h was given; usage has been printed.
var ErrHelp = flag.ErrHelp

// ParseConfig reads flags and environment.
func ParseConfig(args []string, getenv func(string) string, out io.Writer) (Config, error) {
	env := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}
	var cfg Config
	var port, mode string
	fs := flag.NewFlagSet("gr_report", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&cfg.Addr, "addr", env("GR_REPORT_ADDR", ":8443"), "listen `address` as host:port (env GR_REPORT_ADDR)")
	fs.StringVar(&port, "port", env("GR_REPORT_PORT", ""), "listen `port`; overrides the port of -addr (env GR_REPORT_PORT)")
	fs.StringVar(&cfg.DataDir, "data", env("GR_REPORT_DATA", "./data"), "data `directory` (env GR_REPORT_DATA)")
	fs.StringVar(&cfg.TLSCert, "tls-cert", env("GR_REPORT_TLS_CERT", ""), "TLS certificate chain, PEM `file` (env GR_REPORT_TLS_CERT)")
	fs.StringVar(&cfg.TLSKey, "tls-key", env("GR_REPORT_TLS_KEY", ""), "TLS private key, PEM `file` (env GR_REPORT_TLS_KEY)")
	fs.BoolVar(&cfg.InsecureHTTP, "insecure-http", env("GR_REPORT_INSECURE_HTTP", "") == "1", "serve plain HTTP without TLS; development only (env GR_REPORT_INSECURE_HTTP=1)")
	fs.BoolVar(&cfg.DevSeed, "dev-seed", false, "create demo accounts with a known password on first start; needs -insecure-http")
	fs.StringVar(&mode, "sandbox", env("GR_REPORT_SANDBOX", string(sandbox.ModeAuto)), "in-process sandbox `mode`: auto, strict or off (env GR_REPORT_SANDBOX)")
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
	var err error
	if cfg.Sandbox, err = sandbox.ParseMode(mode); err != nil {
		return cfg, err
	}
	host, addrPort, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return cfg, fmt.Errorf("invalid -addr %q: %w", cfg.Addr, err)
	}
	if port != "" {
		addrPort = port
	}
	if n, err := strconv.Atoi(addrPort); err != nil || n < 0 || n > 65535 {
		return cfg, fmt.Errorf("invalid port %q", addrPort)
	}
	cfg.Addr = net.JoinHostPort(host, addrPort)
	if cfg.DataDir == "" {
		return cfg, errors.New("-data must not be empty")
	}
	hasTLS := cfg.TLSCert != "" || cfg.TLSKey != ""
	switch {
	case cfg.InsecureHTTP && hasTLS:
		return cfg, errors.New("-insecure-http cannot be combined with -tls-cert/-tls-key")
	case !cfg.InsecureHTTP && (cfg.TLSCert == "" || cfg.TLSKey == ""):
		return cfg, errors.New("TLS is required: give -tls-cert and -tls-key (see scripts/), or -insecure-http for local development")
	case cfg.DevSeed && !cfg.InsecureHTTP:
		return cfg, errors.New("-dev-seed is only allowed together with -insecure-http")
	}
	return cfg, nil
}
