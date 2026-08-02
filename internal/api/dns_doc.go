package api

// The DNS CRUD routes are registered generically by RegisterEntity; the
// functions below only carry the per-route swaggo annotations for the
// zones, slave-zones and zone-templates entities.
var _ = []any{
	dnsZoneListDoc, dnsZoneGetDoc, dnsZoneCreateDoc, dnsZoneUpdateDoc, dnsZoneDeleteDoc,
	dnsSlaveListDoc, dnsSlaveGetDoc, dnsSlaveCreateDoc, dnsSlaveUpdateDoc, dnsSlaveDeleteDoc,
	dnsTemplateListDoc, dnsTemplateGetDoc, dnsTemplateCreateDoc, dnsTemplateUpdateDoc,
	dnsTemplateDeleteDoc,
}

// dnsZoneListDoc documents GET /api/dns/zones.
//
//	@Summary		List DNS zones
//	@Description	Paginated, permission-scoped list of dns_soa zones (port of dns_zone_get_by_user). Any declared field name may be passed as a query parameter for substring filtering (e.g. ?origin=example). Items carry _datalog_state ("pending"/"error") and _datalog_error while a change is being applied by the daemon or failed there.
//	@Tags			dns
//	@Produce		json
//	@Param			page	query		int		false	"1-based page number"	default(1)
//	@Param			limit	query		int		false	"Page size (max 100)"	default(25)
//	@Param			origin	query		string	false	"Substring filter on origin"
//	@Success		200		{object}	ListResponse
//	@Failure		401		{object}	ErrorResponse
//	@Router			/dns/zones [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func dnsZoneListDoc() {}

// dnsZoneGetDoc documents GET /api/dns/zones/{id}.
//
//	@Summary		Get a DNS zone
//	@Description	Returns the dns_soa record (port of dns_zone_get) including the daemon-written rendered_zone and dnssec_info columns, plus _datalog_state/_datalog_error while a change is pending or failed.
//	@Tags			dns
//	@Produce		json
//	@Param			id	path		int	true	"zone id"
//	@Success		200	{object}	model.DNSSoa
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Permission denied or record not found"
//	@Router			/dns/zones/{id} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func dnsZoneGetDoc() {}

