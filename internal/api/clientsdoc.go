package api

// Swaggo annotations for the client and reseller endpoints. The CRUD
// routes are registered generically by RegisterEntity; the functions
// below only carry the per-route documentation.
var _ = []any{
	clientListDoc, clientGetDoc, clientCreateDoc, clientUpdateDoc, clientDeleteDoc,
	resellerListDoc, resellerGetDoc, resellerCreateDoc, resellerUpdateDoc, resellerDeleteDoc,
	clientByUsernameDoc, clientByCustomerNoDoc, clientByGroupIDDoc, clientIDBySysUserDoc,
	clientChangePasswordDoc, clientDeleteEverythingDoc,
	clientTemplateListDoc, clientTemplateGetDoc, clientTemplateCreateDoc,
	clientTemplateUpdateDoc, clientTemplateDeleteDoc,
	clientTemplatesAssignedListDoc, clientTemplateAssignDoc, clientTemplateUnassignDoc,
	countriesDoc,
	clientMessageTemplateListDoc, clientMessageTemplateGetDoc, clientMessageTemplateCreateDoc,
	clientMessageTemplateUpdateDoc, clientMessageTemplateDeleteDoc, clientSendMessageDoc,
	clientResourceCountsDoc,
}

// clientListDoc documents GET /api/clients.
//
//	@Summary		List clients
//	@Description	Paginated, permission-scoped list of client rows with limit_client = 0 (non-resellers). Any declared field name may be passed as a query parameter for substring filtering (e.g. ?contact_name=smith). Password and key fields are never returned.
//	@Tags			clients
//	@Produce		json
//	@Param			page		query		int		false	"1-based page number"	default(1)
//	@Param			limit		query		int		false	"Page size (max 100)"	default(25)
//	@Param			username	query		string	false	"Substring filter on username"
//	@Success		200			{object}	ListResponse
//	@Failure		401			{object}	ErrorResponse
//	@Router			/clients [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientListDoc() {}

// clientGetDoc documents GET /api/clients/{id}.
//
//	@Summary		Get a client
//	@Description	Password and key fields are redacted.
//	@Tags			clients
//	@Produce		json
//	@Param			id	path		int	true	"client_id"
//	@Success		200	{object}	model.Client
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Permission denied or record not accessible"
//	@Router			/clients/{id} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientGetDoc() {}

// clientCreateDoc documents POST /api/clients.
//
//	@Summary		Create a client
//	@Description	Creates the client plus its linked sys_group and sys_user login (bcrypt-hashed password), applies limit templates (template_master/additional) and journals to sys_datalog. A non-admin (reseller) creator always becomes parent_client_id; admins may set parent_client_id to a reseller. locked/canceled are folded into this entity (locked=y disables the login). Subject to the reseller's limit_client quota (403 error.limit_client).
//	@Tags			clients
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.Client	true	"Client fields (client.tform.php set); password is plaintext and hashed server-side"
//	@Success		201		{object}	model.Client
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Permission denied or limit_client reached"
//	@Failure		422		{object}	ErrorResponse	"Validation failed (username empty/duplicate/malformed, bad parent, ...)"
//	@Router			/clients [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientCreateDoc() {}

// clientUpdateDoc documents PUT /api/clients/{id}.
//
//	@Summary		Update a client
//	@Description	Re-materializes limit templates, caps limits to the parent reseller, and syncs the login identity (username rename, password re-hash when non-empty, locked toggles sys_user.active, parent change re-owns the row). An empty password means unchanged.
//	@Tags			clients
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int				true	"client_id"
//	@Param			record	body		model.Client	true	"Changed field values"
//	@Success		200		{object}	model.Client
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse
//	@Router			/clients/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientUpdateDoc() {}

// clientDeleteDoc documents DELETE /api/clients/{id}.
//
//	@Summary		Delete a client
//	@Description	Removes the client, its sys_user/sys_group, group memberships and template assignments; journals a datalog delete. A reseller that still has child clients is refused (422 error.client_has_children). Owned resources are kept — use delete-everything to purge them.
//	@Tags			clients
//	@Param			id	path	int	true	"client_id"
//	@Success		204
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		422	{object}	ErrorResponse	"Reseller still has child clients"
//	@Router			/clients/{id} [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientDeleteDoc() {}

// resellerListDoc documents GET /api/resellers.
//
//	@Summary		List resellers
//	@Description	Admin only. Client rows with limit_client != 0. This is the single documented reseller surface; /api/clients never returns resellers.
//	@Tags			resellers
//	@Produce		json
//	@Param			page	query		int	false	"1-based page number"	default(1)
//	@Param			limit	query		int	false	"Page size (max 100)"	default(25)
//	@Success		200		{object}	ListResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Not an admin session"
//	@Router			/resellers [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func resellerListDoc() {}

