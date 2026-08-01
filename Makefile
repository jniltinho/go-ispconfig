## Variables for UPX
UPX_VERSION      := 5.1.1
UPX_ARCHIVE      := upx-$(UPX_VERSION)-amd64_linux.tar.xz
UPX_DIR          := upx-$(UPX_VERSION)-amd64_linux
UPX_BIN          := /usr/local/bin/upx
UPX_URL          := https://github.com/upx/upx/releases/download/v$(UPX_VERSION)/$(UPX_ARCHIVE)

## Variables for Go application
APP        := go-ispconfig
BIN        := bin/$(APP)
PREFIX     := go-ispconfig/cmd
VERSION    := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS    := -trimpath -ldflags "-s -w -X $(PREFIX).Version=$(VERSION) -X $(PREFIX).BuildDate=$(BUILD_TIME) -X $(PREFIX).GitCommit=$(GIT_COMMIT)"

.PHONY: all build build-prod run clean frontend frontend-dev \
        migrate tidy deps deps-frontend install-upx lint swagger \
        test test-race help

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
	@if [ -f frontend/package.json ]; then rm -rf web/dist; fi

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

## Generate Swagger documentation (wired by the REST API core tasks)
swagger:
	@echo "swagger: not wired yet (REST API core, task 6.6)"

install-upx:
	@echo "Installing UPX $(UPX_VERSION)..."
	curl -ksSL "$(UPX_URL)" -o "$(UPX_ARCHIVE)"
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
