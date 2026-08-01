# core-cli

## ADDED Requirements

### Requirement: Single binary with Cobra subcommands
The application SHALL build as a single static binary (`CGO_ENABLED=0`) exposing Cobra subcommands: `serve`, `daemon`, `migrate`, `init`, `version`.

#### Scenario: Version output
- **WHEN** the user runs `go-ispconfig version`
- **THEN** the binary prints version, build date and git commit injected via ldflags

#### Scenario: Serve starts panel and API
- **WHEN** the user runs `go-ispconfig serve` with a valid config.toml
- **THEN** Echo listens on the configured address serving both `/api/*` and the embedded SPA

### Requirement: Configuration via config.toml with env override
The binary SHALL load configuration from `config.toml` searched at `--config` flag path, `./config.toml`, then `/etc/go-ispconfig/config.toml`, with environment overrides using prefix `GOISP_` (e.g. `GOISP_SERVER_PORT`).

#### Scenario: Env var overrides file
- **WHEN** config.toml sets `server.port = 8080` and `GOISP_SERVER_PORT=9000` is exported
- **THEN** the server listens on port 9000

#### Scenario: Init generates default config
- **WHEN** the user runs `go-ispconfig init`
- **THEN** a commented default `config.toml` is written and the command refuses to overwrite an existing file

### Requirement: Embedded frontend
The binary SHALL embed the built SPA (`web/dist`) and static assets via `embed.FS`; no runtime filesystem dependency for UI assets, fonts included.

#### Scenario: SPA served from binary
- **WHEN** the binary is copied alone to a clean host and started
- **THEN** the panel loads fully (HTML, JS, CSS, fonts) without network access to CDNs
