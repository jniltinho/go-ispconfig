package api

// Swaggo annotations for the mail-domain endpoints. The CRUD routes are
// registered generically by RegisterEntity; these functions only carry
// per-route documentation.
var _ = []any{
	mailDomainListDoc, mailDomainGetDoc, mailDomainCreateDoc, mailDomainUpdateDoc,
	mailDomainDeleteDoc, mailDomainByDomainDoc, mailDomainSetStatusDoc, mailDomainGenerateDKIMDoc,
}

// mailDomainListDoc documents GET /api/mail/domains.
//
//	@Summary		List mail domains
//	@Description	Paginated, permission-scoped list. dkim_private is never returned. Any declared field may filter (e.g. ?domain=example).
//	@Tags			mail-domains
//	@Produce		json
//	@Param			page	query		int		false	"1-based page number"	default(1)
//	@Param			limit	query		int		false	"Page size (max 100)"	default(25)
//	@Param			domain	query		string	false	"Substring filter on domain"
//	@Success		200		{object}	ListResponse
//	@Failure		401		{object}	ErrorResponse
//	@Router			/mail/domains [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailDomainListDoc() {}

// mailDomainGetDoc documents GET /api/mail/domains/{id}.
//
//	@Summary		Get a mail domain
//	@Description	dkim_private is redacted.
//	@Tags			mail-domains
//	@Produce		json
//	@Param			id	path		int	true	"domain_id"
//	@Success		200	{object}	model.MailDomain
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/mail/domains/{id} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailDomainGetDoc() {}

// mailDomainCreateDoc documents POST /api/mail/domains.
//
//	@Summary		Create a mail domain
//	@Description	Domain is lowercased and IDN-encoded; a mail domain may not collide with a transport domain on the same server. When dkim=y without a key, an RSA pair is generated; a supplied dkim_private is validated. When active+dkim and a managed DNS zone exists, the DKIM TXT is published and the response's dns_published reflects it; otherwise the suggested record is available for manual publication.
//	@Tags			mail-domains
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.MailDomain	true	"Mail domain fields"
//	@Success		201		{object}	model.MailDomain
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Permission denied or limit reached"
//	@Failure		422		{object}	ErrorResponse	"Validation failed (domain empty/malformed, transport collision, bad DKIM key/selector)"
//	@Router			/mail/domains [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailDomainCreateDoc() {}

// mailDomainUpdateDoc documents PUT /api/mail/domains/{id}.
//
//	@Summary		Update a mail domain
//	@Description	Re-reconciles the DKIM TXT record (rename/selector change withdraws the old one; disable/deactivate withdraws).
//	@Tags			mail-domains
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int					true	"domain_id"
//	@Param			record	body		model.MailDomain	true	"Changed fields"
//	@Success		200		{object}	model.MailDomain
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse
//	@Router			/mail/domains/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailDomainUpdateDoc() {}

