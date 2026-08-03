package api

// Swaggo annotations of the ftp-users and shell-users entities
// (routes registered generically by RegisterEntity).
var _ = []any{
	ftpUserListDoc, ftpUserGetDoc, ftpUserCreateDoc, ftpUserUpdateDoc, ftpUserDeleteDoc,
	shellUserListDoc, shellUserGetDoc, shellUserCreateDoc, shellUserUpdateDoc, shellUserDeleteDoc,
}

// ftpUserListDoc documents GET /api/sites/ftp-users.
//
//	@Summary		List FTP users
//	@Description	Paginated, permission-scoped list. Password hashes are always redacted. Items carry _server_name, _parent_domain and _datalog_state.
//	@Tags			sites
//	@Produce		json
//	@Param			page		query		int		false	"1-based page number"	default(1)
//	@Param			limit		query		int		false	"Page size (max 100)"	default(25)
//	@Param			username	query		string	false	"Substring filter on username"
//	@Success		200			{object}	ListResponse
//	@Failure		401			{object}	ErrorResponse
//	@Router			/sites/ftp-users [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func ftpUserListDoc() {}

// ftpUserGetDoc documents GET /api/sites/ftp-users/{id}.
//
//	@Summary		Get an FTP user
//	@Description	Password hash is always redacted.
//	@Tags			sites
//	@Produce		json
//	@Param			id	path		int	true	"ftp_user_id"
//	@Success		200	{object}	model.FTPUser
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Permission denied or record not found"
//	@Router			/sites/ftp-users/{id} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func ftpUserGetDoc() {}

// ftpUserCreateDoc documents POST /api/sites/ftp-users.
//
//	@Summary		Create an FTP user
//	@Description	parent_domain_id must reference a vhost readable by the caller. username is stored with the configured ftpuser_prefix; password is CRYPT-hashed. server_id, uid, gid and default dir are derived from the parent site. Client limit_ftp_user applies. PureFTPd authenticates against the table — no OS account is created.
//	@Tags			sites
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.FTPUser	true	"Field values"
//	@Success		201		{object}	model.FTPUser
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Permission denied or limit reached"
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/sites/ftp-users [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func ftpUserCreateDoc() {}

// ftpUserUpdateDoc documents PUT /api/sites/ftp-users/{id}.
//
//	@Summary		Update an FTP user
//	@Description	Empty password leaves the stored hash unchanged. dir must stay under the parent document root. Admin-only Options fields (uid, gid, ratios, bandwidth) are ignored for non-admin callers.
//	@Tags			sites
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int				true	"ftp_user_id"
//	@Param			record	body		model.FTPUser	true	"Changed field values"
//	@Success		200		{object}	model.FTPUser
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse
//	@Router			/sites/ftp-users/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func ftpUserUpdateDoc() {}

// ftpUserDeleteDoc documents DELETE /api/sites/ftp-users/{id}.
//
//	@Summary		Delete an FTP user
//	@Description	The daemon removes only the .ftpquota marker; the directory tree is left intact.
//	@Tags			sites
//	@Param			id	path	int	true	"ftp_user_id"
//	@Success		204
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/sites/ftp-users/{id} [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func ftpUserDeleteDoc() {}

// shellUserListDoc documents GET /api/sites/shell-users.
//
//	@Summary		List shell users
//	@Description	Paginated, permission-scoped list. Password hashes are always redacted. Items carry _server_name, _parent_domain and _datalog_state.
//	@Tags			sites
//	@Produce		json
//	@Param			page		query		int		false	"1-based page number"	default(1)
//	@Param			limit		query		int		false	"Page size (max 100)"	default(25)
//	@Param			username	query		string	false	"Substring filter on username"
//	@Success		200			{object}	ListResponse
//	@Failure		401			{object}	ErrorResponse
//	@Router			/sites/shell-users [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func shellUserListDoc() {}

// shellUserGetDoc documents GET /api/sites/shell-users/{id}.
//
//	@Summary		Get a shell user
//	@Description	Password hash is always redacted; ssh_rsa public keys are returned for edit.
//	@Tags			sites
//	@Produce		json
//	@Param			id	path		int	true	"shell_user_id"
//	@Success		200	{object}	model.ShellUser
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Permission denied or record not found"
//	@Router			/sites/shell-users/{id} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func shellUserGetDoc() {}

// shellUserCreateDoc documents POST /api/sites/shell-users.
//
//	@Summary		Create a shell user
//	@Description	parent_domain_id must reference a vhost. username is unique, blacklist-checked and capped at 32 chars after shelluser_prefix. Password is CRYPT-hashed subject to global ssh_authentication mode. chroot is empty/no or jailkit (client ssh_chroot allow-list). The daemon creates a real OS account (unless allow_shell_user is disabled).
//	@Tags			sites
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.ShellUser	true	"Field values"
//	@Success		201		{object}	model.ShellUser
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Permission denied or limit reached"
//	@Failure		422		{object}	ErrorResponse
//	@Router			/sites/shell-users [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func shellUserCreateDoc() {}

// shellUserUpdateDoc documents PUT /api/sites/shell-users/{id}.
//
//	@Summary		Update a shell user
//	@Description	parent_domain_id is immutable after create. Empty password leaves the stored hash unchanged. Admin-only Options (puser, pgroup, shell, dir) are ignored for non-admin callers.
//	@Tags			sites
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int				true	"shell_user_id"
//	@Param			record	body		model.ShellUser	true	"Changed field values"
//	@Success		200		{object}	model.ShellUser
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse
//	@Router			/sites/shell-users/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func shellUserUpdateDoc() {}

// shellUserDeleteDoc documents DELETE /api/sites/shell-users/{id}.
//
//	@Summary		Delete a shell user
//	@Description	The daemon removes the OS account (jailkit users are handled by the jailkit plugin).
//	@Tags			sites
//	@Param			id	path	int	true	"shell_user_id"
//	@Success		204
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/sites/shell-users/{id} [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func shellUserDeleteDoc() {}
