package api

// Swaggo annotations of the databases and database-users entities
// (routes registered generically by RegisterEntity).
var _ = []any{
	databaseListDoc, databaseGetDoc, databaseCreateDoc, databaseUpdateDoc, databaseDeleteDoc,
	databaseUserListDoc, databaseUserGetDoc, databaseUserCreateDoc,
	databaseUserUpdateDoc, databaseUserDeleteDoc,
}

// databaseListDoc documents GET /api/sites/databases.
//
//	@Summary	List client databases
//	@Tags		sites
//	@Produce	json
//	@Param		page	query		int	false	"1-based page number"	default(1)
//	@Param		limit	query		int	false	"Page size (max 100)"	default(25)
//	@Success	200		{object}	ListResponse
//	@Failure	401		{object}	ErrorResponse
//	@Router		/sites/databases [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func databaseListDoc() {}

// databaseGetDoc documents GET /api/sites/databases/{id}.
//
//	@Summary	Get a client database
//	@Tags		sites
//	@Produce	json
//	@Param		id	path		int	true	"database_id"
//	@Success	200	{object}	model.WebDatabase
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse	"Permission denied or record not found"
//	@Router		/sites/databases/{id} [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func databaseGetDoc() {}

// databaseCreateDoc documents POST /api/sites/databases.
//
//	@Summary		Create a client database
//	@Description	MySQL only. database_name is submitted without the configured dbname_prefix — the API prepends and crops it to 64 chars. parent_domain_id must reference a vhost readable by the caller; the database inherits its sys_groupid and backup_copies. When the site lives on another server, remote access is force-enabled and the web server IP merges into remote_ips. Client limits (limit_database, limit_database_quota, db_servers) apply. The daemon provisions the physical database and GRANTs.
//	@Tags			sites
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.WebDatabase	true	"Field values"
//	@Success		201		{object}	model.WebDatabase
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Permission denied or limit reached"
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/sites/databases [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func databaseCreateDoc() {}

// databaseUpdateDoc documents PUT /api/sites/databases/{id}.
//
//	@Summary		Update a client database
//	@Description	server_id and database_charset are immutable; only admin may rename. Deactivating revokes the MySQL grants; quota changes re-check limit_database_quota.
//	@Tags			sites
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int					true	"database_id"
//	@Param			record	body		model.WebDatabase	true	"Changed field values"
//	@Success		200		{object}	model.WebDatabase
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/sites/databases/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func databaseUpdateDoc() {}

// databaseDeleteDoc documents DELETE /api/sites/databases/{id}.
//
//	@Summary		Delete a client database
//	@Description	The daemon revokes the users' grants (dropping MySQL accounts no other active database needs) and drops the physical database.
//	@Tags			sites
//	@Param			id	path	int	true	"database_id"
//	@Success		204
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/sites/databases/{id} [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func databaseDeleteDoc() {}

// databaseUserListDoc documents GET /api/sites/database-users.
//
//	@Summary		List database users
//	@Description	Password hash columns are always redacted.
//	@Tags			sites
//	@Produce		json
//	@Param			page	query		int	false	"1-based page number"	default(1)
//	@Param			limit	query		int	false	"Page size (max 100)"	default(25)
//	@Success		200		{object}	ListResponse
//	@Failure		401		{object}	ErrorResponse
//	@Router			/sites/database-users [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func databaseUserListDoc() {}

// databaseUserGetDoc documents GET /api/sites/database-users/{id}.
//
//	@Summary		Get a database user
//	@Description	Password hash columns are always redacted.
//	@Tags			sites
//	@Produce		json
//	@Param			id	path		int	true	"database_user_id"
//	@Success		200	{object}	model.WebDatabaseUser
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Permission denied or record not found"
//	@Router			/sites/database-users/{id} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func databaseUserGetDoc() {}

// databaseUserCreateDoc documents POST /api/sites/database-users.
//
//	@Summary		Create a database user
//	@Description	database_user is submitted without the configured dbuser_prefix — the API prepends and crops it to 32 chars. database_password is write-only plaintext: the API stores the MySQL native and caching_sha2 hashes and never returns them. The password policy (getconf misc min_password_length/strength) applies. The MySQL account materialises when a database first grants it.
//	@Tags			sites
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.WebDatabaseUser	true	"Field values"
//	@Success		201		{object}	model.WebDatabaseUser
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Permission denied or limit reached"
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/sites/database-users [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func databaseUserCreateDoc() {}

// databaseUserUpdateDoc documents PUT /api/sites/database-users/{id}.
//
//	@Summary		Update a database user
//	@Description	An empty or omitted database_password leaves the stored hashes unchanged. Renames and password changes fan out to every server with databases referencing this user.
//	@Tags			sites
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int						true	"database_user_id"
//	@Param			record	body		model.WebDatabaseUser	true	"Changed field values"
//	@Success		200		{object}	model.WebDatabaseUser
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/sites/database-users/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func databaseUserUpdateDoc() {}

// databaseUserDeleteDoc documents DELETE /api/sites/database-users/{id}.
//
//	@Summary		Delete a database user
//	@Description	Databases referencing the user keep working rows-wise: their database_user_id/database_ro_user_id are nulled with journaled updates and the daemon drops the MySQL account.
//	@Tags			sites
//	@Param			id	path	int	true	"database_user_id"
//	@Success		204
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/sites/database-users/{id} [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func databaseUserDeleteDoc() {}
