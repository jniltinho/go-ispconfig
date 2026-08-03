## Variables for UPX
UPX_VERSION      := 5.1.1
UPX_ARCHIVE      := upx-$(UPX_VERSION)-amd64_linux.tar.xz
UPX_DIR          := upx-$(UPX_VERSION)-amd64_linux
UPX_BIN          := /usr/local/bin/upx
UPX_URL          := https://github.com/upx/upx/releases/download/v$(UPX_VERSION)/$(UPX_ARCHIVE)
## SHA256 of $(UPX_ARCHIVE) — update together with UPX_VERSION
UPX_SHA256       := 1ff660454227861e00772f743f66b900072116b9dc24f6ee28b97cce88a7828a

## Variables for Go application
APP        := go-ispconfig
BIN        := bin/$(APP)
PREFIX     := go-ispconfig/cmd
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS    := -trimpath -ldflags "-s -w -X $(PREFIX).Version=$(VERSION) -X $(PREFIX).BuildDate=$(BUILD_TIME) -X $(PREFIX).GitCommit=$(GIT_COMMIT)"

## Package versions: strip the leading "v" (deb), and "-" is illegal in an
## RPM version field, so untagged builds (v0.1.0-3-gabc) become 0.1.0_3_gabc.
DEB_VERSION := $(shell echo $(VERSION) | sed 's/^v//')
RPM_VERSION := $(shell echo $(DEB_VERSION) | tr '-' '_')

## Vagrant test rig (vagrant/): VM=ubuntu (default) or debian
VM        ?= ubuntu
PANEL_IP  := $(if $(filter debian,$(VM)),192.168.56.11,192.168.56.10)
LINUX_BIN := bin/go-ispconfig-linux-amd64

.PHONY: all build build-prod build-linux run clean frontend frontend-dev \
        migrate tidy deps deps-frontend install-upx lint swagger e2e-theme e2e-clients e2e-mail e2e-firewall e2e-database e2e-cron \
        e2e-ftp-shell e2e-ui-qa \
        swagger-check test test-race help deb rpm \
        vagrant-up vagrant-test vagrant-destroy vagrant-lab-up vagrant-lab-fixtures \
        vagrant-lab-status vagrant-parity-test

## Default: build frontend + go binary
all: clean frontend build

## Build frontend (Vue 3 + Vite) to web/dist/
## Until frontend/ is scaffolded (panel skeleton tasks), the committed
## web/dist/index.html placeholder keeps the go:embed compiling.
frontend:
	@if [ -f frontend/package.json ]; then \
		echo "Building Vue 3 frontend..."; \
		cd frontend && npm run build; \
	else \
		echo "frontend/ not scaffolded yet; using committed web/dist placeholder"; \
	fi

## Run Vite dev server (proxy to :8080)
frontend-dev:
	@if [ ! -f frontend/package.json ]; then \
		echo "frontend/ not scaffolded yet"; exit 1; \
	fi
	@echo "Starting Vite dev server on :5173 (proxy → :8080)..."
	cd frontend && npm run dev

## Build Go binary (requires web/dist/ to exist)
build:
	@echo "Building Go application..."
	CGO_ENABLED=0 go build -o $(BIN) $(LDFLAGS) .

## Full production build: frontend + Go + UPX compression
build-prod: clean frontend
	@echo "Building Go application (production)..."
	CGO_ENABLED=0 go build -o $(BIN) $(LDFLAGS) .
	upx --best --lzma $(BIN)

## Run the unit test suite.
## Scoped to ./internal/... because the root package embeds web/dist.
test:
	@echo "Running tests..."
	go test ./internal/...

## Same, with the race detector.
test-race:
	go test -race ./internal/...

## Themed-panel E2E (agent-browser) against a running built binary:
##   make e2e-theme PANEL_URL=http://127.0.0.1:8091 ADMIN_PASSWORD=...
e2e-theme:
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) e2e/panel-theme.sh

## Client-module E2E (agent-browser) against a running built binary:
##   make e2e-clients PANEL_URL=http://127.0.0.1:8092 ADMIN_PASSWORD=...
e2e-clients:
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) e2e/panel-clients.sh