// dnsZoneCreateDoc documents POST /api/dns/zones.
//
//	@Summary		Create a DNS zone
//	@Description	Port of dns_zone_add: validates the dns_soa.tform rules (origin FQDN + unique + IDN/lowercase, ns/mbox formats, refresh/retry/expire/minimum/ttl >= 60, xfer/also_notify IP lists, DNS-capable server), sets the initial serial to <today>01, stamps ownership server-side and journals the insert to sys_datalog; the daemon then writes the zone. update_acl is accepted from admins only.
//	@Tags			dns
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.DNSSoa	true	"Field values (declared fields only; sys_ columns are ignored)"
//	@Success		201		{object}	model.DNSSoa
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/dns/zones [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func dnsZoneCreateDoc() {}

// dnsZoneUpdateDoc documents PUT /api/dns/zones/{id}.
//
//	@Summary		Update a DNS zone
//	@Description	Port of dns_zone_update: merges the declared body fields over the stored record, validates, bumps the SOA serial when any field changed (skippable with "update_serial": false in the body) and saves under the u-flag permission scope with a sys_datalog diff.
//	@Tags			dns
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int				true	"zone id"
//	@Param			record	body		model.DNSSoa	true	"Changed field values"
//	@Success		200		{object}	model.DNSSoa
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/dns/zones/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func dnsZoneUpdateDoc() {}

// dnsZoneDeleteDoc documents DELETE /api/dns/zones/{id}.
//
//	@Summary		Delete a DNS zone
//	@Description	Port of dns_zone_delete: removes the dns_soa row under the d-flag scope and journals the delete; the daemon then removes the zone file and its named.conf.local entry.
//	@Tags			dns
//	@Param			id	path	int	true	"zone id"
//	@Success		204	"Deleted"
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/dns/zones/{id} [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func dnsZoneDeleteDoc() {}

// dnsSlaveListDoc documents GET /api/dns/slave-zones.
//
//	@Summary		List secondary DNS zones
//	@Description	Paginated, permission-scoped list of dns_slave zones with _datalog_state indicators.
//	@Tags			dns
//	@Produce		json
//	@Param			page	query		int		false	"1-based page number"	default(1)
//	@Param			limit	query		int		false	"Page size (max 100)"	default(25)
//	@Param			origin	query		string	false	"Substring filter on origin"
//	@Success		200		{object}	ListResponse
//	@Failure		401		{object}	ErrorResponse
//	@Router			/dns/slave-zones [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func dnsSlaveListDoc() {}

// dnsSlaveGetDoc documents GET /api/dns/slave-zones/{id}.
//
//	@Summary	Get a secondary DNS zone
//	@Tags		dns
//	@Produce	json
//	@Param		id	path		int	true	"slave zone id"
//	@Success	200	{object}	model.DNSSlave
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse	"Permission denied or record not found"
//	@Router		/dns/slave-zones/{id} [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func dnsSlaveGetDoc() {}

// dnsSlaveCreateDoc documents POST /api/dns/slave-zones.
//
//	@Summary		Create a secondary DNS zone
//	@Description	Port of dns_slave_add: origin validated like a zone, ns is the required master IP list, xfer an optional IP list; the insert is journaled so the daemon configures the slave zone.
//	@Tags			dns
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.DNSSlave	true	"Field values"
//	@Success		201		{object}	model.DNSSlave
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/dns/slave-zones [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func dnsSlaveCreateDoc() {}

// dnsSlaveUpdateDoc documents PUT /api/dns/slave-zones/{id}.
//
//	@Summary	Update a secondary DNS zone
//	@Tags		dns
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int				true	"slave zone id"
//	@Param		record	body		model.DNSSlave	true	"Changed field values"
//	@Success	200		{object}	model.DNSSlave
//	@Failure	401		{object}	ErrorResponse
//	@Failure	403		{object}	ErrorResponse
//	@Failure	422		{object}	ErrorResponse
//	@Router		/dns/slave-zones/{id} [put]
//	@Security	CookieAuth
//	@Security	BearerAuth
func dnsSlaveUpdateDoc() {}

// dnsSlaveDeleteDoc documents DELETE /api/dns/slave-zones/{id}.
//
//	@Summary	Delete a secondary DNS zone
//	@Tags		dns
//	@Param		id	path	int	true	"slave zone id"
//	@Success	204	"Deleted"
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse
//	@Router		/dns/slave-zones/{id} [delete]
//	@Security	CookieAuth
//	@Security	BearerAuth
func dnsSlaveDeleteDoc() {}

// dnsTemplateListDoc documents GET /api/dns/zone-templates.
//
//	@Summary		List zone templates (admin)
//	@Description	Full dns_template CRUD list for administrators. Non-admins use GET /dns/templates for the visible wizard templates.
//	@Tags			dns
//	@Produce		json
//	@Param			page	query		int	false	"1-based page number"	default(1)
//	@Param			limit	query		int	false	"Page size (max 100)"	default(25)
//	@Success		200		{object}	ListResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Not an admin"
//	@Router			/dns/zone-templates [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func dnsTemplateListDoc() {}

// dnsTemplateGetDoc documents GET /api/dns/zone-templates/{id}.
//
//	@Summary	Get a zone template (admin)
//	@Tags		dns
//	@Produce	json
//	@Param		id	path		int	true	"template id"
//	@Success	200	{object}	model.DNSTemplate
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse	"Not an admin"
//	@Router		/dns/zone-templates/{id} [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func dnsTemplateGetDoc() {}

// dnsTemplateCreateDoc documents POST /api/dns/zone-templates.
//
//	@Summary		Create a zone template (admin)
//	@Description	name required; fields is a CSV subset of DOMAIN,IP,IPV6,NS1,NS2,EMAIL,DKIM,DNSSEC; template holds the [ZONE]/[DNS_RECORDS] definition.
//	@Tags			dns
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.DNSTemplate	true	"Field values"
//	@Success		201		{object}	model.DNSTemplate
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Not an admin"
//	@Failure		422		{object}	ErrorResponse
//	@Router			/dns/zone-templates [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func dnsTemplateCreateDoc() {}

// dnsTemplateUpdateDoc documents PUT /api/dns/zone-templates/{id}.
//
//	@Summary	Update a zone template (admin)
//	@Tags		dns
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int					true	"template id"
//	@Param		record	body		model.DNSTemplate	true	"Changed field values"
//	@Success	200		{object}	model.DNSTemplate
//	@Failure	401		{object}	ErrorResponse
//	@Failure	403		{object}	ErrorResponse	"Not an admin"
//	@Failure	422		{object}	ErrorResponse
//	@Router		/dns/zone-templates/{id} [put]
//	@Security	CookieAuth
//	@Security	BearerAuth
func dnsTemplateUpdateDoc() {}

// dnsTemplateDeleteDoc documents DELETE /api/dns/zone-templates/{id}.
//
//	@Summary	Delete a zone template (admin)
//	@Tags		dns
//	@Param		id	path	int	true	"template id"
//	@Success	204	"Deleted"
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse	"Not an admin"
//	@Router		/dns/zone-templates/{id} [delete]
//	@Security	CookieAuth
//	@Security	BearerAuth
func dnsTemplateDeleteDoc() {}
