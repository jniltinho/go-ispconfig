package api

// Firewall CRUD entity (add-firewall-module task 1.3 / 3.1). The Go
// replacement of interface/web/admin/form/firewall.tform.php: the
// declarative entity drives the API, the form metadata endpoint and the
// Vue form. Defaults, regex validator, server_id UNIQUE / immutability
// and the open-admin security policy live here.

import (
	"context"

	"gorm.io/gorm"

	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// FirewallDefaults are the create-time defaults from firewall.tform.php
// (design D7 / task 1.3): the panel pre-seeds the common service ports
// when the row is first saved so a fresh install does not silently lock
// out HTTP/SMTP/IMAP. The actual TCP allow-list applied by the daemon
// also unions the protected panel + SSH ports (see firewall.EffectivePorts).
const (
	firewallDefaultTCPPort = "21,22,25,53,80,110,143,443,465,587,993,995,3306,4190,8080,8081,40110:40210"
	firewallDefaultUDPPort = "53"
	firewallDefaultActive  = "y"

	// firewallErrorUnique is the i18n key returned when an admin tries
	// to create a second firewall row for the same server (UNIQUE
	// validator, port of firewall.tform.php tcp_port validator style).
	firewallErrorUnique = "firewall_error_unique"
	// firewallErrorServerImm is the i18n key returned when an update
	// body tries to change server_id on an existing row (port of
	// firewall_edit.php::onBeforeUpdate "The Server can not be changed.").
	firewallErrorServerImm = "firewall_error_server_immutable"

	// firewallTCPPortsRegex is the port-list REGEX byte-identical to
	// firewall.tform.php tcp_ports_error_regex / udp_ports_error_regex
	// (see internal/firewall/ports.go NOTE for the Go RE2 vs PHP PCRE
	// acceptance divergence on pathological inputs).
	firewallTCPPortsRegex = `^$|\d{1,5}(?::\d{1,5})?(?:,\d{1,5}(?::\d{1,5})?)*$`
)

// firewallEntity is the /api/firewall CRUD surface (port of
// firewall.tform.php / firewall.list.php).
//
//	AdminOnly  — PHP gates every route by is_admin(); security policy
//	             admin_allow_firewall_config further restricts to
//	             sys_user.id = 1 by default.
//	server_id  — UNIQUE at the API layer (error firewall_error_unique,
//	             ISPConfig's tform UNIQUE validator) and immutable on
//	             update (error firewall_error_server_immutable).
//	tcp_port / udp_port — same port-list REGEX as the original
//	             tcp_ports_error_regex / udp_ports_error_regex.
//	active     — "y"/"n" checkbox.
func firewallEntity() *Entity {
	return &Entity{
		Name:      "firewall",
		Title:     "firewall_edit_title",
		Policy:    "admin_allow_firewall_config",
		AdminOnly: true,
		Tabs: []Tab{{
			Name:  "firewall",
			Label: "firewall_tab_title",
			Fields: []Field{
				{
					Name: "server_id", Label: "server_id_txt",
					Datatype: "INTEGER", Formtype: "SELECT",
					Validators: []validator.Rule{
						{Type: "ISPOSITIVE", ErrKey: "server_id_error_empty"},
						{Type: "UNIQUE", ErrKey: firewallErrorUnique},
					},
				},
				{
					Name: "tcp_port", Label: "tcp_port_txt",
					Datatype: "VARCHAR", Formtype: "TEXT",
					Default: firewallDefaultTCPPort,
					Validators: []validator.Rule{
						{Type: "REGEX", Regex: firewallTCPPortsRegex, ErrKey: "tcp_ports_error_regex"},
					},
				},
				{
					Name: "udp_port", Label: "udp_port_txt",
					Datatype: "VARCHAR", Formtype: "TEXT",
					Default: firewallDefaultUDPPort,
					Validators: []validator.Rule{
						{Type: "REGEX", Regex: firewallTCPPortsRegex, ErrKey: "udp_ports_error_regex"},
					},
				},
				{
					Name: "active", Label: "active_txt",
					Datatype: "VARCHAR", Formtype: "CHECKBOX",
					Default: firewallDefaultActive,
					Options: []Option{
						{Value: "n", Label: "no_txt"},
						{Value: "y", Label: "yes_txt"},
					},
				},
			},
		}},
		// server_id is set once on create and never changes; refuse the
		// update with a 422 if the body tries to move the row to a
		// different server (PHP firewall_edit.php::onBeforeUpdate).
		BeforeUpdate: firewallServerImmutable,
	}
}

// firewallServerImmutable enforces firewall.tform.php
// onBeforeUpdate's "The Server can not be changed." rule: if the body
// carries a server_id, abort the update with a 422 validation error.
func firewallServerImmutable(_ context.Context, _ *gorm.DB, _ *repository.Identity, body map[string]any, _ any, _ any) error {
	if _, ok := body["server_id"]; ok {
		return &ValidationError{Fields: map[string][]string{
			"server_id": {firewallErrorServerImm},
		}}
	}
	return nil
}
