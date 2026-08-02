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

## Vagrant test rig (vagrant/): VM=ubuntu (default) or debian
VM        ?= ubuntu
PANEL_IP  := $(if $(filter debian,$(VM)),192.168.56.11,192.168.56.10)
LINUX_BIN := bin/go-ispconfig-linux-amd64

.PHONY: all build build-prod build-linux run clean frontend frontend-dev \
        migrate tidy deps deps-frontend install-upx lint swagger e2e-theme e2e-clients e2e-mail \
        swagger-check test test-race help \
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
