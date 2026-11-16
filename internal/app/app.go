package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/rom/gr_report/internal/auth"
	"github.com/rom/gr_report/internal/i18n"
	"github.com/rom/gr_report/internal/model"
	"github.com/rom/gr_report/internal/sandbox"
	"github.com/rom/gr_report/internal/store"
	"github.com/rom/gr_report/internal/web"
)

// InitialPasswordFile is created in the data directory on first start.
const InitialPasswordFile = "initial-admin-password"

// DevPassword is the password of the accounts created by -dev-seed.
const DevPassword = "exercise-dev-1"

// App is a configured server instance.
type App struct {
	Cfg    Config
	Log    *slog.Logger
	Hasher auth.Hasher
	// Reload triggers re-reading of the TLS certificate (SIGHUP).
	Reload <-chan struct{}
	// Ready, if set, is called with the bound address once the server
	// accepts connections.
	Ready func(net.Addr)
}

// certStore serves the current TLS certificate and can swap it at runtime.
type certStore struct {
	certFile, keyFile string
	cur               atomic.Pointer[tls.Certificate]
}

func (c *certStore) load() error {
	cert, err := tls.LoadX509KeyPair(c.certFile, c.keyFile)
	if err != nil {
		return err
	}
	c.cur.Store(&cert)
	return nil
}

func (c *certStore) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return c.cur.Load(), nil
}

// Bootstrap prepares an empty data directory: it creates the administrator
// account with a random one-time password (written to a 0600 file) and two
// starter templates.
func (a *App) Bootstrap(st *store.Store) error {
	now := time.Now().UTC()
	if len(st.Templates(true)) == 0 {
		for _, t := range starterTemplates(a.Cfg.Lang) {
			t.ID, t.Created = model.NewID(), now
			if err := t.Validate(); err != nil {
				return err
			}
			if err := st.PutTemplate(t); err != nil {
				return err
			}
		}
	}
	if len(st.Users()) > 0 {
		return nil
	}
	add := func(username, name string, role model.Role, pw string, mustChange bool) error {
		hash, err := a.Hasher.Hash(pw)
		if err != nil {
			return err
		}
		u := model.User{ID: model.NewID(), Username: username, DisplayName: name, Role: role,
			PassHash: hash, MustChange: mustChange, Created: now}
		if err := u.Validate(); err != nil {
			return err
		}
		return st.PutUser(u)
	}
	if a.Cfg.DevSeed {
		a.Log.Warn("creating demo accounts with a publicly known password; never use -dev-seed in an exercise",
			"accounts", "admin, leader, alpha, bravo", "password", DevPassword)
		for _, d := range []struct {
			user, name string
			role       model.Role
		}{{"admin", "Administrator", model.RoleAdmin}, {"leader", "Exercise leader", model.RoleLeader},
			{"alpha", "Group Alpha", model.RoleGroup}, {"bravo", "Group Bravo", model.RoleGroup}} {
			if err := add(d.user, d.name, d.role, DevPassword, false); err != nil {
				return err
			}
		}
		return nil
	}
	pw := auth.GeneratePassword()
	path := filepath.Join(a.Cfg.DataDir, InitialPasswordFile)
	content := fmt.Sprintf("username: admin\npassword: %s\n\nOne-time password; you must change it at first login. Delete this file afterwards.\n", pw)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return err
	}
	if err := add("admin", "Administrator", model.RoleAdmin, pw, true); err != nil {
		return err
	}
	a.Log.Info("created administrator account; the one-time password is in the file", "username", "admin", "file", path)
	return nil
}

