//go:build linux

package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestEndToEndSandboxed builds the real binary and runs it with TLS and the
// in-process sandbox, then works through a login, a report and a
// certificate reload. It proves that the Go runtime and the server keep
// working under Landlock and seccomp.
func TestEndToEndSandboxed(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the binary")
	}
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl not available")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gr_report")
	build := exec.Command("go", "build", "-o", bin, "./cmd/gr_report")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	tlsDir := filepath.Join(dir, "tls")
	mk := func() {
		out, err := exec.Command("sh", filepath.Join(root, "scripts/mkcert-selfsigned.sh"), "-f", "-o", tlsDir).CombinedOutput()
		if err != nil {
			t.Fatalf("mkcert: %v\n%s", err, out)
		}
	}
	mk()
	if fi, err := os.Stat(filepath.Join(tlsDir, "key.pem")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", fi, err)
	}

	// All settings come from a configuration file, with paths relative to
	// it, as in an installation.
	conf := filepath.Join(dir, "gr_report.yaml")
	yaml := "listen: \"127.0.0.1:0\"\ndata_dir: data\ntls:\n  cert: tls/cert.pem\n  key: tls/key.pem\nsandbox: auto\nchat:\n  enabled: true\n  oversight: true\n"
	if err := os.WriteFile(conf, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-config", conf)
	cmd.Dir = "/" // relative paths must not depend on the working directory
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	lines := make(chan string, 100)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	waitLine := func(re string) string {
		t.Helper()
		rx := regexp.MustCompile(re)
		deadline := time.After(20 * time.Second)
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatalf("server exited before logging %q", re)
				}
				t.Log(l)
				if rx.MatchString(l) {
					return l
				}
			case <-deadline:
				t.Fatalf("timeout waiting for %q", re)
			}
		}
	}
	if l := waitLine(`msg=configuration`); !strings.Contains(l, conf) {
		t.Errorf("configuration file not reported: %s", l)
	}
	waitLine(`chat oversight is on`)
	sandboxLine := waitLine(`msg=sandbox`)
	if !strings.Contains(sandboxLine, "seccomp=true") || !strings.Contains(sandboxLine, "no_new_privs=true") {
		t.Errorf("sandbox not active: %s", sandboxLine)
	}
	base := regexp.MustCompile(`url=(https://\S+)`).FindStringSubmatch(waitLine(`msg=listening`))[1]

	client := func() *http.Client {
		pemBytes, err := os.ReadFile(filepath.Join(tlsDir, "cert.pem"))
		if err != nil {
			t.Fatal(err)
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(pemBytes)
		jar, _ := cookiejar.New(nil)
		return &http.Client{Jar: jar, Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	}
	page := func(resp *http.Response, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d\n%s", resp.Request.URL, resp.StatusCode, b)
		}
		return string(b)
	}
	c := client()
	raw, err := os.ReadFile(filepath.Join(dir, "data", "initial-admin-password"))
	if err != nil {
		t.Fatal(err)
	}
	otp := regexp.MustCompile(`password: (\S+)`).FindSubmatch(raw)[1]
	csrfRe := regexp.MustCompile(`name="csrf" value="([^"]+)"`)
	body := page(c.PostForm(base+"/login", url.Values{"username": {"admin"}, "password": {string(otp)}}))
	csrf := csrfRe.FindStringSubmatch(body)[1]
	// These writes hit the journal (fsync) from inside the sandbox.
	body = page(c.PostForm(base+"/password", url.Values{"csrf": {csrf}, "current": {string(otp)}, "new": {"e2e-admin-password"}, "confirm": {"e2e-admin-password"}}))
	if !strings.Contains(body, "Password changed") {
		t.Fatalf("password change failed:\n%s", body)
	}
	csrf = csrfRe.FindStringSubmatch(body)[1]
	body = page(c.PostForm(base+"/admin/users", url.Values{"csrf": {csrf}, "username": {"alpha"}, "display_name": {"Group Alpha"}, "role": {"group"}}))
	if !strings.Contains(body, "created") {
		t.Fatalf("user creation failed:\n%s", body)
	}

	// Certificate renewal: new files, SIGHUP, no restart.
	mk()
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	waitLine(`certificate reloaded`)
	page(client().Get(base + "/healthz"))

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitLine(`shutting down`)
	if err := cmd.Wait(); err != nil {
		t.Errorf("exit: %v", err)
	}
}
