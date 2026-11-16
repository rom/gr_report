// Package sandbox confines the running process after start-up: it drops
// capabilities, sets no_new_privs, restricts file system and network access
// with Landlock and installs a seccomp filter. On platforms other than Linux
// it is a no-op so the tool can be developed on macOS.
//
// The in-process sandbox is one layer; the systemd unit, the SELinux module
// and the AppArmor profile in deploy/ add independent outer layers. See
// docs/security.md.
package sandbox

import (
	"errors"
	"fmt"
	"strings"
)

// Mode selects how sandbox failures are handled.
type Mode string

// Sandbox modes.
const (
	// ModeAuto applies everything the kernel supports and reports the rest.
	ModeAuto Mode = "auto"
	// ModeStrict fails start-up unless every mechanism could be applied.
	ModeStrict Mode = "strict"
	// ModeOff disables the in-process sandbox (development only).
	ModeOff Mode = "off"
)

// ParseMode converts a command line value to a Mode.
func ParseMode(s string) (Mode, error) {
	switch m := Mode(strings.ToLower(s)); m {
	case ModeAuto, ModeStrict, ModeOff:
		return m, nil
	}
	return "", fmt.Errorf("unknown sandbox mode %q (want auto, strict or off)", s)
}

// Config describes what the process still needs after start-up.
type Config struct {
	Mode Mode
	// ReadWrite lists directories that stay writable (the data directory).
	ReadWrite []string
	// ReadOnly lists files or directories that stay readable (TLS
	// material that is re-read on reload).
	ReadOnly []string
}

// Status reports which mechanisms are active.
type Status struct {
	Platform     string
	NoNewPrivs   bool
	CapsDropped  bool
	LandlockABI  int  // 0: not active
	LandlockNet  bool // TCP bind/connect denied by Landlock
	Seccomp      bool
	CoreDisabled bool
	Notes        []string // why a mechanism is missing
}

// Complete reports whether every mechanism is active.
func (s Status) Complete() bool {
	return s.NoNewPrivs && s.CapsDropped && s.LandlockABI > 0 && s.LandlockNet && s.Seccomp && s.CoreDisabled
}

func (s *Status) note(format string, a ...any) {
	s.Notes = append(s.Notes, fmt.Sprintf(format, a...))
}

// ErrIncomplete is returned in strict mode when a mechanism is unavailable.
var ErrIncomplete = errors.New("sandbox could not be fully applied")

// Apply confines the process. It must be called after all listeners and
// files needed for the lifetime of the process are open.
func Apply(cfg Config) (Status, error) {
	if cfg.Mode == ModeOff {
		st := Status{Platform: platform}
		st.note("sandbox disabled by configuration")
		return st, nil
	}
	st, err := apply(cfg)
	if err != nil {
		return st, err
	}
	if cfg.Mode == ModeStrict && !st.Complete() {
		return st, fmt.Errorf("%w: %s", ErrIncomplete, strings.Join(st.Notes, "; "))
	}
	return st, nil
}
