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
	// A start-up failure is reported with exit code 1.
	args := []string{"-insecure-http", "-sandbox", "off", "-addr", "127.0.0.1:0", "-data", "/proc/nonexistent/data"}
	if code := run(args, noenv, &out, &errb); code != 1 || !strings.Contains(errb.String(), "level=ERROR") {
		t.Errorf("bad data dir: code %d, %q", code, errb.String())
	}
}