## Mail-module E2E (agent-browser) against a running built binary:
##   make e2e-mail PANEL_URL=http://127.0.0.1:8093 ADMIN_PASSWORD=...
e2e-mail:
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) e2e/panel-mail.sh

## Firewall-module E2E (agent-browser) against a running built binary:
##   make e2e-firewall PANEL_URL=http://127.0.0.1:8094 ADMIN_PASSWORD=...
e2e-firewall:
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) e2e/panel-firewall.sh

## Cron-module E2E (agent-browser) against a running built binary:
##   make e2e-cron PANEL_URL=https://127.0.0.1:8096 ADMIN_PASSWORD=...
e2e-cron:
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) CRON_SQL="$(CRON_SQL)" e2e/panel-cron.sh

## Database-module E2E (agent-browser) against a running built binary:
##   make e2e-database PANEL_URL=https://127.0.0.1:8095 ADMIN_PASSWORD=...
e2e-database:
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) e2e/panel-database.sh

## FTP/shell-users E2E (agent-browser) against a running built binary:
##   make e2e-ftp-shell PANEL_URL=http://127.0.0.1:8097 ADMIN_PASSWORD=...
e2e-ftp-shell:
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) e2e/panel-ftp-shell.sh

## DNS-module E2E (agent-browser) against a running built binary whose local
## server row is provisioned with [dns] dns_backend=powerdns:
##   make e2e-dns-powerdns PANEL_URL=http://127.0.0.1:8098 ADMIN_PASSWORD=...
## Self-contained bootstrap (docker MariaDB + powerdns schema + build +
## serve, no running panel required): scripts/e2e-dns-powerdns.sh
e2e-dns-powerdns:
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) e2e/panel-dns-powerdns.sh

## Unified UI QA E2E: baseline smoke + every module suite + theme, in order,
## against ONE running panel. Precondition: freshly migrated DB with the
## seeded admin plus one DNS zone id=1 (panel-theme.sh requirement).
##   make e2e-ui-qa PANEL_URL=http://127.0.0.1:8096 ADMIN_PASSWORD=... [CRON_SQL=...]
e2e-ui-qa:
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) e2e/panel-ui-qa-baseline.sh
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) CRON_SQL="$(CRON_SQL)" e2e/panel-cron.sh
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) e2e/panel-ftp-shell.sh
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) e2e/panel-database.sh
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) e2e/panel-mail.sh
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) e2e/panel-clients.sh
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) e2e/panel-firewall.sh
	PANEL_URL=$(PANEL_URL) ADMIN_PASSWORD=$(ADMIN_PASSWORD) e2e/panel-theme.sh
	@echo "e2e-ui-qa: all suites green"

## Run golangci-lint (godoc on exported identifiers is enforced)
lint:
	golangci-lint run ./...

## Run the application binary
run:
	@echo "Starting application..."
	./$(BIN) serve

## Database migration
migrate:
	@echo "Running database migrations..."
	./$(BIN) migrate

## Clean build artifacts (keeps the committed web/dist placeholder while
## frontend/ does not exist yet)
clean:
	@echo "Cleaning up..."
	rm -f $(BIN)
	@if [ -f frontend/package.json ]; then \
		rm -rf web/dist; \
		git checkout -- web/dist/index.html; \
	fi

## Go module tidy
tidy:
	@echo "Tidying go modules..."
	go mod tidy

## Install Go dependencies
deps:
	@echo "Installing Go dependencies..."
	go mod download

## Install Node dependencies for frontend
deps-frontend:
	@echo "Installing frontend dependencies..."
	cd frontend && npm install

## Generate Swagger documentation (swaggo annotations → internal/api/docs)
swagger:
	swag fmt --dir internal/api
	swag init --dir internal/api --generalInfo api.go --output internal/api/docs \
		--outputTypes go,json --parseDependency

