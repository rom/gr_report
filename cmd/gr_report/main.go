// Command gr_report is a small web based reporting tool for exercises:
// groups send reports to the exercise leader, who works through a queue per
// group. See docs/ for documentation.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rom/gr_report/internal/app"
	"github.com/rom/gr_report/internal/store"
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	cfg, err := app.ParseConfig(args, getenv, stderr)
	if errors.Is(err, app.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "gr_report:", err)
		return 2
	}
	if cfg.ShowVersion {
		fmt.Fprintln(stdout, "gr_report", app.Version)
		return 0
	}
	if cfg.PrintConfig {
		fmt.Fprint(stdout, cfg.YAML())
		return 0
	}
	if cfg.BackupTo != "" {
		if err := store.BackupFile(cfg.DataDir, cfg.BackupTo); err != nil {
			fmt.Fprintln(stderr, "gr_report: backup:", err)
			return 1
		}
		fmt.Fprintln(stdout, "backup written to", cfg.BackupTo)
		return 0
	}
	log := slog.New(slog.NewTextHandler(stderr, nil))
	if cfg.File != "" {
		log.Info("configuration", "file", cfg.File)
	} else {
		log.Info("configuration", "file", "none; using flags, environment and defaults")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	reload := make(chan struct{}, 1)
	go func() {
		for range hup {
			select {
			case reload <- struct{}{}:
			default:
			}
		}
	}()

	a := &app.App{Cfg: cfg, Log: log, Reload: reload}
	if err := a.Run(ctx); err != nil {
		log.Error("fatal", "err", err)
		return 1
	}
	return 0
}
