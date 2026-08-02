package api

// The sites CRUD routes are registered generically by RegisterEntity; the
// functions below only carry the per-route swaggo annotations for the
// web-domains entity.
var _ = []any{
	webDomainListDoc, webDomainGetDoc, webDomainCreateDoc, webDomainUpdateDoc, webDomainDeleteDoc,
}

// webDomainListDoc documents GET /api/sites/web-domains.
//
//	@Summary		List web domains
//	@Description	Paginated, permission-scoped list of vhost/subdomain/alias domains. Any declared field name may be passed as a query parameter for substring filtering (e.g. ?domain=example). Items carry _datalog_state ("pending"/"error") and _datalog_error while a change is being applied by the daemon or failed there.
//	@Tags			sites
//	@Produce		json
//	@Param			page	query		int		false	"1-based page number"	default(1)
//	@Param			limit	query		int		false	"Page size (max 100)"	default(25)
//	@Param			domain	query		string	false	"Substring filter on domain"
//	@Success		200		{object}	ListResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Router			/sites/web-domains [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func webDomainListDoc() {}

// webDomainGetDoc documents GET /api/sites/web-domains/{id}.
//
//	@Summary		Get a web domain
//	@Description	Returns the record plus _datalog_state/_datalog_error when the daemon has not yet applied (or failed to apply) the latest change.
//	@Tags			sites
//	@Produce		json
//	@Param			id	path		int	true	"domain_id"
//	@Success		200	{object}	model.WebDomain
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Permission denied or record not found"
//	@Router			/sites/web-domains/{id} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func webDomainGetDoc() {}

// webDomainCreateDoc documents POST /api/sites/web-domains.
//
//	@Summary		Create a web domain
//	@Description	Validates the tform-ported field rules (domain syntax/uniqueness, quotas, nginx_directives blacklist), derives document_root, system_user and system_group server-side (ISPConfig onAfterInsert semantics) and journals the insert to sys_datalog in the same transaction; the daemon then provisions the site. Admin-only Options fields are ignored for non-admin callers.
//	@Tags			sites
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.WebDomain	true	"Field values (declared fields only; sys_ columns are ignored)"
//	@Success		201		{object}	model.WebDomain
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/sites/web-domains [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func webDomainCreateDoc() {}

// webDomainUpdateDoc documents PUT /api/sites/web-domains/{id}.
//
//	@Summary		Update a web domain
//	@Description	Merges the declared body fields over the stored record, validates the result and saves it under the u-flag permission scope with a sys_datalog diff consumed by the daemon.
//	@Tags			sites
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int				true	"domain_id"
//	@Param			record	body		model.WebDomain	true	"Changed field values"
//	@Success		200		{object}	model.WebDomain
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/sites/web-domains/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func webDomainUpdateDoc() {}

// webDomainDeleteDoc documents DELETE /api/sites/web-domains/{id}.
//
//	@Summary	Delete a web domain
//	@Tags		sites
//	@Param		id	path	int	true	"domain_id"
//	@Success	204	"Deleted (vhost/pool/dirs removed asynchronously by the daemon)"
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse
//	@Router		/sites/web-domains/{id} [delete]
//	@Security	CookieAuth
//	@Security	BearerAuth
func webDomainDeleteDoc() {}