## Fail when the committed swagger spec is stale (CI staleness check)
swagger-check: swagger
	@git diff --exit-code internal/api/docs internal/api \
		|| { echo "swagger docs are stale: run 'make swagger' and commit"; exit 1; }

## Cross-build the linux/amd64 binary consumed by the Vagrant rig
## (frontend first, so the embedded SPA is the real panel, not the placeholder)
build-linux: frontend
	@echo "Building linux/amd64 binary for the Vagrant rig..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(LINUX_BIN) $(LDFLAGS) .

## Bring the test VM up (VM=ubuntu|debian), building the binary first
vagrant-up: build-linux
	cd vagrant && vagrant up $(VM)

## Run the smoke test inside the guest; exit code = test result
vagrant-test:
	cd vagrant && vagrant up $(VM) --no-provision \
		&& vagrant upload smoke-test.sh /tmp/smoke-test.sh $(VM) \
		&& vagrant ssh $(VM) -c "sudo PANEL_IP=$(PANEL_IP) bash /tmp/smoke-test.sh"

## Destroy the installer rig VMs (standing lab VMs legacy/legacy-apache are kept)
vagrant-destroy:
	cd vagrant && vagrant destroy -f ubuntu debian

## Bring up the standing lab: go-ispconfig (.10) + legacy nginx (.20) + legacy apache2 (.21)
vagrant-lab-up: build-linux
	cd vagrant && vagrant up ubuntu legacy legacy-apache

## Idempotent fixture dataset on both legacy lab VMs (see vagrant/lab/)
vagrant-lab-fixtures:
	bash vagrant/lab/fixtures.sh legacy
	bash vagrant/lab/fixtures.sh legacy-apache
	cd vagrant && vagrant upload lab/wordpress.sh /tmp/wordpress.sh legacy \
		&& vagrant ssh legacy -c "sudo bash /tmp/wordpress.sh n"
	cd vagrant && vagrant upload lab/wordpress.sh /tmp/wordpress.sh legacy-apache \
		&& vagrant ssh legacy-apache -c "sudo bash /tmp/wordpress.sh a"