// resellerGetDoc documents GET /api/resellers/{id}.
//
//	@Summary	Get a reseller
//	@Tags		resellers
//	@Produce	json
//	@Param		id	path		int	true	"client_id"
//	@Success	200	{object}	model.Client
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse
//	@Router		/resellers/{id} [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func resellerGetDoc() {}

// resellerCreateDoc documents POST /api/resellers.
//
//	@Summary		Create a reseller
//	@Description	Admin only. Like client create, but limit_client defaults to 100 and must stay non-zero (the reseller role discriminator); the provisioned sys_user carries the client module. A reseller can never have a parent (nesting is rejected with 422).
//	@Tags			resellers
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.Client	true	"Reseller fields (reseller.tform.php set)"
//	@Success		201		{object}	model.Client
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Validation failed (limit_client = 0, parent set, ...)"
//	@Router			/resellers [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func resellerCreateDoc() {}

// resellerUpdateDoc documents PUT /api/resellers/{id}.
//
//	@Summary		Update a reseller
//	@Description	Admin only. Setting limit_client to 0 is rejected (a reseller cannot be demoted here); the client module token on the login follows limit_client.
//	@Tags			resellers
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int				true	"client_id"
//	@Param			record	body		model.Client	true	"Changed field values"
//	@Success		200		{object}	model.Client
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse
//	@Router			/resellers/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func resellerUpdateDoc() {}

// resellerDeleteDoc documents DELETE /api/resellers/{id}.
//
//	@Summary		Delete a reseller
//	@Description	Admin only. Refused while child clients exist (422 error.client_has_children).
//	@Tags			resellers
//	@Param			id	path	int	true	"client_id"
//	@Success		204
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		422	{object}	ErrorResponse
//	@Router			/resellers/{id} [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func resellerDeleteDoc() {}

// clientByUsernameDoc documents GET /api/clients/by-username/{username}.
//
//	@Summary		Get a client by username
//	@Description	Permission-scoped lookup (remote client_get_by_username). Missing and inaccessible rows are both 404. Secrets redacted.
//	@Tags			clients
//	@Produce		json
//	@Param			username	path		string	true	"Login username"
//	@Success		200			{object}	model.Client
//	@Failure		401			{object}	ErrorResponse
//	@Failure		404			{object}	ErrorResponse
//	@Router			/clients/by-username/{username} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientByUsernameDoc() {}

// clientByCustomerNoDoc documents GET /api/clients/by-customer-no/{no}.
//
//	@Summary	Get a client by customer number
//	@Tags		clients
//	@Produce	json
//	@Param		no	path		string	true	"customer_no"
//	@Success	200	{object}	model.Client
//	@Failure	401	{object}	ErrorResponse
//	@Failure	404	{object}	ErrorResponse
//	@Router		/clients/by-customer-no/{no} [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func clientByCustomerNoDoc() {}

// clientByGroupIDDoc documents GET /api/clients/by-groupid/{groupid}.
//
//	@Summary		Get a client by sys_group id
//	@Description	Resolves the owning client of a sys_group (remote client_get_by_groupid).
//	@Tags			clients
//	@Produce		json
//	@Param			groupid	path		int	true	"sys_group groupid"
//	@Success		200		{object}	model.Client
//	@Failure		401		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Router			/clients/by-groupid/{groupid} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientByGroupIDDoc() {}

// clientIDBySysUserDoc documents GET /api/clients/id-by-sysuser/{userid}.
//
//	@Summary		Get the client id of an interface login
//	@Description	remote client_get_id: maps sys_user.userid to its client_id. 404 when the login is not tied to a client.
//	@Tags			clients
//	@Produce		json
//	@Param			userid	path		int				true	"sys_user userid"
//	@Success		200		{object}	map[string]any	"{client_id}"
//	@Failure		401		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Router			/clients/id-by-sysuser/{userid} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientIDBySysUserDoc() {}

// clientChangePasswordDoc documents POST /api/clients/{id}/change-password.
//
//	@Summary		Change a client's login password
//	@Description	remote client_change_password: bcrypt-hashes the new password onto client.password and sys_user.passwort and stamps last_password_change. Update-scoped: only the admin or the owning reseller may call it. Minimum length 8.
//	@Tags			clients
//	@Accept			json
//	@Param			id		path	int					true	"client_id"
//	@Param			record	body	changePasswordBody	true	"New plaintext password"
//	@Success		204
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		422	{object}	ErrorResponse	"Password below policy"
//	@Router			/clients/{id}/change-password [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientChangePasswordDoc() {}