// starterTemplates returns the templates created in an empty database, in
// the configured default language. They are ordinary templates afterwards.
func starterTemplates(lang i18n.Lang) []model.Template {
	if lang == i18n.SV {
		return []model.Template{
			{Name: "Minimal rapport", Description: "Fritextrapport med enbart de obligatoriska delarna: tid, avsändare, mottagare, ämne och innehåll."},
			{
				Name: "Lägesrapport", DefaultSubject: "Lägesrapport",
				Description: "Återkommande lägesrapport. Beskriv vad som har hänt, vad ni gör och vad ni behöver.",
				ContentHelp: "Sammanfattning av läget och planerade åtgärder",
				Fields: []model.Field{
					{Label: "Plats", Type: model.FieldText, Required: true},
					{Label: "Läget avser", Type: model.FieldDatetime, Help: "lokal tid för observationen"},
					{Label: "Status", Type: model.FieldSelect, Required: true, Options: []string{"Grön", "Gul", "Röd"}},
					{Label: "Personal", Type: model.FieldNumber, Help: "antal personer"},
					{Label: "Behöver stöd", Type: model.FieldCheckbox},
					{Label: "Behov och önskemål", Type: model.FieldTextarea},
				},
			},
		}
	}
	return []model.Template{
		{Name: "Minimal report", Description: "Free-text report with only the mandatory parts: time, sender, receiver, subject and content."},
		{
			Name: "Situation report (SITREP)", DefaultSubject: "SITREP",
			Description: "Periodic situation report. Describe what has happened, what you are doing and what you need.",
			ContentHelp: "Summary of the situation and planned actions",
			Fields: []model.Field{
				{Label: "Location", Type: model.FieldText, Required: true},
				{Label: "Situation as of", Type: model.FieldDatetime, Help: "local time of the observation"},
				{Label: "Status", Type: model.FieldSelect, Required: true, Options: []string{"Green", "Amber", "Red"}},
				{Label: "Personnel", Type: model.FieldNumber, Help: "head count"},
				{Label: "Assistance needed", Type: model.FieldCheckbox},
				{Label: "Needs and requests", Type: model.FieldTextarea},
			},
		},
	}
}

// Run starts the server and blocks until ctx is cancelled or serving fails.
func (a *App) Run(ctx context.Context) error {
	cfg := a.Cfg
	dataDir, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return err
	}
	a.Cfg.DataDir = dataDir
	st, err := store.Open(dataDir)
	if err != nil {
		return fmt.Errorf("open data directory %s: %w", dataDir, err)
	}
	defer st.Close()
	if err := a.Bootstrap(st); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	sb := sandbox.Config{Mode: cfg.Sandbox, ReadWrite: []string{dataDir}}
	var certs *certStore
	if !cfg.InsecureHTTP {
		certs = &certStore{}
		if certs.certFile, err = filepath.Abs(cfg.TLSCert); err != nil {
			return err
		}
		if certs.keyFile, err = filepath.Abs(cfg.TLSKey); err != nil {
			return err
		}
		if err := certs.load(); err != nil {
			return fmt.Errorf("load TLS certificate: %w", err)
		}
		// Whole directories stay readable so a renewed certificate that
		// replaces the files can be picked up on reload.
		sb.ReadOnly = []string{filepath.Dir(certs.certFile)}
		if d := filepath.Dir(certs.keyFile); d != sb.ReadOnly[0] {
			sb.ReadOnly = append(sb.ReadOnly, d)
		}
	} else {
		a.Log.Warn("serving plain HTTP: passwords and reports are sent unencrypted; development only")
	}

	srv := web.New(st, a.Log, !cfg.InsecureHTTP)
	srv.Hasher = a.Hasher
	if a.Cfg.Lang != "" {
		srv.DefaultLang = a.Cfg.Lang
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer ln.Close()

	// Everything the process needs is open now: lock it down.
	status, err := sandbox.Apply(sb)
	if err != nil {
		return fmt.Errorf("sandbox: %w", err)
	}
	a.Log.Info("sandbox", "mode", string(cfg.Sandbox), "platform", status.Platform,
		"no_new_privs", status.NoNewPrivs, "caps_dropped", status.CapsDropped,
		"landlock_abi", status.LandlockABI, "landlock_net", status.LandlockNet,
		"seccomp", status.Seccomp, "core_dumps_disabled", status.CoreDisabled, "notes", status.Notes)

	hs := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    32 << 10,
		ErrorLog:          slog.NewLogLogger(a.Log.Handler(), slog.LevelDebug),
	}
	scheme := "http"
	if certs != nil {
		scheme = "https"
		hs.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: certs.get}
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			case _, ok := <-a.Reload:
				if !ok {
					return
				}
				if certs == nil {
					continue
				}
				if err := certs.load(); err != nil {
					a.Log.Error("TLS certificate reload failed; keeping the old one", "err", err)
				} else {
					a.Log.Info("TLS certificate reloaded")
				}
			}
		}
	}()

	errc := make(chan error, 1)
	go func() {
		if certs != nil {
			errc <- hs.ServeTLS(ln, "", "")
		} else {
			errc <- hs.Serve(ln)
		}
	}()
	a.Log.Info("listening", "url", scheme+"://"+ln.Addr().String(), "version", Version)
	if a.Ready != nil {
		a.Ready(ln.Addr())
	}

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	a.Log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := hs.Shutdown(sctx); err != nil {
		return err
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