// mailDomainDeleteDoc documents DELETE /api/mail/domains/{id}.
//
//	@Summary		Delete a mail domain
//	@Description	Journals the delete (the daemon removes the maildomain tree) and withdraws the DKIM TXT.
//	@Tags			mail-domains
//	@Param			id	path	int	true	"domain_id"
//	@Success		204
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/mail/domains/{id} [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailDomainDeleteDoc() {}

// mailDomainByDomainDoc documents GET /api/mail/domains/by-domain/{domain}.
//
//	@Summary		Get a mail domain by name
//	@Description	Permission-scoped lookup (remote mail_domain_get_by_domain); 404 when no accessible domain matches. dkim_private redacted.
//	@Tags			mail-domains
//	@Produce		json
//	@Param			domain	path		string	true	"Domain name"
//	@Success		200		{object}	model.MailDomain
//	@Failure		401		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Router			/mail/domains/by-domain/{domain} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailDomainByDomainDoc() {}

// mailDomainSetStatusDoc documents POST /api/mail/domains/{id}/set-status.
//
//	@Summary		Set a mail domain active/inactive
//	@Description	remote mail_domain_set_status: flips active (y/n), journals the update and reconciles the DKIM TXT.
//	@Tags			mail-domains
//	@Accept			json
//	@Param			id		path	int					true	"domain_id"
//	@Param			record	body	map[string]string	true	"{active: y|n}"
//	@Success		204
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		422	{object}	ErrorResponse
//	@Router			/mail/domains/{id}/set-status [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailDomainSetStatusDoc() {}

// mailDomainGenerateDKIMDoc documents POST /api/mail/domains/generate-dkim.
//
//	@Summary		Generate a DKIM key pair
//	@Description	Returns a fresh 2048-bit RSA private/public PEM pair for the domain form (remote get_dkim create). The key is not stored until the domain is saved.
//	@Tags			mail-domains
//	@Produce		json
//	@Success		200	{object}	map[string]string	"{dkim_private, dkim_public}"
//	@Failure		401	{object}	ErrorResponse
//	@Router			/mail/domains/generate-dkim [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailDomainGenerateDKIMDoc() {}

// Swaggo annotations for the mailbox endpoints.
var _ = []any{
	mailboxListDoc, mailboxGetDoc, mailboxCreateDoc, mailboxUpdateDoc,
	mailboxDeleteDoc, mailboxByClientDoc,
}

// mailboxListDoc documents GET /api/mail/mailboxes.
//
//	@Summary		List mailboxes
//	@Description	Paginated, permission-scoped. The password hash is never returned.
//	@Tags			mailboxes
//	@Produce		json
//	@Param			page	query		int		false	"1-based page number"	default(1)
//	@Param			limit	query		int		false	"Page size (max 100)"	default(25)
//	@Param			email	query		string	false	"Substring filter on email"
//	@Success		200		{object}	ListResponse
//	@Failure		401		{object}	ErrorResponse
//	@Router			/mail/mailboxes [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailboxListDoc() {}

// mailboxGetDoc documents GET /api/mail/mailboxes/{id}.
//
//	@Summary	Get a mailbox
//	@Tags		mailboxes
//	@Produce	json
//	@Param		id	path		int	true	"mailuser_id"
//	@Success	200	{object}	model.MailUser
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse
//	@Router		/mail/mailboxes/{id} [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func mailboxGetDoc() {}

// mailboxCreateDoc documents POST /api/mail/mailboxes.
//
//	@Summary		Create a mailbox
//	@Description	The email domain must exist as a primary mail_domain (not only an aliasdomain). Password is CRYPTMAIL-hashed; maildir/homedir/uid/gid are derived from the domain's server mail config. Subject to the client's limit_mailbox quota.
//	@Tags			mailboxes
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.MailUser	true	"Mailbox fields (password is plaintext, hashed server-side)"
//	@Success		201		{object}	model.MailUser
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Permission denied or limit reached"
//	@Failure		422		{object}	ErrorResponse	"Validation failed (email empty/duplicate, domain missing, ...)"
//	@Router			/mail/mailboxes [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailboxCreateDoc() {}

// mailboxUpdateDoc documents PUT /api/mail/mailboxes/{id}.
//
//	@Summary		Update a mailbox
//	@Description	Empty password leaves the stored hash. Server-derived columns (maildir, homedir, uid, gid, server_id) cannot be set by the client.
//	@Tags			mailboxes
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int				true	"mailuser_id"
//	@Param			record	body		model.MailUser	true	"Changed fields"
//	@Success		200		{object}	model.MailUser
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse
//	@Router			/mail/mailboxes/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailboxUpdateDoc() {}

// mailboxDeleteDoc documents DELETE /api/mail/mailboxes/{id}.
//
//	@Summary		Delete a mailbox
//	@Description	Journals the delete; the daemon removes (or soft-deletes) the maildir.
//	@Tags			mailboxes
//	@Param			id	path	int	true	"mailuser_id"
//	@Success		204
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/mail/mailboxes/{id} [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailboxDeleteDoc() {}

// mailboxByClientDoc documents GET /api/mail/mailboxes/by-client/{client_id}.
//
//	@Summary		List a client's mailboxes
//	@Description	remote mail_user_get_all_by_client: mailboxes owned by the client's group, password-redacted.
//	@Tags			mailboxes
//	@Produce		json
//	@Param			client_id	path		int	true	"client_id"
//	@Success		200			{array}		map[string]any
//	@Failure		401			{object}	ErrorResponse
//	@Router			/mail/mailboxes/by-client/{client_id} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailboxByClientDoc() {}

// Swaggo annotations for the forwarding + transport endpoints. All four
// forwarding surfaces share the mail_forwarding table (type forced
// server-side); one representative doc set is provided per verb per
// surface via generic descriptions.
var _ = []any{
	mailAliasListDoc, mailAliasCreateDoc, mailForwardCreateDoc,
	mailCatchallCreateDoc, mailAliasDomainCreateDoc, mailTransportCreateDoc,
	mailTransportListDoc,
}

// mailAliasListDoc documents GET /api/mail/aliases.
//
//	@Summary		List email aliases
//	@Description	mail_forwarding rows with type=alias (type-filtered). forwards/catchalls/alias-domains have the same shape under /api/mail/{forwards,catchalls,alias-domains}.
//	@Tags			mail-forwarding
//	@Produce		json
//	@Success		200	{object}	ListResponse
//	@Failure		401	{object}	ErrorResponse
//	@Router			/mail/aliases [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailAliasListDoc() {}

// mailAliasCreateDoc documents POST /api/mail/aliases.
//
//	@Summary		Create an email alias
//	@Description	Creates a mail_forwarding row with type=alias (source and destination are email addresses). Datalog insert written.
//	@Tags			mail-forwarding
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.MailForwarding	true	"source/destination emails"
//	@Success		201		{object}	model.MailForwarding
//	@Failure		422		{object}	ErrorResponse
//	@Router			/mail/aliases [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailAliasCreateDoc() {}

// mailForwardCreateDoc documents POST /api/mail/forwards.
//
//	@Summary		Create a forwarding
//	@Description	mail_forwarding type=forward (source and destination emails).
//	@Tags			mail-forwarding
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.MailForwarding	true	"source/destination"
//	@Success		201		{object}	model.MailForwarding
//	@Router			/mail/forwards [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailForwardCreateDoc() {}

// mailCatchallCreateDoc documents POST /api/mail/catchalls.
//
//	@Summary		Create a catchall
//	@Description	mail_forwarding type=catchall (source is a @domain form, destination an email).
//	@Tags			mail-forwarding
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.MailForwarding	true	"source @domain, destination email"
//	@Success		201		{object}	model.MailForwarding
//	@Router			/mail/catchalls [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailCatchallCreateDoc() {}

// mailAliasDomainCreateDoc documents POST /api/mail/alias-domains.
//
//	@Summary		Create an alias domain
//	@Description	mail_forwarding type=aliasdomain (source and destination are @domain forms).
//	@Tags			mail-forwarding
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.MailForwarding	true	"source/destination @domain"
//	@Success		201		{object}	model.MailForwarding
//	@Router			/mail/alias-domains [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailAliasDomainCreateDoc() {}

// mailTransportListDoc documents GET /api/mail/transports.
//
//	@Summary	List mail transports
//	@Tags		mail-transports
//	@Produce	json
//	@Success	200	{object}	ListResponse
//	@Failure	401	{object}	ErrorResponse
//	@Router		/mail/transports [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func mailTransportListDoc() {}

// mailTransportCreateDoc documents POST /api/mail/transports.
//
//	@Summary		Create a mail transport
//	@Description	Unique (server_id, domain); the domain may not collide with a mail_domain on the same server.
//	@Tags			mail-transports
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.MailTransport	true	"domain, transport, sort_order"
//	@Success		201		{object}	model.MailTransport
//	@Failure		422		{object}	ErrorResponse	"Duplicate domain or maildomain collision"
//	@Router			/mail/transports [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailTransportCreateDoc() {}

// Swaggo annotations for the access + spamfilter endpoints.
var _ = []any{
	mailAccessListDoc, mailAccessCreateDoc, spamPolicyListDoc, spamPolicyCreateDoc,
	spamUserCreateDoc, spamWblistCreateDoc,
}

// mailAccessListDoc documents GET /api/mail/access.
//
//	@Summary		List mail access entries
//	@Description	mail_access rows (recipient/sender/client). The daemon renders them into Rspamd global wblist maps.
//	@Tags			mail-access
//	@Produce		json
//	@Success		200	{object}	ListResponse
//	@Failure		401	{object}	ErrorResponse
//	@Router			/mail/access [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailAccessListDoc() {}

// mailAccessCreateDoc documents POST /api/mail/access.
//
//	@Summary		Create a mail access entry
//	@Description	type recipient/sender/client, access e.g. OK/REJECT. Unique (server_id, source, type).
//	@Tags			mail-access
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.MailAccess	true	"source, access, type"
//	@Success		201		{object}	model.MailAccess
//	@Failure		422		{object}	ErrorResponse
//	@Router			/mail/access [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func mailAccessCreateDoc() {}

// spamPolicyListDoc documents GET /api/mail/spamfilter/policies.
//
//	@Summary		List spamfilter policies
//	@Description	Admin only. Rspamd thresholds per policy.
//	@Tags			spamfilter
//	@Produce		json
//	@Success		200	{object}	ListResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/mail/spamfilter/policies [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func spamPolicyListDoc() {}

// spamPolicyCreateDoc documents POST /api/mail/spamfilter/policies.
//
//	@Summary		Create a spamfilter policy
//	@Description	Admin only. No daemon table hook — settings refresh when a dependent spamfilter user/mailbox event fires (design D2).
//	@Tags			spamfilter
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.SpamfilterPolicy	true	"policy fields"
//	@Success		201		{object}	model.SpamfilterPolicy
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse
//	@Router			/mail/spamfilter/policies [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func spamPolicyCreateDoc() {}

// spamUserCreateDoc documents POST /api/mail/spamfilter/users.
//
//	@Summary		Create a spamfilter user
//	@Description	Assigns a policy to an email/@domain identity (unique email). The daemon rewrites that identity's Rspamd settings.
//	@Tags			spamfilter
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.SpamfilterUser	true	"email, policy_id, priority"
//	@Success		201		{object}	model.SpamfilterUser
//	@Failure		422		{object}	ErrorResponse
//	@Router			/mail/spamfilter/users [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func spamUserCreateDoc() {}

// spamWblistCreateDoc documents POST /api/mail/spamfilter/wblists.
//
//	@Summary		Create a spamfilter white/blacklist entry
//	@Description	wb W or B, rid references a spamfilter user. The daemon renders a per-user Rspamd wblist conf.
//	@Tags			spamfilter
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.SpamfilterWblist	true	"wb, rid, email"
//	@Success		201		{object}	model.SpamfilterWblist
//	@Failure		422		{object}	ErrorResponse
//	@Router			/mail/spamfilter/wblists [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func spamWblistCreateDoc() {}
