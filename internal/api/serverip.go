package api

import (
	"net/netip"

	"github.com/labstack/echo/v5"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/validator"
)

// serverIPEntity is the declarative definition of the server_ip entity
// (port of interface/web/admin/form/server_ip.tform.php): the first real
// entity of the CRUD framework, admin-gated by the admin_allow_server_ip
// security policy.
func serverIPEntity() *Entity {
	return &Entity{
		Name:   "server_ip",
		Title:  "server_ip_edit_title",
		Policy: "admin_allow_server_ip",
		Tabs: []Tab{{
			Name:  "server_ip",
			Label: "server_ip_tab_title",
			Fields: []Field{
				{
					Name: "server_id", Label: "server_id_txt",
					Datatype: "INTEGER", Formtype: "SELECT",
					Validators: []validator.Rule{
						{Type: "ISPOSITIVE", ErrKey: "server_id_error_empty"},
					},
				},
				{
					Name: "client_id", Label: "client_id_txt",
					Datatype: "INTEGER", Formtype: "SELECT",
				},
				{
					Name: "ip_type", Label: "ip_type_txt",
					Datatype: "VARCHAR", Formtype: "SELECT",
					Default: "IPv4",
					Options: []Option{{Value: "IPv4", Label: "IPv4"}, {Value: "IPv6", Label: "IPv6"}},
				},
				{
					Name: "ip_address", Label: "ip_address_txt",
					Datatype: "VARCHAR", Formtype: "TEXT",
					Validators: []validator.Rule{
						{Type: "CUSTOM", ErrKey: "ip_error_wrong", Fn: checkServerIP},
						{Type: "UNIQUE", ErrKey: "ip_error_unique"},
					},
				},
				{
					Name: "virtualhost", Label: "virtualhost_txt",
					Datatype: "VARCHAR", Formtype: "CHECKBOX",
					Default: "y",
					Options: []Option{{Value: "n", Label: "no_txt"}, {Value: "y", Label: "yes_txt"}},
				},
				{
					Name: "virtualhost_port", Label: "virtualhost_port_txt",
					Datatype: "VARCHAR", Formtype: "TEXT",
					Default: "80,443",
					Validators: []validator.Rule{
						{Type: "REGEX", Regex: `^([0-9]{1,5},?){1,}$`, ErrKey: "error_port_syntax"},
					},
				},
			},
		}},
	}
}

// checkServerIP is the CUSTOM ip_address validator (port of
// validate_server::check_server_ip): the value must be a valid IPv4 or
// IPv6 address.
func checkServerIP(_ *validator.Context, value string) string {
	if _, err := netip.ParseAddr(value); err != nil {
		return "ip_error_wrong"
	}
	return ""
}

// registerEntities mounts every CRUD entity on the authenticated group.
func registerEntities(g *echo.Group, d *Deps) error {
	if err := RegisterEntity[model.ServerIP](g, d, serverIPEntity()); err != nil {
		return err
	}
	if err := registerSitesEntities(g.Group("/sites"), d); err != nil {
		return err
	}
	if err := registerClientEntities(g, d); err != nil {
		return err
	}
	if err := registerMailEntities(g, d); err != nil {
		return err
	}
	if err := RegisterEntity[model.Firewall](g, d, firewallEntity()); err != nil {
		return err
	}
	return registerDNSEntities(g.Group("/dns"), d)
}

// The CRUD routes are registered generically by RegisterEntity; the
// functions below only carry the per-route swaggo annotations for the
// server_ip entity.
var _ = []any{serverIPListDoc, serverIPGetDoc, serverIPCreateDoc, serverIPUpdateDoc, serverIPDeleteDoc}

// serverIPListDoc documents GET /api/server_ip.
//
//	@Summary		List server IP addresses
//	@Description	Paginated, permission-scoped list. Any declared field name may be passed as a query parameter for substring filtering (e.g. ?ip_address=10.0).
//	@Tags			server_ip
//	@Produce		json
//	@Param			page		query		int		false	"1-based page number"	default(1)
//	@Param			limit		query		int		false	"Page size (max 100)"	default(25)
//	@Param			ip_address	query		string	false	"Substring filter on ip_address"
//	@Success		200			{object}	ListResponse
//	@Failure		401			{object}	ErrorResponse
//	@Failure		403			{object}	ErrorResponse
//	@Router			/server_ip [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func serverIPListDoc() {}

// serverIPGetDoc documents GET /api/server_ip/{id}.
//
//	@Summary	Get a server IP address
//	@Tags		server_ip
//	@Produce	json
//	@Param		id	path		int	true	"server_ip_id"
//	@Success	200	{object}	model.ServerIP
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse	"Permission denied or record not found"
//	@Router		/server_ip/{id} [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func serverIPGetDoc() {}

// serverIPCreateDoc documents POST /api/server_ip.
//
//	@Summary		Create a server IP address
//	@Description	Validates the declared fields (ip_address must be a unique valid IP), runs the client limit hook, stamps ownership server-side and journals the insert to sys_datalog.
//	@Tags			server_ip
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.ServerIP	true	"Field values (declared fields only; sys_ columns are ignored)"
//	@Success		201		{object}	model.ServerIP
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/server_ip [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func serverIPCreateDoc() {}

// serverIPUpdateDoc documents PUT /api/server_ip/{id}.
//
//	@Summary		Update a server IP address
//	@Description	Merges the declared body fields over the stored record, validates the result and saves it under the u-flag permission scope with a sys_datalog diff.
//	@Tags			server_ip
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int				true	"server_ip_id"
//	@Param			record	body		model.ServerIP	true	"Changed field values"
//	@Success		200		{object}	model.ServerIP
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/server_ip/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func serverIPUpdateDoc() {}

// serverIPDeleteDoc documents DELETE /api/server_ip/{id}.
//
//	@Summary	Delete a server IP address
//	@Tags		server_ip
//	@Param		id	path	int	true	"server_ip_id"
//	@Success	204	"Deleted"
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse
//	@Router		/server_ip/{id} [delete]
//	@Security	CookieAuth
//	@Security	BearerAuth
func serverIPDeleteDoc() {}
