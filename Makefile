# gr_report - build, test and install.
# `make help` lists the targets.

GO          ?= go
BIN         := gr_report
PKG         := github.com/rom/gr_report
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X $(PKG)/internal/app.Version=$(VERSION)

# Listen port for `make dev` and `make run`.
PORT        ?=
DEV_PORT    := $(if $(PORT),$(PORT),8080)
RUN_PORT    := $(if $(PORT),$(PORT),8443)
HOST        ?= 127.0.0.1
DATA        ?= ./data
TLS_DIR     ?= ./tls
SANDBOX     ?= auto

# Quality gates.
COVER_MIN   ?= 85
FUZZTIME    ?= 10s

# Installation.
PREFIX      ?= /usr/local
DESTDIR     ?=
SYSCONFDIR  ?= /etc
UNITDIR     ?= /etc/systemd/system

# CGO_ENABLED=0 gives a fully static binary on Linux (no dynamic loader).
BUILDFLAGS  := -trimpath -ldflags '$(LDFLAGS)'

.DEFAULT_GOAL := build

.PHONY: help
help: ## Show this help
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z0-9_-]+:.*## / { printf "  %-20s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# ---- build ---------------------------------------------------------------

.PHONY: build
build: ## Build bin/gr_report for this platform (static on Linux)
	CGO_ENABLED=0 $(GO) build $(BUILDFLAGS) -o bin/$(BIN) ./cmd/gr_report

.PHONY: build-linux
build-linux: ## Cross-compile Linux amd64 and arm64 binaries into dist/
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(BIN)-linux-amd64 ./cmd/gr_report
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(BIN)-linux-arm64 ./cmd/gr_report

.PHONY: build-darwin
build-darwin: ## Cross-compile macOS development binaries into dist/
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(BIN)-darwin-arm64 ./cmd/gr_report
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(BIN)-darwin-amd64 ./cmd/gr_report

# ---- run -----------------------------------------------------------------

.PHONY: dev
dev: build ## Run a development server: plain HTTP on localhost, demo accounts (PORT=8080)
	./bin/$(BIN) -insecure-http -dev-seed -addr $(HOST):$(DEV_PORT) -data $(DATA) -sandbox $(SANDBOX)

.PHONY: run
run: build $(TLS_DIR)/cert.pem ## Run with TLS using the certificate in ./tls (PORT=8443, HOST=127.0.0.1)
	./bin/$(BIN) -addr $(HOST):$(RUN_PORT) -data $(DATA) -sandbox $(SANDBOX) \
		-tls-cert $(TLS_DIR)/cert.pem -tls-key $(TLS_DIR)/key.pem

$(TLS_DIR)/cert.pem:
	sh scripts/mkcert-selfsigned.sh -o $(TLS_DIR)

.PHONY: cert-selfsigned
cert-selfsigned: ## Create a self-signed certificate in ./tls (NAME=host ALT=names,ips)
	sh scripts/mkcert-selfsigned.sh -f -o $(TLS_DIR) $(if $(NAME),-n $(NAME)) $(if $(ALT),-a $(ALT))

.PHONY: cert-letsencrypt
cert-letsencrypt: ## Obtain a Let's Encrypt certificate (NAME=host EMAIL=addr, as root)
	@test -n "$(NAME)" && test -n "$(EMAIL)" || { echo "usage: make cert-letsencrypt NAME=report.example.org EMAIL=you@example.org"; exit 2; }
	sh scripts/mkcert-letsencrypt.sh -n $(NAME) -m $(EMAIL)

# ---- quality -------------------------------------------------------------

.PHONY: test
test: ## Run unit, integration and end-to-end tests
	CGO_ENABLED=0 $(GO) test -count=1 ./...

.PHONY: test-short
test-short: ## Run the tests without the end-to-end test that builds the binary
	CGO_ENABLED=0 $(GO) test -count=1 -short ./...

.PHONY: test-race
test-race: ## Run the tests with the race detector (needs cgo)
	CGO_ENABLED=1 $(GO) test -count=1 -race ./...

.PHONY: cover
cover: ## Run tests with coverage; fails below COVER_MIN (85) percent
	sh scripts/check-coverage.sh $(COVER_MIN) coverage.out

.PHONY: cover-html
cover-html: cover ## Write an HTML coverage report to coverage.html
	$(GO) tool cover -html=coverage.out -o coverage.html

.PHONY: fuzz
fuzz: ## Run every fuzz target for FUZZTIME (10s) each
	sh scripts/fuzz.sh $(FUZZTIME)

.PHONY: fmt
fmt: ## Format the Go sources
	gofmt -w cmd internal