// clientDeleteEverythingDoc documents DELETE /api/clients/{id}/everything.
//
//	@Summary		Delete a client and everything it owns
//	@Description	Admin only (remote client_delete_everything): cascades child clients (their resources and logins included), purges every resource owned by the group(s) (web domains, folders, folder users, dns zones, records, slave zones — the tables Go daemons consume today; more follow with their modules) with one datalog delete per row, then deprovisions the login and deletes the client.
//	@Tags			clients
//	@Param			id	path	int	true	"client_id"
//	@Success		204
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Not an admin session"
//	@Failure		404	{object}	ErrorResponse
//	@Router			/clients/{id}/everything [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientDeleteEverythingDoc() {}

// clientTemplateListDoc documents GET /api/client-templates.
//
//	@Summary		List client limit templates
//	@Description	Admin only (client_templates_get_all). Master ('m') and additional ('a') templates with their limit columns.
//	@Tags			client-templates
//	@Produce		json
//	@Param			page	query		int	false	"1-based page number"	default(1)
//	@Param			limit	query		int	false	"Page size (max 100)"	default(25)
//	@Success		200		{object}	ListResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Not an admin session"
//	@Router			/client-templates [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientTemplateListDoc() {}

// clientTemplateGetDoc documents GET /api/client-templates/{id}.
//
//	@Summary	Get a client limit template
//	@Tags		client-templates
//	@Produce	json
//	@Param		id	path		int	true	"template_id"
//	@Success	200	{object}	model.ClientTemplate
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse
//	@Router		/client-templates/{id} [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func clientTemplateGetDoc() {}

// clientTemplateCreateDoc documents POST /api/client-templates.
//
//	@Summary		Create a client limit template
//	@Description	Admin only. template_type 'm' (master, applied via client.template_master) or 'a' (additional, applied via the assignment endpoints).
//	@Tags			client-templates
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.ClientTemplate	true	"Template fields"
//	@Success		201		{object}	model.ClientTemplate
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Validation failed (empty template_name, ...)"
//	@Router			/client-templates [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientTemplateCreateDoc() {}

// clientTemplateUpdateDoc documents PUT /api/client-templates/{id}.
//
//	@Summary		Update a client limit template
//	@Description	Admin only. Changed limits apply to a client the next time that client is saved or its assignments change (no immediate fan-out re-materialization).
//	@Tags			client-templates
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int						true	"template_id"
//	@Param			record	body		model.ClientTemplate	true	"Changed field values"
//	@Success		200		{object}	model.ClientTemplate
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse
//	@Router			/client-templates/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientTemplateUpdateDoc() {}

