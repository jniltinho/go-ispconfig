package api

// Swaggo annotations for the firewall endpoints. The CRUD routes are
// registered generically by RegisterEntity; these functions only carry
// per-route documentation. Every route is admin-only and further gated by
// the admin_allow_firewall_config security policy (superadmin by default).
var _ = []any{
	firewallListDoc, firewallGetDoc, firewallCreateDoc, firewallUpdateDoc, firewallDeleteDoc,
}

// firewallListDoc documents GET /api/firewall.
//
//	@Summary		List firewall records
//	@Description	Paginated list of per-server UFW firewall rows. Admin-only, gated by admin_allow_firewall_config (superadmin by default). Any declared field may filter (e.g. ?server_id=1).
//	@Tags			firewall
//	@Produce		json
//	@Param			page		query		int	false	"1-based page number"	default(1)
//	@Param			limit		query		int	false	"Page size (max 100)"	default(25)
//	@Param			server_id	query		int	false	"Filter by server_id"
//	@Success		200			{object}	ListResponse
//	@Failure		401			{object}	ErrorResponse
//	@Failure		403			{object}	ErrorResponse	"Not admin or blocked by admin_allow_firewall_config"
//	@Router			/firewall [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func firewallListDoc() {}

// firewallGetDoc documents GET /api/firewall/{id}.
//
//	@Summary	Get a firewall record
//	@Tags		firewall
//	@Produce	json
//	@Param		id	path		int	true	"firewall_id"
//	@Success	200	{object}	model.Firewall
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse
//	@Failure	404	{object}	ErrorResponse
//	@Router		/firewall/{id} [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func firewallGetDoc() {}

// firewallCreateDoc documents POST /api/firewall.
//
//	@Summary		Create a firewall record
//	@Description	One row per server: server_id is UNIQUE (firewall_error_unique). tcp_port/udp_port are comma-separated port lists (single ports or lower:higher ranges); when omitted the tform defaults are applied. The daemon always unions the panel and SSH ports into the applied TCP allow-list so the record can never lock the operator out.
//	@Tags			firewall
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.Firewall	true	"Firewall fields"
//	@Success		201		{object}	model.Firewall
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Not admin or blocked by admin_allow_firewall_config"
//	@Failure		422		{object}	ErrorResponse	"Validation failed (server_id empty/duplicate, malformed port list)"
//	@Router			/firewall [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func firewallCreateDoc() {}

// firewallUpdateDoc documents PUT /api/firewall/{id}.
//
//	@Summary		Update a firewall record
//	@Description	server_id is immutable (firewall_error_server_immutable); a full-object PUT that re-sends the unchanged server_id is accepted, a real change is rejected.
//	@Tags			firewall
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int				true	"firewall_id"
//	@Param			record	body		model.Firewall	true	"Changed fields"
//	@Success		200		{object}	model.Firewall
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Validation failed (server_id changed, malformed port list)"
//	@Router			/firewall/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func firewallUpdateDoc() {}

// firewallDeleteDoc documents DELETE /api/firewall/{id}.
//
//	@Summary		Delete a firewall record
//	@Description	Journals a firewall_delete the daemon consumes to reset and disable UFW on the target server.
//	@Tags			firewall
//	@Param			id	path	int	true	"firewall_id"
//	@Success		204	"No Content"
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Router			/firewall/{id} [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func firewallDeleteDoc() {}
