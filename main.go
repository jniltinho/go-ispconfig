// go-ispconfig is a hosting control panel — a Go port of ISPConfig3 managing
// nginx (web) and Bind (DNS) while keeping the original database schema.
// The Vue 3 SPA is embedded at build time from web/dist and served by the binary.
package main

import (
	"embed"

	// Embed the IANA time zone database. The binary is shipped standalone and
	// cannot rely on /usr/share/zoneinfo being present on stripped-down hosts.
	_ "time/tzdata"

	"go-ispconfig/cmd"
)

//go:embed all:web/dist
//go:embed config.toml.example
var embeddedFiles embed.FS

// main is the binary entry point; it delegates all CLI parsing to the cmd package.
func main() {
	cmd.Execute(embeddedFiles)
}