// clientTemplateDeleteDoc documents DELETE /api/client-templates/{id}.
//
//	@Summary		Delete a client limit template
//	@Description	Admin only. Existing assignments to the deleted template are skipped at merge time (PHP parity).
//	@Tags			client-templates
//	@Param			id	path	int	true	"template_id"
//	@Success		204
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/client-templates/{id} [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientTemplateDeleteDoc() {}

// clientTemplatesAssignedListDoc documents GET /api/clients/{id}/templates.
//
//	@Summary		List a client's additional templates
//	@Description	remote client_template_additional_get: the client_template_assigned rows in assignment order.
//	@Tags			client-templates
//	@Produce		json
//	@Param			id	path		int	true	"client_id"
//	@Success		200	{array}		assignedTemplateJSON
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/clients/{id}/templates [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientTemplatesAssignedListDoc() {}

// clientTemplateAssignDoc documents POST /api/clients/{id}/templates.
//
//	@Summary		Assign an additional template to a client
//	@Description	remote client_template_additional_add: creates a client_template_assigned row and re-materializes the client limits in the same transaction. The same template may be assigned repeatedly (its limits add each time).
//	@Tags			client-templates
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int					true	"client_id"
//	@Param			record	body		assignTemplateBody	true	"Template to assign"
//	@Success		201		{object}	assignedTemplateJSON
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"No update permission on the client"
//	@Failure		404		{object}	ErrorResponse	"Unknown template id"
//	@Router			/clients/{id}/templates [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientTemplateAssignDoc() {}

// clientTemplateUnassignDoc documents DELETE /api/clients/{id}/templates/{assigned}.
//
//	@Summary		Remove an additional template assignment
//	@Description	remote client_template_additional_delete: deletes one client_template_assigned row and re-materializes the client limits in the same transaction.
//	@Tags			client-templates
//	@Param			id			path	int	true	"client_id"
//	@Param			assigned	path	int	true	"assigned_template_id"
//	@Success		204
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse	"Assignment not found on this client"
//	@Router			/clients/{id}/templates/{assigned} [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientTemplateUnassignDoc() {}

// countriesDoc documents GET /api/countries.
//
//	@Summary		List countries
//	@Description	Read-only ISO country list for address forms, ordered by printable name.
//	@Tags			clients
//	@Produce		json
//	@Success		200	{array}		model.Country
//	@Failure		401	{object}	ErrorResponse
//	@Router			/countries [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func countriesDoc() {}

// clientMessageTemplateListDoc documents GET /api/client-message-templates.
//
//	@Summary		List client message templates
//	@Description	Ownership-scoped (resellers manage their own templates; admins see all). template_type "welcome" of the creating user's group is sent automatically after client creation when SMTP is configured.
//	@Tags			client-messages
//	@Produce		json
//	@Success		200	{object}	ListResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/client-message-templates [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientMessageTemplateListDoc() {}

// clientMessageTemplateGetDoc documents GET /api/client-message-templates/{id}.
//
//	@Summary	Get a client message template
//	@Tags		client-messages
//	@Produce	json
//	@Param		id	path		int	true	"client_message_template_id"
//	@Success	200	{object}	model.ClientMessageTemplate
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse
//	@Router		/client-message-templates/{id} [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func clientMessageTemplateGetDoc() {}

// clientMessageTemplateCreateDoc documents POST /api/client-message-templates.
//
//	@Summary		Create a client message template
//	@Description	Subject and message may use {column} placeholders resolved from the recipient client (e.g. {username}, {contact_name}); {password} resolves only in the welcome-on-create email.
//	@Tags			client-messages
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.ClientMessageTemplate	true	"Template fields"
//	@Success		201		{object}	model.ClientMessageTemplate
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse
//	@Router			/client-message-templates [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientMessageTemplateCreateDoc() {}

// clientMessageTemplateUpdateDoc documents PUT /api/client-message-templates/{id}.
//
//	@Summary	Update a client message template
//	@Tags		client-messages
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int							true	"client_message_template_id"
//	@Param		record	body		model.ClientMessageTemplate	true	"Changed field values"
//	@Success	200		{object}	model.ClientMessageTemplate
//	@Failure	401		{object}	ErrorResponse
//	@Failure	403		{object}	ErrorResponse
//	@Failure	422		{object}	ErrorResponse
//	@Router		/client-message-templates/{id} [put]
//	@Security	CookieAuth
//	@Security	BearerAuth
func clientMessageTemplateUpdateDoc() {}

// clientMessageTemplateDeleteDoc documents DELETE /api/client-message-templates/{id}.
//
//	@Summary	Delete a client message template
//	@Tags		client-messages
//	@Param		id	path	int	true	"client_message_template_id"
//	@Success	204
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse
//	@Router		/client-message-templates/{id} [delete]
//	@Security	CookieAuth
//	@Security	BearerAuth
func clientMessageTemplateDeleteDoc() {}

// clientSendMessageDoc documents POST /api/clients/send-message.
//
//	@Summary		Send an email to clients
//	@Description	Port of client_message.php. Renders {column} placeholders per recipient and submits one email per client with an address; recipients are permission-scoped (a reseller only reaches its own clients). Subject/message come from the body or from template_id. Requires mail.smtp_host to be configured (422 error.smtp_not_configured otherwise).
//	@Tags			client-messages
//	@Accept			json
//	@Produce		json
//	@Param			record	body		sendMessageBody	true	"Recipients and content"
//	@Success		200		{object}	sendMessageResult
//	@Failure		401		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse	"Unknown template_id"
//	@Failure		422		{object}	ErrorResponse	"No SMTP transport or empty content"
//	@Router			/clients/send-message [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientSendMessageDoc() {}

// clientResourceCountsDoc documents GET /api/clients/{id}/resource-counts.
//
//	@Summary		Count the resources a client owns
//	@Description	Owned-resource counts for the delete confirmation UI (web domains/folders, dns zones/records/slaves, direct child clients). Read-scoped.
//	@Tags			clients
//	@Produce		json
//	@Param			id	path		int	true	"client_id"
//	@Success		200	{object}	map[string]int64
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/clients/{id}/resource-counts [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientResourceCountsDoc() {}
