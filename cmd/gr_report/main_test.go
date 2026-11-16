package main

import (
	"bytes"
	"strings"
	"testing"
)

func noenv(string) string { return "" }

func TestRunFlags(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"-version"}, noenv, &out, &errb); code != 0 || !strings.HasPrefix(out.String(), "gr_report ") {
		t.Errorf("-version: code %d, %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"-h"}, noenv, &out, &errb); code != 0 || !strings.Contains(errb.String(), "-sandbox") {
		t.Errorf("-h: code %d, %q", code, errb.String())
	}
	errb.Reset()
	if code := run(nil, noenv, &out, &errb); code != 2 || !strings.Contains(errb.String(), "TLS is required") {
		t.Errorf("no TLS: code %d, %q", code, errb.String())
	}
	errb.Reset()
	// -print-config shows the effective settings without starting.
	out.Reset()
	if code := run([]string{"-insecure-http", "-port", "9999", "-print-config"}, noenv, &out, &errb); code != 0 ||
		!strings.Contains(out.String(), `listen: :9999`) || !strings.Contains(out.String(), "insecure_http: true") {
		t.Errorf("-print-config: code %d, %q %q", code, out.String(), errb.String())
	}
	if code := run([]string{"-config", "/nonexistent.yaml"}, noenv, &out, &errb); code != 2 || !strings.Contains(errb.String(), "configuration file") {
		t.Errorf("missing config file: code %d, %q", code, errb.String())
	}
	errb.Reset()
	// A start-up failure is reported with exit code 1.
	args := []string{"-insecure-http", "-sandbox", "off", "-addr", "127.0.0.1:0", "-data", "/proc/nonexistent/data"}
	if code := run(args, noenv, &out, &errb); code != 1 || !strings.Contains(errb.String(), "level=ERROR") {
		t.Errorf("bad data dir: code %d, %q", code, errb.String())
	}
}
