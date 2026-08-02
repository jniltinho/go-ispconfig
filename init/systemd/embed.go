// Package systemd embeds the shipped unit files so the installer writes
// exactly what the repo tracks (single source, no drift).
package systemd

import _ "embed"

// ServeUnit is the go-ispconfig-serve.service unit (panel, User=go-ispconfig).
//
//go:embed go-ispconfig-serve.service
var ServeUnit string

// DaemonUnit is the go-ispconfig-daemon.service unit (config-apply daemon, root).
//
//go:embed go-ispconfig-daemon.service
var DaemonUnit string