## IP/URL/health table for every lab VM
vagrant-lab-status:
	@printf "%-14s %-15s %-30s %s\n" "MACHINE" "IP" "PANEL" "HEALTH"
	@for m in "ubuntu 192.168.56.10" "debian 192.168.56.11" \
	          "legacy 192.168.56.20" "legacy-apache 192.168.56.21"; do \
		set -- $$m; name=$$1; ip=$$2; \
		code=$$(curl -sk -o /dev/null -m 3 -w '%{http_code}' https://$$ip:8080/ 2>/dev/null); \
		[ "$$code" = 200 ] || [ "$$code" = 302 ] && health="panel up ($$code)" || health="unreachable"; \
		printf "%-14s %-15s %-30s %s\n" "$$name" "$$ip" "https://$$ip:8080" "$$health"; \
	done

## Run the agent-browser parity suite against both panels (see vagrant/parity/)
vagrant-parity-test:
	bash vagrant/parity/parity-test.sh

## Debian package. Layout matches what the units expect (ExecStart
## /usr/local/bin/go-ispconfig, --config /etc/go-ispconfig/config.toml) and
## what `go-ispconfig install` writes, so the deb and the installer agree.
## The example config is shipped, not config.toml: the real one is generated
## by `go-ispconfig install` with the DB credentials.
deb: build-prod
	@echo "Building Debian package..."
	rm -rf build/deb
	mkdir -p build/deb/usr/local/bin
	mkdir -p build/deb/etc/go-ispconfig
	mkdir -p build/deb/etc/systemd/system
	mkdir -p build/deb/usr/share/doc/go-ispconfig
	mkdir -p build/deb/DEBIAN
	cp $(BIN) build/deb/usr/local/bin/go-ispconfig
	chmod 755 build/deb/usr/local/bin/go-ispconfig
	cp config.toml.example build/deb/etc/go-ispconfig/config.toml.example
	chmod 644 build/deb/etc/go-ispconfig/config.toml.example
	cp init/systemd/go-ispconfig-serve.service build/deb/etc/systemd/system/
	cp init/systemd/go-ispconfig-daemon.service build/deb/etc/systemd/system/
	chmod 644 build/deb/etc/systemd/system/go-ispconfig-*.service
	cp docs/install.md build/deb/usr/share/doc/go-ispconfig/
	chmod 644 build/deb/usr/share/doc/go-ispconfig/install.md
	@echo "Package: go-ispconfig" > build/deb/DEBIAN/control
	@echo "Version: $(DEB_VERSION)" >> build/deb/DEBIAN/control
	@echo "Section: web" >> build/deb/DEBIAN/control
	@echo "Priority: optional" >> build/deb/DEBIAN/control
	@echo "Architecture: amd64" >> build/deb/DEBIAN/control
	@echo "Maintainer: jniltinho <jniltinho@gmail.com>" >> build/deb/DEBIAN/control
	@echo "Description: go-ispconfig - ISPConfig3 panel in Go + Vue 3" >> build/deb/DEBIAN/control
	@echo " Self-contained hosting panel for nginx/apache2, Bind/PowerDNS," >> build/deb/DEBIAN/control
	@echo " MariaDB, Dovecot, Rspamd, pure-ftpd, fail2ban and getmail." >> build/deb/DEBIAN/control
	@echo " Run 'go-ispconfig install' after installing this package." >> build/deb/DEBIAN/control
	@printf '%s\n' '#!/bin/sh' 'set -e' \
		'getent group sshusers >/dev/null || groupadd --system sshusers' \
		'id -u go-ispconfig >/dev/null 2>&1 || useradd --system --user-group \' \
		'  --home-dir /etc/go-ispconfig --no-create-home \' \
		'  --shell /usr/sbin/nologin go-ispconfig' \
		'install -d -o go-ispconfig -g go-ispconfig -m 0750 /etc/go-ispconfig/ssl' \
		'systemctl daemon-reload >/dev/null 2>&1 || true' \
		> build/deb/DEBIAN/postinst
	chmod 755 build/deb/DEBIAN/postinst
	find build/deb -type d -exec chmod 755 {} +
	dpkg-deb --root-owner-group --build build/deb go-ispconfig_$(DEB_VERSION)_amd64.deb
	rm -rf build/deb
	@echo "Debian package created: go-ispconfig_$(DEB_VERSION)_amd64.deb"

## RPM package. Same layout as the deb.
rpm: build-prod
	@echo "Building RPM package..."
	rm -rf build/rpm
	mkdir -p build/rpm/BUILD build/rpm/RPMS build/rpm/SOURCES build/rpm/SPECS build/rpm/SRPMS
	@echo "Name: go-ispconfig" > build/rpm/SPECS/go-ispconfig.spec
	@echo "Version: $(RPM_VERSION)" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "Release: 1" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "Summary: ISPConfig3 hosting panel in Go + Vue 3" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "License: BSD-3-Clause" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "URL: https://github.com/jniltinho/go-ispconfig" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "%description" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "Self-contained hosting panel for nginx/apache2, Bind/PowerDNS, MariaDB," >> build/rpm/SPECS/go-ispconfig.spec
	@echo "Dovecot, Rspamd, pure-ftpd, fail2ban and getmail." >> build/rpm/SPECS/go-ispconfig.spec
	@echo "Run 'go-ispconfig install' after installing this package." >> build/rpm/SPECS/go-ispconfig.spec
	@echo "%install" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "mkdir -p %{buildroot}/usr/local/bin" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "mkdir -p %{buildroot}/etc/go-ispconfig" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "mkdir -p %{buildroot}/etc/systemd/system" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "mkdir -p %{buildroot}/usr/share/doc/go-ispconfig" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "install -m 755 $(CURDIR)/$(BIN) %{buildroot}/usr/local/bin/go-ispconfig" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "install -m 644 $(CURDIR)/config.toml.example %{buildroot}/etc/go-ispconfig/config.toml.example" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "install -m 644 $(CURDIR)/init/systemd/go-ispconfig-serve.service %{buildroot}/etc/systemd/system/" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "install -m 644 $(CURDIR)/init/systemd/go-ispconfig-daemon.service %{buildroot}/etc/systemd/system/" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "install -m 644 $(CURDIR)/docs/install.md %{buildroot}/usr/share/doc/go-ispconfig/" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "%post" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "getent group sshusers >/dev/null || groupadd --system sshusers" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "id -u go-ispconfig >/dev/null 2>&1 || useradd --system --user-group --home-dir /etc/go-ispconfig --no-create-home --shell /usr/sbin/nologin go-ispconfig" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "install -d -o go-ispconfig -g go-ispconfig -m 0750 /etc/go-ispconfig/ssl" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "systemctl daemon-reload >/dev/null 2>&1 || true" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "%files" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "%defattr(-,root,root,-)" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "/usr/local/bin/go-ispconfig" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "%dir /etc/go-ispconfig" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "%config(noreplace) /etc/go-ispconfig/config.toml.example" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "/etc/systemd/system/go-ispconfig-serve.service" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "/etc/systemd/system/go-ispconfig-daemon.service" >> build/rpm/SPECS/go-ispconfig.spec
	@echo "%doc /usr/share/doc/go-ispconfig/install.md" >> build/rpm/SPECS/go-ispconfig.spec
	rpmbuild -bb --define "_topdir $(CURDIR)/build/rpm" build/rpm/SPECS/go-ispconfig.spec
	find build/rpm/RPMS -name "*.rpm" -exec mv {} . \;
	rm -rf build/rpm
	@echo "RPM package created: go-ispconfig-$(RPM_VERSION)-1.x86_64.rpm"

install-upx:
	@echo "Installing UPX $(UPX_VERSION)..."
	curl -sSL "$(UPX_URL)" -o "$(UPX_ARCHIVE)"
	echo "$(UPX_SHA256)  $(UPX_ARCHIVE)" | sha256sum -c -
	tar -xf "$(UPX_ARCHIVE)"
	chmod +x "$(UPX_DIR)/upx"
	mv "$(UPX_DIR)/upx" "$(UPX_BIN)"
	rm -rf "$(UPX_DIR)" "$(UPX_ARCHIVE)"

help:
	@echo "Makefile commands:"
	@echo "  all              - Clean, build frontend + Go binary"
	@echo "  frontend         - Build Vue 3 SPA to web/dist/"
	@echo "  frontend-dev     - Start Vite dev server (:5173)"
	@echo "  build            - Build Go binary (requires web/dist/)"
	@echo "  build-prod       - Full prod build with UPX compression"
	@echo "  deb              - Build the .deb package (dpkg-deb)"
	@echo "  rpm              - Build the .rpm package (rpmbuild)"
	@echo "  test             - Run unit tests (./internal/...)"
	@echo "  test-race        - Unit tests with race detector"
	@echo "  lint             - Run golangci-lint"
	@echo "  run              - Run the application (serve)"
	@echo "  migrate          - Run database migrations"
	@echo "  clean            - Remove binary and built web/dist"
	@echo "  tidy             - Run go mod tidy"
	@echo "  deps             - Download Go dependencies"
	@echo "  deps-frontend    - Install frontend npm packages"
	@echo "  swagger          - Generate Swagger API documentation"
	@echo "  install-upx      - Download and install UPX binary"
	@echo "  build-linux      - Cross-build linux/amd64 binary (Vagrant rig)"
	@echo "  vagrant-up       - Build binary + vagrant up (VM=ubuntu|debian)"
	@echo "  vagrant-test     - Run smoke test in the guest (VM=ubuntu|debian)"
	@echo "  vagrant-destroy  - Destroy installer rig VMs (keeps standing lab)"
	@echo "  vagrant-lab-up   - Bring up the standing lab (ubuntu + both legacies)"
	@echo "  vagrant-lab-fixtures - Idempotent fixture dataset on both legacy VMs"
	@echo "  vagrant-lab-status - IP/URL/health table for all lab VMs"
	@echo "  vagrant-parity-test - Run the parity suite against both panels"