.PHONY: lint
lint: ## gofmt, go vet (Linux, arm64, macOS), golangci-lint and shellcheck when installed
	@test -z "$$(gofmt -l cmd internal)" || { echo "gofmt needed:"; gofmt -l cmd internal; exit 1; }
	$(GO) vet ./...
	GOOS=linux GOARCH=arm64 $(GO) vet ./...
	GOOS=darwin GOARCH=arm64 $(GO) vet ./...
	@if command -v golangci-lint >/dev/null; then golangci-lint run ./...; else echo "golangci-lint not installed, skipped"; fi
	@if command -v shellcheck >/dev/null; then shellcheck scripts/*.sh; else echo "shellcheck not installed, skipped"; fi

.PHONY: verify-deploy
verify-deploy: ## Validate the systemd unit, AppArmor profile and SELinux module when the tools exist
	@if command -v systemd-analyze >/dev/null; then \
		tmp=$$(mktemp -d) && sed 's#^ExecStart=.*#ExecStart=/bin/true#; s#^EnvironmentFile=#EnvironmentFile=-#' deploy/systemd/gr_report.service > $$tmp/gr_report.service && \
		systemd-analyze verify $$tmp/gr_report.service && systemd-analyze security --offline=yes --threshold=20 $$tmp/gr_report.service | tail -1; rm -rf $$tmp; \
	else echo "systemd-analyze not installed, skipped"; fi
	@if command -v apparmor_parser >/dev/null; then apparmor_parser -Q -K deploy/apparmor/usr.local.bin.gr_report && echo "AppArmor profile OK"; else echo "apparmor_parser not installed, skipped"; fi
	@if [ -f /usr/share/selinux/devel/Makefile ]; then \
		tmp=$$(mktemp -d) && cp deploy/selinux/gr_report.* $$tmp/ && $(MAKE) -s -C $$tmp -f /usr/share/selinux/devel/Makefile gr_report.pp && echo "SELinux module OK"; rm -rf $$tmp; \
	else echo "selinux-policy-devel not installed, skipped"; fi

.PHONY: check
check: lint cover fuzz verify-deploy build-darwin ## Everything CI runs

# ---- install (Linux, as root) ----------------------------------------------

.PHONY: install
install: build ## Install binary, systemd unit, service user and default config
	install -D -m 0755 bin/$(BIN) $(DESTDIR)$(PREFIX)/bin/$(BIN)
	install -D -m 0644 deploy/systemd/gr_report.service $(DESTDIR)$(UNITDIR)/gr_report.service
	sed -i 's#^ExecStart=.*#ExecStart=$(PREFIX)/bin/$(BIN)#' $(DESTDIR)$(UNITDIR)/gr_report.service
	install -D -m 0644 deploy/systemd/sysusers.conf $(DESTDIR)/usr/lib/sysusers.d/gr_report.conf
	install -D -m 0644 deploy/systemd/tmpfiles.conf $(DESTDIR)/usr/lib/tmpfiles.d/gr_report.conf
	test -e $(DESTDIR)$(SYSCONFDIR)/gr_report/gr_report.env || \
		install -D -m 0644 deploy/systemd/gr_report.env $(DESTDIR)$(SYSCONFDIR)/gr_report/gr_report.env
	@if [ -z "$(DESTDIR)" ] && command -v systemctl >/dev/null; then \
		systemd-sysusers /usr/lib/sysusers.d/gr_report.conf && \
		systemd-tmpfiles --create /usr/lib/tmpfiles.d/gr_report.conf && \
		systemctl daemon-reload; \
	fi
	@echo "next: create a certificate (docs/operations.md), then: systemctl enable --now gr_report"

.PHONY: install-apparmor
install-apparmor: ## Install and load the AppArmor profile
	install -D -m 0644 deploy/apparmor/usr.local.bin.gr_report /etc/apparmor.d/usr.local.bin.gr_report
	apparmor_parser -r /etc/apparmor.d/usr.local.bin.gr_report

.PHONY: install-selinux
install-selinux: ## Build and load the SELinux module, label files (needs selinux-policy-devel)
	$(MAKE) -C deploy/selinux -f /usr/share/selinux/devel/Makefile gr_report.pp
	semodule -i deploy/selinux/gr_report.pp
	restorecon -R $(PREFIX)/bin/$(BIN) $(SYSCONFDIR)/gr_report /var/lib/gr_report 2>/dev/null || true

.PHONY: uninstall
uninstall: ## Remove binary and unit; keeps /etc/gr_report and /var/lib/gr_report
	-systemctl disable --now gr_report 2>/dev/null
	rm -f $(DESTDIR)$(PREFIX)/bin/$(BIN) $(DESTDIR)$(UNITDIR)/gr_report.service
	rm -f $(DESTDIR)/usr/lib/sysusers.d/gr_report.conf $(DESTDIR)/usr/lib/tmpfiles.d/gr_report.conf

.PHONY: clean
clean: ## Remove build and test artefacts
	rm -rf bin dist coverage.out coverage.html
	rm -f deploy/selinux/gr_report.pp
	rm -rf deploy/selinux/tmp
