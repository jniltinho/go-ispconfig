package api

// Swaggo annotations for Sites → Cron (routes registered by RegisterEntity
// plus GET /api/sites/crons/:id/runs).
var _ = []any{
	cronListDoc, cronGetDoc, cronCreateDoc, cronUpdateDoc, cronDeleteDoc, cronRunsDoc,
}

// cronListDoc documents GET /api/sites/crons.
//
//	@Summary	List cron jobs
//	@Tags		sites
//	@Produce	json
//	@Param		page	query		int	false	"1-based page number"	default(1)
//	@Param		limit	query		int	false	"Page size (max 100)"	default(25)
//	@Success	200		{object}	ListResponse
//	@Failure	401		{object}	ErrorResponse
//	@Router		/sites/crons [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func cronListDoc() {}

// cronGetDoc documents GET /api/sites/crons/{id}.
//
//	@Summary	Get a cron job
//	@Tags		sites
//	@Produce	json
//	@Param		id	path		int	true	"cron id"
//	@Success	200	{object}	model.Cron
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse	"Permission denied or record not found"
//	@Router		/sites/crons/{id} [get]
//	@Security	CookieAuth
//	@Security	BearerAuth
func cronGetDoc() {}

// cronCreateDoc documents POST /api/sites/crons.
//
//	@Summary		Create a cron job
//	@Description	parent_domain_id must reference a vhost readable by the caller; server_id and sys_groupid are forced from the parent. Schedule fields (run_min/hour/mday/month/wday) and command are validated (validate_cron rules). Type is auto-derived (URL scheme → url; else owner limit_cron_type → full/chrooted). Non-admin clients are subject to limit_cron, limit_cron_type and limit_cron_frequency. Writes sys_datalog; the daemon registers the job in-process (no OS crontab).
//	@Tags			sites
//	@Accept			json
//	@Produce		json
//	@Param			record	body		model.Cron	true	"Field values"
//	@Success		201		{object}	model.Cron
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Permission denied or limit reached"
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/sites/crons [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func cronCreateDoc() {}

// cronUpdateDoc documents PUT /api/sites/crons/{id}.
//
//	@Summary		Update a cron job
//	@Description	parent_domain_id is immutable. Type is re-derived from command. Non-admin frequency and type limits apply. Writes a sys_datalog update with {old,new}.
//	@Tags			sites
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int			true	"cron id"
//	@Param			record	body		model.Cron	true	"Changed field values"
//	@Success		200		{object}	model.Cron
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/sites/crons/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func cronUpdateDoc() {}

// cronDeleteDoc documents DELETE /api/sites/crons/{id}.
//
//	@Summary		Delete a cron job
//	@Description	Removes the row and journals a datalog delete; the daemon unregisters the job.
//	@Tags			sites
//	@Param			id	path	int	true	"cron id"
//	@Success		204
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/sites/crons/{id} [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func cronDeleteDoc() {}

// cronRunsDoc documents GET /api/sites/crons/{id}/runs.
//
//	@Summary		List cron run history
//	@Description	Paginated sys_log rows matching the cron_run id=&lt;id&gt; message convention (newest first). Requires read permission on the cron row. Each item exposes status, exit, start/end unix times and the bounded output tail.
//	@Tags			sites
//	@Produce		json
//	@Param			id		path		int	true	"cron id"
//	@Param			page	query		int	false	"1-based page number"	default(1)
//	@Param			limit	query		int	false	"Page size (max 100)"	default(25)
//	@Success		200		{object}	cronRunListResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Permission denied or record not found"
//	@Router			/sites/crons/{id}/runs [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func cronRunsDoc() {}
