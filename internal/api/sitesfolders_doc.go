package api

// Swaggo annotations of the web-folders and web-folder-users entities
// (routes registered generically by RegisterEntity).
var _ = []any{
	webFolderListDoc, webFolderGetDoc, webFolderCreateDoc, webFolderUpdateDoc, webFolderDeleteDoc,
	webFolderUserListDoc, webFolderUserGetDoc, webFolderUserCreateDoc,
	webFolderUserUpdateDoc, webFolderUserDeleteDoc,
}

// webFolderListDoc documents GET /api/sites/web-folders.
//
//	@Summary	List protected folders
//	@Tags		sites
//	@Produce	json
//	@Param		page	query		int	false	"1-based page number"	default(1)
//	@Param		limit	query		int	false	"Page size (max 100)"	default(25)
//	@Success	200		{object}	ListResponse
//	@Failure	401		{object}	ErrorResponse
//	@Router		/sites/web-folders [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func webFolderListDoc() {}

// webFolderGetDoc documents GET /api/sites/web-folders/{id}.
//
//	@Summary	Get a protected folder
//	@Tags		sites
//	@Produce	json
//	@Param		id	path		int	true	"web_folder_id"
//	@Success	200	{object}	model.WebFolder
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse	"Permission denied or record not found"
//	@Router		/sites/web-folders/{id} [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func webFolderGetDoc() {}

// webFolderCreateDoc documents POST /api/sites/web-folders.
//
//	@Summary		Create a protected folder
//	@Description	server_id is derived from the referenced parent domain, which must be readable by the caller (cross-client references are denied). The daemon maintains the .htpasswd file and the auth_basic vhost location.
//	@Tags			sites
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.WebFolder	true	"Field values"
//	@Success		201		{object}	model.WebFolder
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/sites/web-folders [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func webFolderCreateDoc() {}

// webFolderUpdateDoc documents PUT /api/sites/web-folders/{id}.
//
//	@Summary	Update a protected folder
//	@Tags		sites
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int				true	"web_folder_id"
//	@Param		record	body		model.WebFolder	true	"Changed field values"
//	@Success	200		{object}	model.WebFolder
//	@Failure	401		{object}	ErrorResponse
//	@Failure	403		{object}	ErrorResponse
//	@Failure	422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router		/sites/web-folders/{id} [put]
//	@Security	CookieAuth
//	@Security	BearerAuth
func webFolderUpdateDoc() {}

// webFolderDeleteDoc documents DELETE /api/sites/web-folders/{id}.
//
//	@Summary	Delete a protected folder
//	@Tags		sites
//	@Param		id	path	int	true	"web_folder_id"
//	@Success	204	"Deleted"
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse
//	@Router		/sites/web-folders/{id} [delete]
//	@Security	CookieAuth
//	@Security	BearerAuth
func webFolderDeleteDoc() {}

// webFolderUserListDoc documents GET /api/sites/web-folder-users.
//
//	@Summary	List protected folder users
//	@Tags		sites
//	@Produce	json
//	@Param		page			query		int		false	"1-based page number"	default(1)
//	@Param		limit			query		int		false	"Page size (max 100)"	default(25)
//	@Param		web_folder_id	query		string	false	"Filter on web_folder_id"
//	@Success	200				{object}	ListResponse
//	@Failure	401				{object}	ErrorResponse
//	@Router		/sites/web-folder-users [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func webFolderUserListDoc() {}

// webFolderUserGetDoc documents GET /api/sites/web-folder-users/{id}.
//
//	@Summary	Get a protected folder user
//	@Tags		sites
//	@Produce	json
//	@Param		id	path		int	true	"web_folder_user_id"
//	@Success	200	{object}	model.WebFolderUser
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse	"Permission denied or record not found"
//	@Router		/sites/web-folder-users/{id} [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func webFolderUserGetDoc() {}

// webFolderUserCreateDoc documents POST /api/sites/web-folder-users.
//
//	@Summary		Create a protected folder user
//	@Description	The plaintext password is stored SHA-512 crypted (never plain); the daemon copies the hash into the folder's .htpasswd file. server_id is derived from the referenced folder, which must be readable by the caller.
//	@Tags			sites
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.WebFolderUser	true	"Field values"
//	@Success		201		{object}	model.WebFolderUser
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/sites/web-folder-users [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func webFolderUserCreateDoc() {}

// webFolderUserUpdateDoc documents PUT /api/sites/web-folder-users/{id}.
//
//	@Summary	Update a protected folder user
//	@Tags		sites
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int					true	"web_folder_user_id"
//	@Param		record	body		model.WebFolderUser	true	"Changed field values (a plaintext password is re-crypted)"
//	@Success	200		{object}	model.WebFolderUser
//	@Failure	401		{object}	ErrorResponse
//	@Failure	403		{object}	ErrorResponse
//	@Failure	422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router		/sites/web-folder-users/{id} [put]
//	@Security	CookieAuth
//	@Security	BearerAuth
func webFolderUserUpdateDoc() {}

// webFolderUserDeleteDoc documents DELETE /api/sites/web-folder-users/{id}.
//
//	@Summary	Delete a protected folder user
//	@Tags		sites
//	@Param		id	path	int	true	"web_folder_user_id"
//	@Success	204	"Deleted"
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse
//	@Router		/sites/web-folder-users/{id} [delete]
//	@Security	CookieAuth
//	@Security	BearerAuth
func webFolderUserDeleteDoc() {}
